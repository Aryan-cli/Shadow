package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"time"
)

const csrfCookieName = "_csrf"

func GenerateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GetOrCreateCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	token, err := GenerateCSRFToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int((2 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return token
}

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut ||
			r.Method == http.MethodDelete || r.Method == http.MethodPatch {
			cookie, err := r.Cookie(csrfCookieName)
			if err != nil || cookie.Value == "" {
				http.Error(w, "Forbidden — missing CSRF cookie", http.StatusForbidden)
				return
			}
			formToken := r.FormValue("_csrf")
			if formToken == "" {
				formToken = r.Header.Get("X-CSRF-Token")
			}
			if subtle.ConstantTimeCompare([]byte(formToken), []byte(cookie.Value)) != 1 {
				http.Error(w, "Forbidden — invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
