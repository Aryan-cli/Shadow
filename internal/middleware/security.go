package middleware

import "net/http"

func SecurityHeaders(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("X-Frame-Options", "DENY")
                w.Header().Set("X-Content-Type-Options", "nosniff")
                w.Header().Set("Referrer-Policy", "no-referrer")
                w.Header().Set("X-XSS-Protection", "0")
                w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
                w.Header().Set("Content-Security-Policy",
                        "default-src 'self'; "+
                                "script-src 'self' 'unsafe-inline'; "+
                                "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
                                "font-src 'self' https://fonts.gstatic.com; "+
                                "img-src 'self' data:; "+
                                "connect-src 'self'; "+
                                "frame-ancestors 'none'",
                )
                next.ServeHTTP(w, r)
        })
}
