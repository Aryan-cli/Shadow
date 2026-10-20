package handlers

import (
        "fmt"
        "html/template"
        "io"
        "log"
        "net/http"
        "os"
        "path/filepath"
        "regexp"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/session"
        "strconv"
        "strings"
        "time"
)

var htmlTagRe = regexp.MustCompile(`<[a-zA-Z/][^>]{0,200}>`)

func stripHTMLTags(s string) string {
        return strings.TrimSpace(htmlTagRe.ReplaceAllString(s, ""))
}

type ProfilePageData struct {
        Session      *session.Data
        CSRFToken    string
        Initials     string
        NavInitials  string
        NavAvatarURL string
        User         *db.User
        IsOwnProfile bool
        AvatarURL    string
        BannerURL    string
        Error        string
        Success      string
        FriendStatus string
        OnlineStatus string
        LastSeenText string
}

func computeUserStatus(user *db.User) (status, text string) {
        if user.LastSeenAt == nil {
                return "offline", "never been online"
        }
        diff := time.Since(*user.LastSeenAt)
        onlineSec := db.GetSiteSettingInt("online_threshold_seconds", 30)
        idleSec := db.GetSiteSettingInt("idle_threshold_seconds", 600)

        if !user.ShowOnlineStatus {
                if diff < 24*time.Hour {
                        return "hidden", "seen just now"
                }
                return "hidden", "a while ago"
        }

        if diff < time.Duration(onlineSec)*time.Second {
                return "online", "online"
        }
        if diff < time.Duration(idleSec)*time.Second {
                return "idle", "active recently"
        }
        if diff < 2*time.Hour {
                mins := int(diff.Minutes())
                return "offline", "seen " + strconv.Itoa(mins) + "m ago"
        }
        if diff < 24*time.Hour {
                return "offline", "seen today at " + user.LastSeenAt.Local().Format("15:04")
        }
        if diff < 48*time.Hour {
                return "offline", "seen yesterday"
        }
        return "offline", "seen " + user.LastSeenAt.Local().Format("Jan 2")
}

func profileInitials(username string) string {
        if len(username) >= 2 {
                return strings.ToUpper(username[:2])
        } else if len(username) == 1 {
                return strings.ToUpper(username[:1])
        }
        return "??"
}

func assetURL(path string) string {
        if path == "" {
                return ""
        }
        return "/static/" + strings.TrimPrefix(path, "static/")
}

func navFieldsForViewer(sessData *session.Data) (navInitials, navAvatarURL string) {
        if sessData == nil {
                return "", ""
        }
        navInitials = profileInitials(sessData.Username)
        viewer, err := db.GetUserByID(sessData.UserID)
        if err == nil && viewer != nil {
                navAvatarURL = assetURL(viewer.AvatarPath)
        }
        return
}

func ProfileHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        username := strings.TrimPrefix(r.URL.Path, "/profile")
        username = strings.TrimPrefix(username, "/")

        if username == "" {
                http.Redirect(w, r, "/profile/"+sessData.Username, http.StatusSeeOther)
                return
        }

        user, err := db.GetUserByUsername(username)
        if err != nil || user == nil {
                http.NotFound(w, r)
                return
        }

        isOwn := sessData.Username == user.Username
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        navInitials, navAvatarURL := navFieldsForViewer(sessData)

        friendStatus := ""
        if !isOwn && sessData != nil {
                friendStatus, _ = db.FriendshipStatus(sessData.UserID, user.ID)
        }
        onlineStatus, lastSeenText := computeUserStatus(user)

        data := ProfilePageData{
                Session:      sessData,
                CSRFToken:    csrfToken,
                Initials:     profileInitials(user.Username),
                NavInitials:  navInitials,
                NavAvatarURL: navAvatarURL,
                User:         user,
                IsOwnProfile: isOwn,
                AvatarURL:    assetURL(user.AvatarPath),
                BannerURL:    assetURL(user.BannerPath),
                FriendStatus: friendStatus,
                OnlineStatus: onlineStatus,
                LastSeenText: lastSeenText,
        }

        tmpl, err := parseProfileTemplate()
        if err != nil {
                log.Printf("profile template error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "profile", data); err != nil {
                log.Printf("profile render error: %v", err)
        }
}

func renderUserNotFound(w http.ResponseWriter, r *http.Request, username string) {
        _, sessData, _ := session.FromRequest(r)
        csrfToken := ""
        if sessData != nil {
                csrfToken = middleware.GetOrCreateCSRFToken(w, r)
        }
        navInitials, navAvatarURL := navFieldsForViewer(sessData)

        data := struct {
                Session      *session.Data
                CSRFToken    string
                Initials     string
                NavInitials  string
                NavAvatarURL string
                AvatarURL    string
                Username     string
        }{
                Session:      sessData,
                CSRFToken:    csrfToken,
                NavInitials:  navInitials,
                NavAvatarURL: navAvatarURL,
                Username:     username,
        }

        tmpl, err := template.New("not-found").ParseFiles(
                filepath.Join("templates", "base.html"),
                filepath.Join("templates", "not-found.html"),
        )
        if err != nil {
                log.Printf("not-found template error: %v", err)
                http.NotFound(w, r)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.WriteHeader(http.StatusNotFound)
        if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
                log.Printf("not-found render error: %v", err)
        }
}

