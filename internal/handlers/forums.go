package handlers

import (
        "database/sql"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "sync"
        "time"

        "golang.org/x/time/rate"
        "shadow/internal/db"
        "shadow/internal/session"
)

// ── Per-IP rate limiters for content creation ─────────────────────────────────

var (
        postRLMu   sync.Mutex
        postRL     = make(map[string]*rate.Limiter) // 5 posts / min
        commentRLMu sync.Mutex
        commentRL  = make(map[string]*rate.Limiter) // 20 comments / min
        serverRLMu sync.Mutex
        serverRL   = make(map[string]*rate.Limiter) // 2 servers / hour
)

func getPostLimiter(ip string) *rate.Limiter {
        postRLMu.Lock()
        defer postRLMu.Unlock()
        if l, ok := postRL[ip]; ok {
                return l
        }
        l := rate.NewLimiter(rate.Every(12*time.Second), 5)
        postRL[ip] = l
        return l
}

func getCommentLimiter(ip string) *rate.Limiter {
        commentRLMu.Lock()
        defer commentRLMu.Unlock()
        if l, ok := commentRL[ip]; ok {
                return l
        }
        l := rate.NewLimiter(rate.Every(3*time.Second), 20)
        commentRL[ip] = l
        return l
}

func getServerLimiter(ip string) *rate.Limiter {
        serverRLMu.Lock()
        defer serverRLMu.Unlock()
        if l, ok := serverRL[ip]; ok {
                return l
        }
        l := rate.NewLimiter(rate.Every(30*time.Minute), 2)
        serverRL[ip] = l
        return l
}

type ForumsPageData struct {
        PageData
        Posts         []db.ForumPost
        Categories    []db.ForumCategory
        TopCategories []db.ForumCategory
        Hashtags      []db.ForumCategory
        Servers       []db.ForumServer
        FollowedSrvs  []db.ForumServer
        OnlineUsers   []db.OnlineUser
        Stats         db.ForumStats
        ActiveCat     string
        ActiveServer  int
        ActiveHashtag string
        SearchQuery   string
        SearchFilter  string
        HasMore       bool
        CurrentPage   int
        FollowedUsers []db.User
        TimeAgo       func(time.Time) string
        CreateError   string
}

type ForumPostPageData struct {
        PageData
        Post     *db.ForumPost
        Comments []db.ForumComment
        TimeAgo  func(time.Time) string
}

type ForumServerPageData struct {
        PageData
        Server        *db.ForumServer
        Posts         []db.ForumPost
        Members       []db.ServerMember
        Categories    []db.ForumCategory
        FollowedSrvs  []db.ForumServer
        FollowedUsers []db.User
        HasMore       bool
        CurrentPage   int
        TimeAgo       func(time.Time) string
        ViaInvite     bool
}

type ForumUserProfilePageData struct {
        PageData
        ProfileUser  *db.User
        Posts        []db.ForumPost
        Replies      []db.ForumComment
        PostCount    int
        ReplyCount   int
        TotalScore   int
        IsFollowing  bool
        Tab          string
        HasMore      bool
        CurrentPage  int
        PrevPage     int
        TimeAgo      func(time.Time) string
}

func timeAgo(t time.Time) string {
        diff := time.Since(t)
        switch {
        case diff < time.Minute:
                return "just now"
        case diff < time.Hour:
                m := int(diff.Minutes())
                if m == 1 {
                        return "1 min ago"
                }
                return strconv.Itoa(m) + " min ago"
        case diff < 24*time.Hour:
                h := int(diff.Hours())
                if h == 1 {
                        return "1 hr ago"
                }
                return strconv.Itoa(h) + " hr ago"
        case diff < 7*24*time.Hour:
                d := int(diff.Hours() / 24)
                if d == 1 {
                        return "1 day ago"
                }
                return strconv.Itoa(d) + " days ago"
        default:
                return t.Format("Jan 2, 2006")
        }
}

