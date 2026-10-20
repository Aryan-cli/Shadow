package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	cookieName = "shadow_session"
	sessionTTL = 24 * time.Hour
)

type Data struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
}

var rdb *redis.Client

func Init() error {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	rdb = redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   0,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}

func newID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Create(ctx context.Context, userID int, username string) (string, error) {
	sessionID, err := newID()
	if err != nil {
		return "", err
	}
	data := &Data{UserID: userID, Username: username}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	key := "sess:" + sessionID
	if err := rdb.Set(ctx, key, encoded, sessionTTL).Err(); err != nil {
		return "", fmt.Errorf("redis set: %w", err)
	}
	return sessionID, nil
}

func Get(ctx context.Context, sessionID string) (*Data, error) {
	key := "sess:" + sessionID
	val, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var data Data
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, err
	}
	rdb.Expire(ctx, key, sessionTTL)
	return &data, nil
}

func Delete(ctx context.Context, sessionID string) error {
	return rdb.Del(ctx, "sess:"+sessionID).Err()
}

func SetCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   os.Getenv("APP_ENV") == "production",
		SameSite: http.SameSiteStrictMode,
	})
}

func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   os.Getenv("APP_ENV") == "production",
		SameSite: http.SameSiteStrictMode,
	})
}

func FromRequest(r *http.Request) (string, *Data, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "", nil, nil
	}
	data, err := Get(r.Context(), cookie.Value)
	if err != nil {
		return "", nil, err
	}
	return cookie.Value, data, nil
}
