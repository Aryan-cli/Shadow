package handlers

import (
        "database/sql"
        "log"
        "net/http"
        "os"
        "regexp"
        "shadow/internal/auth"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/session"
        "strings"
        "time"
)

var (
        usernameRe      = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)
        loginLimiter    = middleware.NewRateLimiter(5, 10, 5, 15*time.Minute)
        registerLimiter = middleware.NewRateLimiter(1, 5, 3, 1*time.Hour)
)

func clientIP(r *http.Request) string {
        if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
                return strings.SplitN(fwd, ",", 2)[0]
        }
        return r.RemoteAddr
}

func renderPage(w http.ResponseWriter, r *http.Request, tplFile string, data PageData) {
        tmpl, err := parseTemplates(tplFile)
        if err != nil {
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
                log.Printf("template error: %v", err)
        }
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData != nil {
                http.Redirect(w, r, "/", http.StatusSeeOther)
                return
        }
        data := pageData(w, r)

        if r.Method == http.MethodGet {
                renderPage(w, r, "login.html", data)
                return
        }

        ip := clientIP(r)
        if loginLimiter.IsBlocked(ip) {
                data.Error = "Too many failed attempts. Please wait 15 minutes."
                w.WriteHeader(http.StatusTooManyRequests)
                renderPage(w, r, "login.html", data)
                return
        }

        username := strings.TrimSpace(r.FormValue("username"))
        password := r.FormValue("password")

        if username == "" || password == "" {
                data.Error = "Username and password are required."
                renderPage(w, r, "login.html", data)
                return
        }

        adminUser := os.Getenv("ADMIN_USERNAME")
        adminPass := os.Getenv("ADMIN_PASSWORD")
        if adminUser != "" && adminPass != "" && username == adminUser && password == adminPass {
                token := newAdminToken()
                adminSessionsMu.Lock()
                adminSessions[token] = time.Now().Add(adminSessionTTL)
                adminSessionsMu.Unlock()
                http.SetCookie(w, &http.Cookie{
                        Name:     adminCookieName,
                        Value:    token,
                        Path:     "/",
                        MaxAge:   int(adminSessionTTL.Seconds()),
                        HttpOnly: true,
                        Secure:   os.Getenv("APP_ENV") == "production",
                        SameSite: http.SameSiteStrictMode,
                })
                http.Redirect(w, r, GetAdminPath(), http.StatusSeeOther)
                return
        }

        var userID int
        var passwordHash string
        var isBanned bool
        var deletedAt sql.NullTime

        err := db.DB.QueryRowContext(r.Context(),
                `SELECT id, password_hash, COALESCE(banned, false), deleted_at FROM users WHERE username = $1`,
                username,
        ).Scan(&userID, &passwordHash, &isBanned, &deletedAt)

        if err == sql.ErrNoRows {
                loginLimiter.RecordFailedAttempt(ip)
                data.Error = "User not found."
                renderPage(w, r, "login.html", data)
                return
        }
        if err != nil {
                log.Printf("login db error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        ok, err := auth.VerifyPassword(password, passwordHash)
        if err != nil || !ok {
                loginLimiter.RecordFailedAttempt(ip)
                data.Error = "Invalid username or password."
                renderPage(w, r, "login.html", data)
                return
        }

        // Handle soft-deleted accounts (24-hour recovery window)
        if deletedAt.Valid {
                if time.Since(deletedAt.Time) < 24*time.Hour {
                        _ = db.RestoreUser(userID)
                } else {
                        loginLimiter.RecordFailedAttempt(ip)
                        data.Error = "User not found."
                        renderPage(w, r, "login.html", data)
                        return
                }
        }

        if isBanned {
                data.Error = "Your account has been banned."
                renderPage(w, r, "login.html", data)
                return
        }

        loginLimiter.ResetAttempts(ip)
        _ = db.UpdateLastLogin(userID)

        sessionID, err := session.Create(r.Context(), userID, username)
        if err != nil {
                log.Printf("session create error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        session.SetCookie(w, sessionID)

        _, _ = db.DB.ExecContext(r.Context(),
                `INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
                sessionID, userID, time.Now().Add(24*time.Hour),
        )
        http.Redirect(w, r, "/", http.StatusSeeOther)
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData != nil {
                http.Redirect(w, r, "/", http.StatusSeeOther)
                return
        }
        data := pageData(w, r)

        if r.Method == http.MethodGet {
                renderPage(w, r, "register.html", data)
                return
        }

        ip := clientIP(r)
        if registerLimiter.IsBlocked(ip) {
                data.Error = "Too many registrations from this address. Please try again later."
                w.WriteHeader(http.StatusTooManyRequests)
                renderPage(w, r, "register.html", data)
                return
        }
        registerLimiter.RecordFailedAttempt(ip)

        username := strings.TrimSpace(r.FormValue("username"))
        password := r.FormValue("password")
        confirmPassword := r.FormValue("confirm_password")

        if !usernameRe.MatchString(username) {
                data.Error = "Username must be 3–30 characters: letters, numbers, or underscore only."
                renderPage(w, r, "register.html", data)
                return
        }
        if len(password) < 8 {
                data.Error = "Password must be at least 8 characters."
                renderPage(w, r, "register.html", data)
                return
        }
        if password != confirmPassword {
                data.Error = "Passwords do not match."
                renderPage(w, r, "register.html", data)
                return
        }

        hash, err := auth.HashPassword(password)
        if err != nil {
                log.Printf("hash error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        var newUserID int
        err = db.DB.QueryRowContext(r.Context(),
                `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $1, $2) RETURNING id`,
                username, hash,
        ).Scan(&newUserID)

        if err != nil {
                if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
                        data.Error = "That username is already taken."
                        renderPage(w, r, "register.html", data)
                        return
                }
                log.Printf("register db error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        sessionID, err := session.Create(r.Context(), newUserID, username)
        if err != nil {
                log.Printf("session create error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        session.SetCookie(w, sessionID)
        _, _ = db.DB.ExecContext(r.Context(),
                `INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`,
                sessionID, newUserID, time.Now().Add(24*time.Hour),
        )
        http.Redirect(w, r, "/", http.StatusSeeOther)
}

func StatusCheckHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Cache-Control", "no-store")

        if sessData == nil {
                w.Write([]byte(`{"status":"ok","authenticated":false}`))
                return
        }

        banned, err := db.GetUserBanned(sessData.UserID)
        if err == sql.ErrNoRows {
                sessionID, _, _ := session.FromRequest(r)
                if sessionID != "" {
                        _ = session.Delete(r.Context(), sessionID)
                        _, _ = db.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = $1`, sessionID)
                }
                session.ClearCookie(w)
                w.Write([]byte(`{"status":"deleted","authenticated":true}`))
                return
        }
        if err != nil {
                w.Write([]byte(`{"status":"ok","authenticated":true}`))
                return
        }
        if banned {
                w.Write([]byte(`{"status":"banned","authenticated":true}`))
                return
        }
        w.Write([]byte(`{"status":"ok","authenticated":true}`))
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Redirect(w, r, "/", http.StatusSeeOther)
                return
        }
        sessionID, _, _ := session.FromRequest(r)
        if sessionID != "" {
                _ = session.Delete(r.Context(), sessionID)
                _, _ = db.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = $1`, sessionID)
        }
        session.ClearCookie(w)
        http.Redirect(w, r, "/", http.StatusSeeOther)
}
