package handlers

import (
        "context"
        "encoding/json"
        "fmt"
        "html/template"
        "log"
        "net/http"
        "path/filepath"
        "shadow/internal/auth"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/session"
        "strings"
        "time"
)

type SettingsData struct {
        Session          *session.Data
        CSRFToken        string
        Initials         string
        User             *db.User
        AvatarURL        string
        BannerURL        string
        LastActivity     string
        ShowOnlineStatus bool
        Payments         []db.UserPaymentRow
}

func parseSettingsTemplate() (*template.Template, error) {
        return template.New("settings").ParseFiles(
                filepath.Join("templates", "settings.html"),
        )
}

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        user, err := db.GetUserByID(sessData.UserID)
        if err != nil || user == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        initials := ""
        if len(user.Username) >= 2 {
                initials = strings.ToUpper(user.Username[:2])
        } else if len(user.Username) == 1 {
                initials = strings.ToUpper(user.Username[:1])
        }

        lastActivity := "Recently"
        if t := db.GetLastActivity(sessData.UserID); t != nil {
                d := time.Since(*t)
                switch {
                case d < time.Minute:
                        lastActivity = "Just now"
                case d < time.Hour:
                        lastActivity = fmt.Sprintf("%d min ago", int(d.Minutes()))
                case d < 24*time.Hour:
                        lastActivity = fmt.Sprintf("%d hours ago", int(d.Hours()))
                default:
                        lastActivity = t.Format("Jan 2, 2006")
                }
        }

        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        showOnline, _ := db.GetShowOnlineStatus(sessData.UserID)

        payCtx, payCancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer payCancel()
        payments, err := db.GetUserPaymentHistory(payCtx, sessData.UserID)
        if err != nil {
                log.Printf("[settings] payment history error for user %d: %v", sessData.UserID, err)
        }

        data := SettingsData{
                Session:          sessData,
                CSRFToken:        csrfToken,
                Initials:         initials,
                User:             user,
                AvatarURL:        assetURL(user.AvatarPath),
                BannerURL:        assetURL(user.BannerPath),
                LastActivity:     lastActivity,
                ShowOnlineStatus: showOnline,
                Payments:         payments,
        }

        tmpl, err := parseSettingsTemplate()
        if err != nil {
                log.Printf("settings template error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "settings", data); err != nil {
                log.Printf("settings render error: %v", err)
        }
}

func settingsJSONUpdate(w http.ResponseWriter, r *http.Request, fn func(int, string) error, maxLen int) {
        if r.Method != http.MethodPost {
                http.NotFound(w, r)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        r.ParseMultipartForm(1 << 20)
        val := stripHTMLTags(strings.TrimSpace(r.FormValue("value")))
        if maxLen > 0 && len(val) > maxLen {
                val = val[:maxLen]
        }
        if err := fn(sessData.UserID, val); err != nil {
                log.Printf("settings update: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func SettingsDisplayNameHandler(w http.ResponseWriter, r *http.Request) {
        settingsJSONUpdate(w, r, db.UpdateDisplayName, 100)
}

func SettingsBioHandler(w http.ResponseWriter, r *http.Request) {
        settingsJSONUpdate(w, r, db.UpdateBio, 300)
}

func SettingsEmailHandler(w http.ResponseWriter, r *http.Request) {
        settingsJSONUpdate(w, r, db.UpdateEmail, 200)
}

func SettingsAvatarHandler(w http.ResponseWriter, r *http.Request) {
        handleUpload(w, r, "avatar", "uploads/avatars", db.UpdateAvatarPath)
}

func SettingsBannerHandler(w http.ResponseWriter, r *http.Request) {
        handleUpload(w, r, "banner", "uploads/banners", db.UpdateBannerPath)
}

func SettingsRemoveAvatarHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.NotFound(w, r)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        if err := db.RemoveAvatarPath(sessData.UserID); err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func SettingsPrivacyHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.NotFound(w, r)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        r.ParseForm()
        show := r.FormValue("show_online_status") == "1"
        if err := db.UpdateShowOnlineStatus(sessData.UserID, show); err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func SettingsDeleteAccountHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.NotFound(w, r)
                return
        }
        sessionID, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        r.ParseMultipartForm(1 << 20)
        password := r.FormValue("password")
        if password == "" {
                http.Error(w, "Password is required", http.StatusBadRequest)
                return
        }
        hash, err := db.GetPasswordHash(sessData.UserID)
        if err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        ok, err := auth.VerifyPassword(password, hash)
        if err != nil || !ok {
                http.Error(w, "Incorrect password", http.StatusBadRequest)
                return
        }
        // Collect and delete all user sessions from Redis
        rows, qErr := db.DB.QueryContext(r.Context(), `SELECT id FROM sessions WHERE user_id = $1`, sessData.UserID)
        if qErr == nil {
                defer rows.Close()
                for rows.Next() {
                        var sid string
                        if rows.Scan(&sid) == nil {
                                _ = session.Delete(r.Context(), sid)
                        }
                }
        }
        // Soft-delete the account
        if err := db.SoftDeleteUser(sessData.UserID); err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        // Clear DB sessions and cookie
        _, _ = db.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = $1`, sessData.UserID)
        _ = sessionID
        session.ClearCookie(w)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]string{"redirect": "/login"})
}

func SettingsPasswordHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.NotFound(w, r)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        r.ParseMultipartForm(1 << 20)
        current := strings.TrimSpace(r.FormValue("current"))
        newPw := strings.TrimSpace(r.FormValue("new"))
        confirm := strings.TrimSpace(r.FormValue("confirm"))

        if current == "" || newPw == "" || confirm == "" {
                http.Error(w, "All fields are required", http.StatusBadRequest)
                return
        }
        if newPw != confirm {
                http.Error(w, "New passwords do not match", http.StatusBadRequest)
                return
        }
        if len(newPw) < 8 {
                http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
                return
        }

        hash, err := db.GetPasswordHash(sessData.UserID)
        if err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        ok, err := auth.VerifyPassword(current, hash)
        if err != nil || !ok {
                http.Error(w, "Current password is incorrect", http.StatusBadRequest)
                return
        }
        newHash, err := auth.HashPassword(newPw)
        if err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        if err := db.UpdatePassword(sessData.UserID, newHash); err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