func ForumsHandler(w http.ResponseWriter, r *http.Request) {
        base := pageData(w, r)
        ctx := r.Context()

        activeCat := r.URL.Query().Get("cat")
        activeHashtag := r.URL.Query().Get("tag")
        search := r.URL.Query().Get("q")
        searchFilter := r.URL.Query().Get("filter")
        serverIDStr := r.URL.Query().Get("server")
        serverID, _ := strconv.Atoi(serverIDStr)

        pageStr := r.URL.Query().Get("page")
        page, _ := strconv.Atoi(pageStr)
        if page < 1 {
                page = 1
        }
        limit := 20
        offset := (page - 1) * limit

        viewerID := 0
        if base.Session != nil {
                viewerID = base.Session.UserID
        }

        sortBy := r.URL.Query().Get("sort")
        dateFilter := r.URL.Query().Get("date")
        posts, err := db.GetForumPosts(ctx, activeCat, serverID, activeHashtag, search, viewerID, limit+1, offset, sortBy, dateFilter)
        if err != nil {
                posts = []db.ForumPost{}
        }
        hasMore := len(posts) > limit
        if hasMore {
                posts = posts[:limit]
        }

        cats, _ := db.GetAllCategories(ctx)
        topCats, _ := db.GetTopCategories(ctx, 5)
        hashtags, _ := db.GetHashtags(ctx, 10)
        stats, _ := db.GetForumStats(ctx)
        online, _ := db.GetOnlineUsersForForum(ctx, 5)

        var followedSrvs []db.ForumServer
        var followedUsers []db.User
        var allServers []db.ForumServer
        if base.Session != nil {
                followedSrvs, _ = db.GetFollowedServers(ctx, base.Session.UserID)
                allServers, _ = db.GetAllForumServers(ctx, base.Session.UserID)
                followedUsers, _ = db.GetFollowedUsers(ctx, base.Session.UserID)
        } else {
                allServers, _ = db.GetAllForumServers(ctx, 0)
        }

        data := ForumsPageData{
                PageData:      base,
                Posts:         posts,
                Categories:    cats,
                TopCategories: topCats,
                Hashtags:      hashtags,
                Servers:       allServers,
                FollowedSrvs:  followedSrvs,
                FollowedUsers: followedUsers,
                OnlineUsers:   online,
                Stats:         stats,
                ActiveCat:     activeCat,
                ActiveServer:  serverID,
                ActiveHashtag: activeHashtag,
                SearchQuery:   search,
                SearchFilter:  searchFilter,
                HasMore:       hasMore,
                CurrentPage:   page,
                TimeAgo:       timeAgo,
                CreateError:   "",
        }

        tmpl, err := parseTemplates("forums.html")
        if err != nil {
                http.Error(w, "Template error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
                http.Error(w, "Template execute error", http.StatusInternalServerError)
        }
}

func ForumsPostCreateHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
        if err := r.ParseMultipartForm(12 << 20); err != nil {
                // fall back to regular form
                _ = r.ParseForm()
        }

        // ── Rate limit: 5 posts per minute per IP ────────────────────────────────
        if !getPostLimiter(clientIP(r)).Allow() {
                http.Error(w, "Too many posts — please slow down", http.StatusTooManyRequests)
                return
        }

        title := strings.TrimSpace(r.FormValue("title"))
        body := strings.TrimSpace(r.FormValue("body"))
        catSlug := strings.TrimSpace(r.FormValue("category"))
        tagsRaw := strings.TrimSpace(r.FormValue("hashtags"))
        serverIDStr := strings.TrimSpace(r.FormValue("server_id"))

        if title == "" {
                http.Redirect(w, r, "/forums?err=title_required", http.StatusSeeOther)
                return
        }
        if len(title) > 300 {
                http.Error(w, "title too long (max 300 characters)", http.StatusBadRequest)
                return
        }
        if len(body) > 20000 {
                http.Error(w, "body too long (max 20,000 characters)", http.StatusBadRequest)
                return
        }

        ctx := r.Context()

        var categoryID sql.NullInt64
        if catSlug != "" {
                cat, err := db.GetCategoryBySlug(ctx, catSlug)
                if err == nil {
                        categoryID = sql.NullInt64{Int64: int64(cat.ID), Valid: true}
                }
        }

        var serverID sql.NullInt64
        if sID, err := strconv.Atoi(serverIDStr); err == nil && sID > 0 {
                // Always verify the server exists and check permissions server-side
                sv, svErr := db.GetServerByID(ctx, sID, sess.UserID)
                if svErr != nil {
                        http.Redirect(w, r, "/forums?err=invalid_server", http.StatusSeeOther)
                        return
                }
                serverID = sql.NullInt64{Int64: int64(sv.ID), Valid: true}
                if !sv.FreeForAll && sv.UserRole != "owner" && sv.UserRole != "partner" {
                        http.Redirect(w, r, fmt.Sprintf("/forums/server/%d?err=no_permission", sv.PublicID), http.StatusSeeOther)
                        return
                }
        }

        // Handle optional image upload
        imageURL := ""
        if file, header, err := r.FormFile("image"); err == nil {
                defer file.Close()
                ext := strings.ToLower(filepath.Ext(header.Filename))
                allowedExt := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
                if allowedExt[ext] {
                        headerBuf := make([]byte, 512)
                        n, _ := file.Read(headerBuf)
                        detectedMIME := http.DetectContentType(headerBuf[:n])
                        allowedMIME := map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}
                        if allowedMIME[detectedMIME] {
                                if _, err2 := file.Seek(0, io.SeekStart); err2 == nil {
                                        if err3 := os.MkdirAll("static/uploads/forum", 0755); err3 == nil {
                                                filename := fmt.Sprintf("%d_%d%s", sess.UserID, time.Now().UnixNano(), ext)
                                                savePath := filepath.Join("static", "uploads", "forum", filename)
                                                if dst, err4 := os.Create(savePath); err4 == nil {
                                                        if _, err5 := io.Copy(dst, file); err5 == nil {
                                                                imageURL = "/static/uploads/forum/" + filename
                                                        }
                                                        dst.Close()
                                                }
                                        }
                                }
                        }
                }
        }

        postID, err := db.CreateForumPost(ctx, sess.UserID, serverID, categoryID, title, body, imageURL)
        if err != nil {
                http.Redirect(w, r, "/forums?err=post_failed", http.StatusSeeOther)
                return
        }

        var tags []string
        seen := make(map[string]bool)
        for _, t := range strings.Split(tagsRaw, ",") {
                t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
                if t == "" || seen[t] {
                        continue
                }
                if len(t) > 50 {
                        t = t[:50]
                }
                seen[t] = true
                tags = append(tags, t)
        }
        for _, word := range strings.Fields(body) {
                if strings.HasPrefix(word, "#") {
                        t := strings.TrimPrefix(word, "#")
                        t = strings.Trim(t, ".,!?;:")
                        if t == "" || seen[t] {
                                continue
                        }
                        if len(t) > 50 {
                                t = t[:50]
                        }
                        seen[t] = true
                        tags = append(tags, t)
                }
        }
        // Cap total hashtags at 10 per post
        if len(tags) > 10 {
                tags = tags[:10]
        }
        if len(tags) > 0 {
                _ = db.AttachHashtagsToPost(ctx, postID, tags)
        }

        http.Redirect(w, r, "/forums/post/"+strconv.Itoa(postID), http.StatusSeeOther)
}

