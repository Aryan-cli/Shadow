package handlers

import (
        "database/sql"
        "fmt"
        "html/template"
        "net/http"
        "path/filepath"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/session"
        "strings"
)

type PageData struct {
        Session   *session.Data
        CSRFToken string
        Initials  string
        AvatarURL string
        Error     string
        Success   string
        Banned    bool
}

func parseTemplates(names ...string) (*template.Template, error) {
        funcMap := template.FuncMap{
                "upper": strings.ToUpper,
                "add1":  func(i int) int { return i + 1 },
                "truncate": func(s string, n int) string {
                        if len(s) <= n {
                                return s
                        }
                        return s[:n] + "…"
                },
                "seq": func(n int) []int {
                        s := make([]int, n)
                        for i := range s {
                                s[i] = i
                        }
                        return s
                },
                "list": func(v ...interface{}) []interface{} {
                        return v
                },
                "formatPrice": func(p float64) string {
                        return fmt.Sprintf("%.2f", p)
                },
                "trimStaticPrefix": func(path string) string {
                        return strings.TrimPrefix(path, "static/")
                },
                "int": func(f float64) int {
                        return int(f + 0.5)
                },
        }
        files := make([]string, 0, len(names)+1)
        files = append(files, filepath.Join("templates", "base.html"))
        for _, n := range names {
                files = append(files, filepath.Join("templates", n))
        }
        return template.New("base").Funcs(funcMap).ParseFiles(files...)
}

func pageData(w http.ResponseWriter, r *http.Request) PageData {
        _, sessData, _ := session.FromRequest(r)
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        initials := ""
        avatarURL := ""
        banned := false

        if sessData != nil {
                if len(sessData.Username) >= 2 {
                        initials = strings.ToUpper(sessData.Username[:2])
                } else if len(sessData.Username) == 1 {
                        initials = strings.ToUpper(sessData.Username[:1])
                }
                if u, err := db.GetUserByID(sessData.UserID); err == nil && u != nil {
                        avatarURL = assetURL(u.AvatarPath)
                }
                isBanned, err := db.GetUserBanned(sessData.UserID)
                if err == sql.ErrNoRows {
                        sessionID, _, _ := session.FromRequest(r)
                        if sessionID != "" {
                                _ = session.Delete(r.Context(), sessionID)
                                _, _ = db.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = $1`, sessionID)
                        }
                        session.ClearCookie(w)
                        sessData = nil
                } else if err == nil && isBanned {
                        banned = true
                }
        }

        return PageData{Session: sessData, CSRFToken: csrfToken, Initials: initials, AvatarURL: avatarURL, Banned: banned}
}

func HomeHandler(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
                http.NotFound(w, r)
                return
        }
        data := pageData(w, r)
        tmpl, err := parseTemplates("home.html")
        if err != nil {
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
                http.Error(w, "Template error", http.StatusInternalServerError)
        }
}