func UserProfileHandler(w http.ResponseWriter, r *http.Request) {
        username := strings.TrimPrefix(r.URL.Path, "/user/")
        username = strings.TrimPrefix(username, "/")
        if username == "" {
                http.Redirect(w, r, "/", http.StatusSeeOther)
                return
        }
        if !usernameRe.MatchString(username) {
                renderUserNotFound(w, r, username)
                return
        }

        user, err := db.GetUserByUsername(username)
        if err != nil || user == nil {
                renderUserNotFound(w, r, username)
                return
        }

        _, sessData, _ := session.FromRequest(r)
        isOwn := sessData != nil && sessData.Username == user.Username

        csrfToken := ""
        if sessData != nil {
                csrfToken = middleware.GetOrCreateCSRFToken(w, r)
        }
        navInitials, navAvatarURL := navFieldsForViewer(sessData)

        fStatus := ""
        if !isOwn && sessData != nil {
                fStatus, _ = db.FriendshipStatus(sessData.UserID, user.ID)
        }
        oStatus, lsText := computeUserStatus(user)

        data := ProfilePageData{
                Session:      sessData,
                CSRFToken:    csrfToken,
                Initials:     profileInitials(user.Username),
                NavInitials:  navInitials,
                NavAvatarURL: navAvatarURL,
                User:         user,
                IsOwnProfile: isOwn,
                AvatarURL:    assetURL(user.AvatarPath),
                BannerURL:    assetURL(user.BannerPath),
                FriendStatus: fStatus,
                OnlineStatus: oStatus,
                LastSeenText: lsText,
        }

        tmpl, err := parseProfileTemplate()
        if err != nil {
                log.Printf("user profile template error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "profile", data); err != nil {
                log.Printf("user profile render error: %v", err)
        }
}

func ProfileUpdateHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.NotFound(w, r)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        r.ParseForm()
        displayName := stripHTMLTags(strings.TrimSpace(r.FormValue("display_name")))
        bio := stripHTMLTags(strings.TrimSpace(r.FormValue("bio")))

        if len(displayName) > 100 {
                displayName = displayName[:100]
        }
        if len(bio) > 300 {
                bio = bio[:300]
        }

        if displayName != "" {
                if err := db.UpdateDisplayName(sessData.UserID, displayName); err != nil {
                        log.Printf("update display_name: %v", err)
                }
        }
        if err := db.UpdateBio(sessData.UserID, bio); err != nil {
                log.Printf("update bio: %v", err)
        }

        http.Redirect(w, r, "/profile/"+sessData.Username, http.StatusSeeOther)
}

func ProfileAvatarHandler(w http.ResponseWriter, r *http.Request) {
        handleUpload(w, r, "avatar", "uploads/avatars", db.UpdateAvatarPath)
}

func ProfileBannerHandler(w http.ResponseWriter, r *http.Request) {
        handleUpload(w, r, "banner", "uploads/banners", db.UpdateBannerPath)
}

var allowedImageMIME = map[string]bool{
        "image/jpeg": true,
        "image/png":  true,
        "image/gif":  true,
        "image/webp": true,
}

func handleUpload(w http.ResponseWriter, r *http.Request, field, dir string, updateFn func(int, string) error) {
        if r.Method != http.MethodPost {
                http.NotFound(w, r)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
        if err := r.ParseMultipartForm(20 << 20); err != nil {
                http.Error(w, "File too large (max 20MB)", http.StatusBadRequest)
                return
        }

        file, header, err := r.FormFile(field)
        if err != nil {
                http.Error(w, "No file provided", http.StatusBadRequest)
                return
        }
        defer file.Close()

        ext := strings.ToLower(filepath.Ext(header.Filename))
        allowedExt := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
        if !allowedExt[ext] {
                http.Error(w, "Only jpg/png/gif/webp allowed", http.StatusBadRequest)
                return
        }

        headerBuf := make([]byte, 512)
        n, err := file.Read(headerBuf)
        if err != nil && err != io.EOF {
                http.Error(w, "Server error reading file", http.StatusInternalServerError)
                return
        }
        detectedMIME := http.DetectContentType(headerBuf[:n])
        if !allowedImageMIME[detectedMIME] {
                http.Error(w, "File content is not a valid image", http.StatusBadRequest)
                return
        }
        if _, err := file.Seek(0, io.SeekStart); err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        if err := os.MkdirAll("static/"+dir, 0755); err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        filename := fmt.Sprintf("%d_%d%s", sessData.UserID, time.Now().UnixNano(), ext)
        savePath := filepath.Join("static", dir, filename)

        dst, err := os.Create(savePath)
        if err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        defer dst.Close()

        if _, err := io.Copy(dst, file); err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        dbPath := filepath.Join("static", dir, filename)
        if err := updateFn(sessData.UserID, dbPath); err != nil {
                log.Printf("db update %s: %v", field, err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        fmt.Fprintf(w, `{"url":"/static/%s/%s"}`, dir, filename)
}

func parseProfileTemplate() (*template.Template, error) {
        return template.New("profile").ParseFiles(
                filepath.Join("templates", "profile.html"),
        )
}