func ForumsPostDetailHandler(w http.ResponseWriter, r *http.Request) {
        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/forums/post/"), "/")
        postIDStr := parts[0]

        // Handle comment create sub-route: /forums/post/{id}/comment
        if len(parts) >= 2 && parts[1] == "comment" {
                ForumsCommentCreateHandler(w, r)
                return
        }

        postID, err := strconv.Atoi(postIDStr)
        if err != nil || postID <= 0 {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }

        base := pageData(w, r)
        ctx := r.Context()

        viewerID := 0
        if base.Session != nil {
                viewerID = base.Session.UserID
        }

        post, err := db.GetPostByID(ctx, postID, viewerID)
        if err != nil {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }

        // Increment view count (fire and forget)
        go db.IncrementPostView(ctx, postID)

        comments, _ := db.GetPostComments(ctx, postID, viewerID)

        data := ForumPostPageData{
                PageData: base,
                Post:     post,
                Comments: comments,
                TimeAgo:  timeAgo,
        }

        tmpl, err := parseTemplates("forums-post.html")
        if err != nil {
                http.Error(w, "Template error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
                http.Error(w, "Template execute error", http.StatusInternalServerError)
        }
}

func ForumsCommentCreateHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/forums/post/"), "/")
        postIDStr := parts[0]
        postID, err := strconv.Atoi(postIDStr)
        if err != nil || postID <= 0 {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }

        // ── Rate limit: 20 comments per minute per IP ────────────────────────────
        if !getCommentLimiter(clientIP(r)).Allow() {
                http.Error(w, "Too many replies — please slow down", http.StatusTooManyRequests)
                return
        }

        if err := r.ParseForm(); err != nil {
                http.Redirect(w, r, "/forums/post/"+postIDStr, http.StatusSeeOther)
                return
        }
        body := strings.TrimSpace(r.FormValue("body"))
        if body == "" {
                http.Redirect(w, r, "/forums/post/"+postIDStr, http.StatusSeeOther)
                return
        }
        if len(body) > 5000 {
                http.Error(w, "reply too long (max 5,000 characters)", http.StatusBadRequest)
                return
        }

        parentIDStr := r.FormValue("parent_id")
        var parentID sql.NullInt64
        if pid, err := strconv.Atoi(parentIDStr); err == nil && pid > 0 {
                parentID = sql.NullInt64{Int64: int64(pid), Valid: true}
        }

        _, err = db.CreateComment(r.Context(), postID, sess.UserID, body, parentID)
        if err != nil {
                http.Redirect(w, r, "/forums/post/"+postIDStr, http.StatusSeeOther)
                return
        }
        http.Redirect(w, r, "/forums/post/"+postIDStr+"#comments", http.StatusSeeOther)
}

func ForumsVoteHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"error": "not logged in"})
                return
        }

        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/post/"), "/")
        postIDStr := parts[0]
        postID, err := strconv.Atoi(postIDStr)
        if err != nil || postID <= 0 {
                http.Error(w, "invalid post id", http.StatusBadRequest)
                return
        }

        action := ""
        if len(parts) >= 2 {
                action = parts[1]
        }

        if action == "delete" {
                if err := db.DeleteForumPost(r.Context(), postID, sess.UserID); err != nil {
                        http.Error(w, "delete failed", http.StatusForbidden)
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
                return
        }

        if action == "edit" {
                if err := r.ParseForm(); err != nil {
                        http.Error(w, "bad request", http.StatusBadRequest)
                        return
                }
                title := strings.TrimSpace(r.FormValue("title"))
                body := strings.TrimSpace(r.FormValue("body"))
                if title == "" {
                        http.Error(w, "title required", http.StatusBadRequest)
                        return
                }
                if len(title) > 300 {
                        http.Error(w, "title too long (max 300 characters)", http.StatusBadRequest)
                        return
                }
                if len(body) > 20000 {
                        http.Error(w, "body too long (max 20,000 characters)", http.StatusBadRequest)
                        return
                }
                if err := db.UpdateForumPost(r.Context(), postID, sess.UserID, title, body); err != nil {
                        http.Error(w, "edit failed", http.StatusForbidden)
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "title": title, "body": body})
                return
        }

        if err := r.ParseForm(); err != nil {
                http.Error(w, "bad request", http.StatusBadRequest)
                return
        }
        value, _ := strconv.Atoi(r.FormValue("value"))
        if value != -1 && value != 1 && value != 0 {
                value = 0
        }

        score, err := db.VoteOnPost(r.Context(), postID, sess.UserID, value)
        if err != nil {
                http.Error(w, "vote failed", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{
                "score":     score,
                "user_vote": value,
        })
}

func ForumsCategoryCreateHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        if err := r.ParseForm(); err != nil {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        name := strings.TrimSpace(r.FormValue("name"))
        if name == "" {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }

        cat, err := db.CreateCategory(r.Context(), name, sess.UserID)
        if err != nil {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        http.Redirect(w, r, "/forums?cat="+cat.Slug, http.StatusSeeOther)
}

func ForumsServerCreateHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        // ── Rate limit: 2 servers per hour per IP ─────────────────────────────────
        if !getServerLimiter(clientIP(r)).Allow() {
                isAJAX := r.Header.Get("X-Requested-With") == "XMLHttpRequest"
                if isAJAX {
                        w.Header().Set("Content-Type", "application/json")
                        json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "Too many servers created — please wait before trying again."})
                } else {
                        http.Error(w, "Too many requests", http.StatusTooManyRequests)
                }
                return
        }

        if err := r.ParseForm(); err != nil {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        name := strings.TrimSpace(r.FormValue("name"))
        desc := strings.TrimSpace(r.FormValue("description"))
        handle := strings.TrimSpace(r.FormValue("handle"))
        if name == "" {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }

        isAJAX := r.Header.Get("X-Requested-With") == "XMLHttpRequest"

        if len(name) > 80 {
                if isAJAX {
                        w.Header().Set("Content-Type", "application/json")
                        json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "Server name must be 80 characters or fewer."})
                } else {
                        http.Redirect(w, r, "/forums", http.StatusSeeOther)
                }
                return
        }
        if len(desc) > 500 {
                if isAJAX {
                        w.Header().Set("Content-Type", "application/json")
                        json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "Description must be 500 characters or fewer."})
                } else {
                        http.Redirect(w, r, "/forums", http.StatusSeeOther)
                }
                return
        }
        if len(handle) > 50 {
                if isAJAX {
                        w.Header().Set("Content-Type", "application/json")
                        json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "Handle must be 50 characters or fewer."})
                } else {
                        http.Redirect(w, r, "/forums", http.StatusSeeOther)
                }
                return
        }
        sv, err := db.CreateForumServer(r.Context(), name, desc, handle, sess.UserID)
        if err != nil {
                msg := "Server could not be created. Please try again."
                if strings.Contains(err.Error(), "handle already exists") {
                        msg = "This handle is already taken. Please choose a different @handle."
                } else if strings.Contains(err.Error(), "already exists") {
                        msg = "A server with this name already exists. Please choose a different name."
                }
                if isAJAX {
                        w.Header().Set("Content-Type", "application/json")
                        json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": msg})
                        return
                }
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        if isAJAX {
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "redirect": fmt.Sprintf("/forums/server/%d", sv.PublicID)})
                return
        }
        http.Redirect(w, r, fmt.Sprintf("/forums/server/%d", sv.PublicID), http.StatusSeeOther)
}

func ForumsServerFollowHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"error": "not logged in"})
                return
        }

        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/server/"), "/")
        serverIDStr := parts[0]
        serverID, err := strconv.Atoi(serverIDStr)
        if err != nil || serverID <= 0 {
                http.Error(w, "invalid server id", http.StatusBadRequest)
                return
        }

        following, err := db.ToggleServerFollow(r.Context(), serverID, sess.UserID)
        if err != nil {
                http.Error(w, "toggle failed", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"following": following})
}

func ForumsUserFollowHandler(w http.ResponseWriter, r *http.Request) {
        path := strings.TrimPrefix(r.URL.Path, "/api/forums/user/")
        parts := strings.Split(strings.Trim(path, "/"), "/")
        if len(parts) >= 2 && parts[1] == "popup" {
                ForumsUserPopupHandler(w, r)
                return
        }
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"error": "not logged in"})
                return
        }

        targetUsername := parts[0]
        if targetUsername == "" {
                http.Error(w, "invalid username", http.StatusBadRequest)
                return
        }

        targetUser, err := db.GetUserByUsername(targetUsername)
        if err != nil || targetUser == nil {
                http.Error(w, "user not found", http.StatusNotFound)
                return
        }
        if targetUser.ID == sess.UserID {
                http.Error(w, "cannot follow yourself", http.StatusBadRequest)
                return
        }

        following, err := db.ToggleUserFollow(r.Context(), sess.UserID, targetUser.ID)
        if err != nil {
                http.Error(w, "toggle failed", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"following": following})
}

// ── Server Page ──────────────────────────────────────────────────────────────

// GET /invite/{token} — redirects to server page
func InviteHandler(w http.ResponseWriter, r *http.Request) {
        token := strings.TrimPrefix(r.URL.Path, "/invite/")
        token = strings.Trim(token, "/")
        if token == "" {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        ctx := r.Context()
        viewerID := 0
        if _, sess, _ := session.FromRequest(r); sess != nil {
                viewerID = sess.UserID
        }
        sv, err := db.GetServerByInviteToken(ctx, token, viewerID)
        if err != nil {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        http.Redirect(w, r, fmt.Sprintf("/forums/server/%d?ref=invite", sv.PublicID), http.StatusSeeOther)
}

func ForumsServerPageHandler(w http.ResponseWriter, r *http.Request) {
        raw := strings.TrimPrefix(r.URL.Path, "/forums/server/")
        raw = strings.Trim(raw, "/")
        if raw == "" {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }
        // detect sub-paths like /forums/server/{id}/settings
        parts := strings.SplitN(raw, "/", 2)
        idOrSlug := parts[0]
        subPath := ""
        if len(parts) >= 2 {
                subPath = parts[1]
        }

        base := pageData(w, r)
        ctx := r.Context()

        viewerID := 0
        if base.Session != nil {
                viewerID = base.Session.UserID
        }

        // resolve server: 10-digit public_id directly, or handle → redirect to canonical
        var sv *db.ForumServer
        var err error
        pubID, numErr := strconv.ParseInt(idOrSlug, 10, 64)
        if numErr == nil && pubID >= 1000000000 {
                sv, err = db.GetServerByPublicID(ctx, pubID, viewerID)
                if err != nil {
                        http.Redirect(w, r, "/forums", http.StatusSeeOther)
                        return
                }
        } else {
                // treat as handle — resolve and 301 redirect to canonical public_id URL
                sv, err = db.GetServerByHandle(ctx, idOrSlug, viewerID)
                if err != nil {
                        http.Redirect(w, r, "/forums", http.StatusSeeOther)
                        return
                }
                canonicalPath := fmt.Sprintf("/forums/server/%d", sv.PublicID)
                if subPath != "" {
                        canonicalPath += "/" + subPath
                }
                if r.URL.RawQuery != "" {
                        canonicalPath += "?" + r.URL.RawQuery
                }
                http.Redirect(w, r, canonicalPath, http.StatusMovedPermanently)
                return
        }

        if subPath == "settings" {
                // redirect to server page using numeric ID
                http.Redirect(w, r, fmt.Sprintf("/forums/server/%d", sv.PublicID), http.StatusSeeOther)
                return
        }

        pageStr := r.URL.Query().Get("page")
        page, _ := strconv.Atoi(pageStr)
        if page < 1 {
                page = 1
        }
        limit := 20
        offset := (page - 1) * limit

        posts, _ := db.GetForumPosts(ctx, "", sv.ID, "", "", viewerID, limit+1, offset, "", "")
        hasMore := len(posts) > limit
        if hasMore {
                posts = posts[:limit]
        }

        members, _ := db.GetServerMembers(ctx, sv.ID)
        cats, _ := db.GetAllCategories(ctx)

        var followedSrvs []db.ForumServer
        var followedUsers []db.User
        if base.Session != nil {
                followedSrvs, _ = db.GetFollowedServers(ctx, base.Session.UserID)
                followedUsers, _ = db.GetFollowedUsers(ctx, base.Session.UserID)
        }

        viaInvite := r.URL.Query().Get("ref") == "invite"
        data := ForumServerPageData{
                PageData:      base,
                Server:        sv,
                Posts:         posts,
                Members:       members,
                Categories:    cats,
                FollowedSrvs:  followedSrvs,
                FollowedUsers: followedUsers,
                HasMore:       hasMore,
                CurrentPage:   page,
                TimeAgo:       timeAgo,
                ViaInvite:     viaInvite,
        }

        tmpl, err := parseTemplates("forums-server.html")
        if err != nil {
                http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
                http.Error(w, "Template execute error: "+err.Error(), http.StatusInternalServerError)
        }
}

// ── Server Settings API ───────────────────────────────────────────────────────

func ForumsServerSettingsHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
        }

        // path: /api/forums/server/{public_id}/settings
        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/server/"), "/")
        pubID, err := strconv.ParseInt(parts[0], 10, 64)
        if err != nil || pubID <= 0 {
                http.Error(w, "invalid server id", http.StatusBadRequest)
                return
        }
        ctx := r.Context()
        serverID := db.GetInternalServerID(ctx, pubID)
        if serverID == 0 {
                http.Error(w, "server not found", http.StatusNotFound)
                return
        }

        role := db.GetMemberRole(ctx, serverID, sess.UserID)
        if role != "owner" {
                http.Error(w, "forbidden", http.StatusForbidden)
                return
        }

        if err := r.ParseForm(); err != nil {
                http.Error(w, "bad request", http.StatusBadRequest)
                return
        }
        name := strings.TrimSpace(r.FormValue("name"))
        desc := strings.TrimSpace(r.FormValue("description"))
        freeForAll := r.FormValue("free_for_all") == "true"

        if name == "" {
                http.Error(w, "name required", http.StatusBadRequest)
                return
        }
        if len(name) > 80 {
                http.Error(w, "server name too long (max 80 chars)", http.StatusBadRequest)
                return
        }
        if len(desc) > 500 {
                http.Error(w, "description too long (max 500 chars)", http.StatusBadRequest)
                return
        }

        if err := db.UpdateServerInfo(ctx, serverID, name, desc, freeForAll); err != nil {
                http.Error(w, "update failed", http.StatusInternalServerError)
                return
        }

        sv, _ := db.GetServerByID(ctx, serverID, sess.UserID)
        publicID := int64(0)
        if sv != nil {
                publicID = sv.PublicID
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "public_id": publicID})
}

