package middleware

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	attempts int
	blocked  time.Time
}

type RateLimiter struct {
	visitors map[string]*visitor
	mu       sync.Mutex
	r        rate.Limit
	b        int
	maxAttempts int
	blockDur    time.Duration
}

func NewRateLimiter(r rate.Limit, b int, maxAttempts int, blockDur time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors:    make(map[string]*visitor),
		r:           r,
		b:           b,
		maxAttempts: maxAttempts,
		blockDur:    blockDur,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	for range time.Tick(5 * time.Minute) {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > 10*time.Minute {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) getVisitor(ip string) *visitor {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	v, ok := rl.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.r, rl.b)}
		rl.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	return v
}

func (rl *RateLimiter) RecordFailedAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	v, ok := rl.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.r, rl.b), lastSeen: time.Now()}
		rl.visitors[ip] = v
	}
	v.attempts++
	if v.attempts >= rl.maxAttempts {
		v.blocked = time.Now().Add(rl.blockDur)
	}
}

func (rl *RateLimiter) ResetAttempts(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if v, ok := rl.visitors[ip]; ok {
		v.attempts = 0
		v.blocked = time.Time{}
	}
}

func (rl *RateLimiter) IsBlocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	v, ok := rl.visitors[ip]
	if !ok {
		return false
	}
	if !v.blocked.IsZero() && time.Now().Before(v.blocked) {
		return true
	}
	if !v.blocked.IsZero() && time.Now().After(v.blocked) {
		v.attempts = 0
		v.blocked = time.Time{}
	}
	return false
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = fwd
		}
		v := rl.getVisitor(ip)
		if !v.limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