// ── Server Member Action API ──────────────────────────────────────────────────
// POST /api/forums/server/{id}/member
// form: action=role|kick|ban|unban, user_id=N, role=owner|partner|member, reason=...

func ForumsServerMemberActionHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
        }

        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/server/"), "/")
        pubID, err := strconv.ParseInt(parts[0], 10, 64)
        if err != nil || pubID <= 0 {
                http.Error(w, "invalid server id", http.StatusBadRequest)
                return
        }
        ctx := r.Context()
        serverID := db.GetInternalServerID(ctx, pubID)
        if serverID == 0 {
                http.Error(w, "server not found", http.StatusNotFound)
                return
        }

        myRole := db.GetMemberRole(ctx, serverID, sess.UserID)
        if myRole != "owner" && myRole != "partner" {
                http.Error(w, "forbidden", http.StatusForbidden)
                return
        }

        if err := r.ParseForm(); err != nil {
                http.Error(w, "bad request", http.StatusBadRequest)
                return
        }
        action := r.FormValue("action")
        targetUserID, _ := strconv.Atoi(r.FormValue("user_id"))
        if targetUserID <= 0 {
                http.Error(w, "invalid user_id", http.StatusBadRequest)
                return
        }
        // Cannot act on the owner
        targetRole := db.GetMemberRole(ctx, serverID, targetUserID)
        if targetRole == "owner" && sess.UserID != targetUserID {
                http.Error(w, "cannot act on server owner", http.StatusForbidden)
                return
        }

        var actionErr error
        switch action {
        case "role":
                if myRole != "owner" {
                        http.Error(w, "only owner can change roles", http.StatusForbidden)
                        return
                }
                newRole := r.FormValue("role")
                if newRole != "partner" && newRole != "member" {
                        http.Error(w, "invalid role", http.StatusBadRequest)
                        return
                }
                actionErr = db.SetMemberRole(ctx, serverID, targetUserID, newRole)
        case "kick":
                if targetRole == "owner" {
                        http.Error(w, "cannot kick owner", http.StatusForbidden)
                        return
                }
                if myRole == "partner" && targetRole == "partner" {
                        http.Error(w, "partner cannot kick partner", http.StatusForbidden)
                        return
                }
                actionErr = db.KickMember(ctx, serverID, targetUserID)
        case "ban":
                if myRole == "partner" && (targetRole == "owner" || targetRole == "partner") {
                        http.Error(w, "partner cannot ban owner or partner", http.StatusForbidden)
                        return
                }
                reason := r.FormValue("reason")
                if len(reason) > 500 {
                        reason = reason[:500]
                }
                actionErr = db.BanMember(ctx, serverID, targetUserID, sess.UserID, reason)
        case "unban":
                if myRole != "owner" && myRole != "partner" {
                        http.Error(w, "forbidden", http.StatusForbidden)
                        return
                }
                actionErr = db.UnbanMember(ctx, serverID, targetUserID)
        default:
                http.Error(w, "unknown action", http.StatusBadRequest)
                return
        }

        if actionErr != nil {
                http.Error(w, actionErr.Error(), http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// ── Server Join / Leave ───────────────────────────────────────────────────────

func ForumsServerJoinLeaveHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
        }

        // path: /api/forums/server/{public_id}/join  or /api/forums/server/{public_id}/leave
        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/server/"), "/")
        pubID, err := strconv.ParseInt(parts[0], 10, 64)
        if err != nil || pubID <= 0 {
                http.Error(w, "invalid server id", http.StatusBadRequest)
                return
        }
        ctx := r.Context()
        serverID := db.GetInternalServerID(ctx, pubID)
        if serverID == 0 {
                http.Error(w, "server not found", http.StatusNotFound)
                return
        }
        action := ""
        if len(parts) >= 2 {
                action = parts[1]
        }

        r.ParseForm()
        inviteToken := strings.TrimSpace(r.FormValue("invite_token"))
        var actionErr error
        switch action {
        case "join":
                actionErr = db.JoinServer(ctx, serverID, sess.UserID, inviteToken)
        case "leave":
                actionErr = db.LeaveServer(ctx, serverID, sess.UserID)
        default:
                http.Error(w, "unknown action", http.StatusBadRequest)
                return
        }

        if actionErr != nil {
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": actionErr.Error()})
                return
        }

        w.Header().Set("Content-Type", "application/json")
        joined := action == "join"
        json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "joined": joined})
}

// ── Comment Vote Handler ──────────────────────────────────────────────────────
// POST /api/forums/comment/{id}/vote  body: value=1|-1|0

func ForumsCommentVoteHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
        }
        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/comment/"), "/")
        commentID, err := strconv.Atoi(parts[0])
        if err != nil || commentID <= 0 {
                http.Error(w, "invalid comment id", http.StatusBadRequest)
                return
        }

        action := ""
        if len(parts) >= 2 {
                action = parts[1]
        }

        if action == "delete" {
                if err := db.DeleteForumComment(r.Context(), commentID, sess.UserID); err != nil {
                        http.Error(w, "delete failed", http.StatusForbidden)
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
                return
        }

        if action == "edit" {
                if err := r.ParseForm(); err != nil {
                        http.Error(w, "bad request", http.StatusBadRequest)
                        return
                }
                body := strings.TrimSpace(r.FormValue("body"))
                if body == "" {
                        http.Error(w, "body required", http.StatusBadRequest)
                        return
                }
                if len(body) > 5000 {
                        http.Error(w, "reply too long (max 5,000 characters)", http.StatusBadRequest)
                        return
                }
                if err := db.UpdateForumComment(r.Context(), commentID, sess.UserID, body); err != nil {
                        http.Error(w, "edit failed", http.StatusForbidden)
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "body": body})
                return
        }

        if err := r.ParseForm(); err != nil {
                http.Error(w, "bad request", http.StatusBadRequest)
                return
        }
        value, _ := strconv.Atoi(r.FormValue("value"))
        if value != -1 && value != 0 && value != 1 {
                http.Error(w, "invalid vote value", http.StatusBadRequest)
                return
        }
        score, userVote, err := db.VoteOnComment(r.Context(), commentID, sess.UserID, value)
        if err != nil {
                http.Error(w, "vote failed", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"score": score, "user_vote": userVote})
}

// ── Server Image Upload Handler ───────────────────────────────────────────────
// POST /api/forums/server/{id}/logo  or  /api/forums/server/{id}/banner

func ForumsServerImageHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
        }
        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/server/"), "/")
        pubID, err := strconv.ParseInt(parts[0], 10, 64)
        if err != nil || pubID <= 0 {
                http.Error(w, "invalid server id", http.StatusBadRequest)
                return
        }
        ctx := r.Context()
        serverID := db.GetInternalServerID(ctx, pubID)
        if serverID == 0 {
                http.Error(w, "server not found", http.StatusNotFound)
                return
        }
        action := ""
        if len(parts) >= 2 {
                action = parts[1]
        }
        role := db.GetMemberRole(ctx, serverID, sess.UserID)
        if role != "owner" && role != "partner" {
                http.Error(w, "forbidden", http.StatusForbidden)
                return
        }
        r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
        if err := r.ParseMultipartForm(20 << 20); err != nil {
                http.Error(w, "file too large (max 20MB)", http.StatusBadRequest)
                return
        }
        file, _, err := r.FormFile("file")
        if err != nil {
                http.Error(w, "no file", http.StatusBadRequest)
                return
        }
        defer file.Close()

        magicBuf := make([]byte, 8)
        if _, err := file.Read(magicBuf); err != nil {
                http.Error(w, "read error", http.StatusBadRequest)
                return
        }
        if !isImage(magicBuf) {
                http.Error(w, "not an image", http.StatusBadRequest)
                return
        }
        if _, err := file.Seek(0, 0); err != nil {
                http.Error(w, "seek error", http.StatusInternalServerError)
                return
        }

        var ext string
        switch {
        case magicBuf[0] == 0xFF && magicBuf[1] == 0xD8:
                ext = ".jpg"
        case magicBuf[0] == 0x89 && magicBuf[1] == 0x50:
                ext = ".png"
        case magicBuf[0] == 0x47 && magicBuf[1] == 0x49:
                ext = ".gif"
        case len(magicBuf) >= 8 && magicBuf[0] == 0x52 && magicBuf[1] == 0x49 &&
                magicBuf[2] == 0x46 && magicBuf[3] == 0x46 &&
                magicBuf[4] == 0x57 && magicBuf[5] == 0x45 && magicBuf[6] == 0x42 && magicBuf[7] == 0x50:
                ext = ".webp"
        default:
                ext = ".jpg"
        }

        fname := "srv_" + action + "_" + strconv.Itoa(serverID) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36) + ext
        savePath := "static/uploads/" + fname
        if err := os.MkdirAll("static/uploads", 0755); err != nil {
                http.Error(w, "storage error", http.StatusInternalServerError)
                return
        }
        out, err := os.Create(savePath)
        if err != nil {
                http.Error(w, "storage error", http.StatusInternalServerError)
                return
        }
        defer out.Close()
        if _, err := io.Copy(out, file); err != nil {
                http.Error(w, "write error", http.StatusInternalServerError)
                return
        }

        urlPath := "/static/uploads/" + fname
        if action == "logo" {
                err = db.UpdateServerLogo(ctx, serverID, urlPath)
        } else {
                err = db.UpdateServerBanner(ctx, serverID, urlPath)
        }
        if err != nil {
                http.Error(w, "db error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "url": urlPath})
}

func isImage(b []byte) bool {
        if len(b) < 4 {
                return false
        }
        // JPEG
        if b[0] == 0xFF && b[1] == 0xD8 {
                return true
        }
        // PNG
        if b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47 {
                return true
        }
        // GIF
        if b[0] == 0x47 && b[1] == 0x49 && b[2] == 0x46 {
                return true
        }
        // WebP: RIFF....WEBP
        if len(b) >= 8 && b[0] == 0x52 && b[1] == 0x49 && b[4] == 0x57 {
                return true
        }
        return false
}

// ── User Profile Popup API ────────────────────────────────────────────────────
// GET /api/forums/user/{username}/popup

func ForumsUserPopupHandler(w http.ResponseWriter, r *http.Request) {
        username := strings.TrimPrefix(r.URL.Path, "/api/forums/user/")
        username = strings.TrimSuffix(username, "/popup")
        username = strings.Trim(username, "/")
        if username == "" {
                http.Error(w, "not found", http.StatusNotFound)
                return
        }
        u, err := db.GetUserPublicProfile(r.Context(), username)
        if err != nil {
                http.Error(w, "not found", http.StatusNotFound)
                return
        }
        initials := ""
        name := u.Username
        if u.DisplayName != "" {
                name = u.DisplayName
        }
        if len(name) >= 2 {
                initials = strings.ToUpper(name[:2])
        } else if len(name) == 1 {
                initials = strings.ToUpper(name[:1])
        }
        isFollowing := false
        _, sess2, _ := session.FromRequest(r)
        if sess2 != nil && sess2.UserID != u.ID {
                isFollowing, _ = db.IsUserFollowing(r.Context(), sess2.UserID, u.ID)
        }
        type PopupResp struct {
                ID          int    `json:"id"`
                Username    string `json:"username"`
                DisplayName string `json:"display_name"`
                Bio         string `json:"bio"`
                AvatarURL   string `json:"avatar_url"`
                Initials    string `json:"initials"`
                IsFollowing bool   `json:"is_following"`
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(PopupResp{
                ID:          u.ID,
                Username:    u.Username,
                DisplayName: name,
                Bio:         u.Bio,
                AvatarURL:   assetURL(u.AvatarPath),
                Initials:    initials,
                IsFollowing: isFollowing,
        })
}

// ── Handle Check API ─────────────────────────────────────────────────────────

func ForumsHandleCheckHandler(w http.ResponseWriter, r *http.Request) {
        handle := strings.TrimSpace(r.URL.Query().Get("handle"))
        excludeIDStr := r.URL.Query().Get("exclude")
        excludeID, _ := strconv.Atoi(excludeIDStr)
        available := db.CheckHandleAvailable(r.Context(), handle, excludeID)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]bool{"available": available})
}

// ── Server API Dispatcher ─────────────────────────────────────────────────────

func ForumsServerAPIDispatch(w http.ResponseWriter, r *http.Request) {
        path := strings.TrimPrefix(r.URL.Path, "/api/forums/server/")
        parts := strings.Split(strings.Trim(path, "/"), "/")
        if len(parts) < 2 {
                http.Error(w, "not found", http.StatusNotFound)
                return
        }
        action := parts[1]
        switch action {
        case "follow":
                ForumsServerFollowHandler(w, r)
        case "settings":
                ForumsServerSettingsHandler(w, r)
        case "member":
                ForumsServerMemberActionHandler(w, r)
        case "join", "leave":
                ForumsServerJoinLeaveHandler(w, r)
        case "logo", "banner":
                ForumsServerImageHandler(w, r)
        case "delete":
                ForumsServerDeleteHandler(w, r)
        default:
                http.Error(w, "not found", http.StatusNotFound)
        }
}

// ── Server Delete API ─────────────────────────────────────────────────────────
// POST /api/forums/server/{id}/delete

func ForumsServerDeleteHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sess, _ := session.FromRequest(r)
        if sess == nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
        }

        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/forums/server/"), "/")
        pubID, err := strconv.ParseInt(parts[0], 10, 64)
        if err != nil || pubID <= 0 {
                http.Error(w, "invalid server id", http.StatusBadRequest)
                return
        }
        ctx := r.Context()
        serverID := db.GetInternalServerID(ctx, pubID)
        if serverID == 0 {
                http.Error(w, "server not found", http.StatusNotFound)
                return
        }

        if err := db.DeleteForumServer(ctx, serverID, sess.UserID); err != nil {
                status := http.StatusInternalServerError
                if err.Error() == "forbidden" {
                        status = http.StatusForbidden
                } else if err.Error() == "server not found" {
                        status = http.StatusNotFound
                }
                http.Error(w, err.Error(), status)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// ── Search API ────────────────────────────────────────────────────────────────

func ForumsSearchAPIHandler(w http.ResponseWriter, r *http.Request) {
        q := strings.TrimSpace(r.URL.Query().Get("q"))
        filterType := r.URL.Query().Get("filter")

        if strings.HasPrefix(q, "@") {
                q = strings.TrimPrefix(q, "@")
                if filterType == "" {
                        filterType = "servers"
                }
        }

        viewerID := 0
        _, sess, _ := session.FromRequest(r)
        if sess != nil {
                viewerID = sess.UserID
        }

        resp, err := db.SearchForums(r.Context(), q, filterType, viewerID, 8)
        if err != nil || resp == nil {
                resp = &db.ForumSearchResponse{Suggestions: []string{}, Results: []db.ForumSearchResult{}}
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
}

func ForumsPostsAPIHandler(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()

        cat := r.URL.Query().Get("cat")
        serverSlug := r.URL.Query().Get("server")
        tag := r.URL.Query().Get("tag")
        q := r.URL.Query().Get("q")
        sortBy := r.URL.Query().Get("sort")
        dateFilter := r.URL.Query().Get("date")
        pageStr := r.URL.Query().Get("page")
        page, _ := strconv.Atoi(pageStr)
        if page < 1 {
                page = 1
        }
        limit := 20
        offset := (page - 1) * limit

        viewerID := 0
        _, sess, _ := session.FromRequest(r)
        if sess != nil {
                viewerID = sess.UserID
        }

        serverID := 0
        var serverInfo *db.ForumServer
        if serverSlug != "" {
                sv, err := db.GetServerBySlug(ctx, serverSlug, viewerID)
                if err == nil {
                        serverID = sv.ID
                        serverInfo = sv
                }
        }

        posts, err := db.GetForumPosts(ctx, cat, serverID, tag, q, viewerID, limit+1, offset, sortBy, dateFilter)
        if err != nil {
                posts = []db.ForumPost{}
        }
        hasMore := len(posts) > limit
        if hasMore {
                posts = posts[:limit]
        }

        type PostJSON struct {
                ID       int      `json:"id"`
                Title    string   `json:"title"`
                Body     string   `json:"body"`
                Author   string   `json:"author"`
                CatName  string   `json:"cat_name"`
                CatSlug  string   `json:"cat_slug"`
                SrvName  string   `json:"srv_name"`
                SrvSlug  string   `json:"srv_slug"`
                SrvID    int      `json:"srv_id"`
                Score    int      `json:"score"`
                Comments int      `json:"comments"`
                Vote     int      `json:"vote"`
                TimeAgo  string   `json:"time_ago"`
                Tags     []string `json:"tags"`
                ImageURL string   `json:"img"`
        }

        postList := []PostJSON{}
        for _, p := range posts {
                tags := p.Hashtags
                if tags == nil {
                        tags = []string{}
                }
                postList = append(postList, PostJSON{
                        ID:       p.ID,
                        Title:    p.Title,
                        Body:     p.Body,
                        Author:   p.AuthorName,
                        CatName:  p.CategoryName,
                        CatSlug:  p.CategorySlug,
                        SrvName:  p.ServerName,
                        SrvSlug:  p.ServerSlug,
                        SrvID:    int(p.ServerID.Int64),
                        Score:    p.Score,
                        Comments: p.CommentCount,
                        Vote:     p.UserVote,
                        TimeAgo:  timeAgo(p.CreatedAt),
                        Tags:     tags,
                        ImageURL: p.ImageURL,
                })
        }

        type ServerJSON struct {
                ID       int    `json:"id"`
                Name     string `json:"name"`
                Slug     string `json:"slug"`
                Initials string `json:"initials"`
                Members  int    `json:"members"`
                IsMember bool   `json:"is_member"`
        }

        var srvJSON interface{}
        if serverInfo != nil {
                srvJSON = ServerJSON{
                        ID:       serverInfo.ID,
                        Name:     serverInfo.Name,
                        Slug:     serverInfo.Slug,
                        Initials: serverInfo.Initials,
                        Members:  serverInfo.MemberCount,
                        IsMember: serverInfo.IsMember,
                }
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{
                "posts":    postList,
                "has_more": hasMore,
                "page":     page,
                "server":   srvJSON,
        })
}

func ForumUserProfileHandler(w http.ResponseWriter, r *http.Request) {
        username := strings.TrimPrefix(r.URL.Path, "/forums/u/")
        username = strings.Trim(username, "/")
        if username == "" {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }

        base := pageData(w, r)
        ctx := r.Context()

        viewerID := 0
        if base.Session != nil {
                viewerID = base.Session.UserID
        }

        profileUser, err := db.GetUserByUsername(username)
        if err != nil || profileUser == nil {
                http.Redirect(w, r, "/forums", http.StatusSeeOther)
                return
        }

        tab := r.URL.Query().Get("tab")
        if tab != "replies" {
                tab = "posts"
        }

        pageStr := r.URL.Query().Get("page")
        page, _ := strconv.Atoi(pageStr)
        if page < 1 {
                page = 1
        }
        limit := 20
        offset := (page - 1) * limit

        postCount, replyCount, totalScore := db.GetUserForumStats(ctx, username)

        var posts []db.ForumPost
        var replies []db.ForumComment
        var hasMore bool

        if tab == "posts" {
                posts, _ = db.GetUserForumPosts(ctx, username, viewerID, limit+1, offset)
                hasMore = len(posts) > limit
                if hasMore {
                        posts = posts[:limit]
                }
        } else {
                replies, _ = db.GetUserForumReplies(ctx, username, viewerID, limit+1, offset)
                hasMore = len(replies) > limit
                if hasMore {
                        replies = replies[:limit]
                }
        }

        isFollowing := false
        if base.Session != nil && base.Session.UserID != profileUser.ID {
                isFollowing, _ = db.IsUserFollowing(ctx, base.Session.UserID, profileUser.ID)
        }

        prevPage := page - 1
        if prevPage < 1 {
                prevPage = 1
        }
        data := ForumUserProfilePageData{
                PageData:    base,
                ProfileUser: profileUser,
                Posts:       posts,
                Replies:     replies,
                PostCount:   postCount,
                ReplyCount:  replyCount,
                TotalScore:  totalScore,
                IsFollowing: isFollowing,
                Tab:         tab,
                HasMore:     hasMore,
                CurrentPage: page,
                PrevPage:    prevPage,
                TimeAgo:     timeAgo,
        }

        tmpl, err := parseTemplates("forums-user-profile.html")
        if err != nil {
                http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
                http.Error(w, "Template execute error: "+err.Error(), http.StatusInternalServerError)
        }
}
