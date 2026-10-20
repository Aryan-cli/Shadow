package handlers

import (
        "context"
        "crypto/rand"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "html/template"
        "log"
        "net/http"
        "path/filepath"
        "shadow/internal/auth"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/payment"
        "strconv"
        "strings"
        "sync"
        "time"
)

const adminCookieName = "shadow_admin"
const adminSessionTTL = 4 * time.Hour
const adminPathLen = 10

var (
        adminSessionsMu sync.Mutex
        adminSessions   = make(map[string]time.Time)

        adminPathMu      sync.RWMutex
        currentAdminPath string
)

func generateAdminPath() string {
        const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
        b := make([]byte, adminPathLen)
        _, _ = rand.Read(b)
        for i := range b {
                b[i] = chars[int(b[i])%len(chars)]
        }
        return string(b)
}

func InitAdminPath() {
        adminPathMu.Lock()
        currentAdminPath = generateAdminPath()
        adminPathMu.Unlock()
        log.Printf("[ADMIN] Panel path: /%s", currentAdminPath)

        go func() {
                ticker := time.NewTicker(24 * time.Hour)
                defer ticker.Stop()
                for range ticker.C {
                        newPath := generateAdminPath()
                        adminPathMu.Lock()
                        currentAdminPath = newPath
                        adminPathMu.Unlock()
                        log.Printf("[ADMIN] Panel path rotated: /%s", newPath)
                }
        }()
}

func GetAdminPath() string {
        adminPathMu.RLock()
        defer adminPathMu.RUnlock()
        return "/" + currentAdminPath
}

func newAdminToken() string {
        b := make([]byte, 32)
        _, _ = rand.Read(b)
        return hex.EncodeToString(b)
}

func isAdminLoggedIn(r *http.Request) bool {
        cookie, err := r.Cookie(adminCookieName)
        if err != nil {
                return false
        }
        adminSessionsMu.Lock()
        defer adminSessionsMu.Unlock()
        expiry, ok := adminSessions[cookie.Value]
        if !ok {
                return false
        }
        if time.Now().After(expiry) {
                delete(adminSessions, cookie.Value)
                return false
        }
        return true
}

func AdminLogoutHandler(w http.ResponseWriter, r *http.Request) {
        cookie, err := r.Cookie(adminCookieName)
        if err == nil {
                adminSessionsMu.Lock()
                delete(adminSessions, cookie.Value)
                adminSessionsMu.Unlock()
        }
        http.SetCookie(w, &http.Cookie{
                Name:     adminCookieName,
                Value:    "",
                Path:     "/",
                MaxAge:   -1,
                HttpOnly: true,
                SameSite: http.SameSiteStrictMode,
        })
        http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func adminGuard(w http.ResponseWriter, r *http.Request) bool {
        if !isAdminLoggedIn(r) {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return false
        }
        return true
}

type AdminBase struct {
        CSRFToken  string
        ActivePage string
        AdminPath  string
}

type AdminDashData struct {
        AdminBase
        TotalUsers   int
        OnlineCount  int
        OfflineCount int
}

func AdminDashHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        total, _ := db.AdminCountUsers()
        online, _ := db.AdminCountOnlineUsers()

        renderAdminPage(w, r, "admin-dash.html", "admin-dash", AdminDashData{
                AdminBase:    AdminBase{CSRFToken: csrfToken, ActivePage: "dash", AdminPath: GetAdminPath()},
                TotalUsers:   total,
                OnlineCount:  online,
                OfflineCount: total - online,
        })
}

type AdminUsersData struct {
        AdminBase
        Users  []db.AdminUser
        Search string
}

func AdminUsersHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        search := strings.TrimSpace(r.URL.Query().Get("q"))

        users, err := db.AdminListUsers(search)
        if err != nil {
                log.Printf("admin list users: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        renderAdminPage(w, r, "admin-users.html", "admin-users", AdminUsersData{
                AdminBase: AdminBase{CSRFToken: csrfToken, ActivePage: "users", AdminPath: GetAdminPath()},
                Users:     users,
                Search:    search,
        })
}

type AdminUserDetailData struct {
        AdminBase
        User         *db.AdminUser
        PasswordHash string
        Error        string
        Success      string
}

func AdminUserDetailHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        ap := GetAdminPath()

        idStr := strings.TrimPrefix(r.URL.Path, ap+"/users/")
        idStr = strings.Split(idStr, "/")[0]
        userID, err := strconv.Atoi(idStr)
        if err != nil {
                http.Redirect(w, r, "/", http.StatusFound)
                return
        }

        user, err := db.AdminGetUser(userID)
        if err != nil || user == nil {
                http.Redirect(w, r, ap+"/users", http.StatusFound)
                return
        }

        hash, _ := db.GetPasswordHash(userID)

        renderAdminPage(w, r, "admin-user-detail.html", "admin-user-detail", AdminUserDetailData{
                AdminBase:    AdminBase{CSRFToken: csrfToken, ActivePage: "users", AdminPath: ap},
                User:         user,
                PasswordHash: hash,
                Success:      r.URL.Query().Get("success"),
        })
}

func AdminUserActionHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        if r.Method != http.MethodPost {
                http.Redirect(w, r, "/", http.StatusFound)
                return
        }

        ap := GetAdminPath()
        path := strings.TrimPrefix(r.URL.Path, ap+"/users/")
        parts := strings.Split(path, "/")
        if len(parts) < 2 {
                http.Redirect(w, r, ap+"/users", http.StatusFound)
                return
        }
        userID, err := strconv.Atoi(parts[0])
        if err != nil {
                http.Redirect(w, r, ap+"/users", http.StatusFound)
                return
        }
        action := parts[1]
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)

        switch action {
        case "ban":
                _ = db.AdminBanUser(userID)
                http.Redirect(w, r, ap+"/users/"+parts[0], http.StatusSeeOther)

        case "unban":
                _ = db.AdminUnbanUser(userID)
                http.Redirect(w, r, ap+"/users/"+parts[0], http.StatusSeeOther)

        case "delete":
                if err := db.AdminDeleteUser(userID); err != nil {
                        log.Printf("admin delete user: %v", err)
                        http.Error(w, "Failed to delete user", http.StatusInternalServerError)
                        return
                }
                http.Redirect(w, r, ap+"/users", http.StatusSeeOther)

        case "password":
                newPass := r.FormValue("new_password")
                if len(newPass) < 8 {
                        user, _ := db.AdminGetUser(userID)
                        hash, _ := db.GetPasswordHash(userID)
                        renderAdminPage(w, r, "admin-user-detail.html", "admin-user-detail", AdminUserDetailData{
                                AdminBase:    AdminBase{CSRFToken: csrfToken, ActivePage: "users", AdminPath: ap},
                                User:         user,
                                PasswordHash: hash,
                                Error:        "Password must be at least 8 characters.",
                        })
                        return
                }
                hash, err := auth.HashPassword(newPass)
                if err != nil {
                        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                        return
                }
                _ = db.UpdatePassword(userID, hash)
                http.Redirect(w, r, ap+"/users/"+parts[0]+"?success=password", http.StatusSeeOther)

        default:
                http.Redirect(w, r, ap+"/users", http.StatusFound)
        }
}

func AdminUserRouteHandler(w http.ResponseWriter, r *http.Request) {
        ap := GetAdminPath()
        path := strings.TrimPrefix(r.URL.Path, ap+"/users/")
        parts := strings.Split(path, "/")
        if len(parts) == 1 {
                AdminUserDetailHandler(w, r)
        } else {
                AdminUserActionHandler(w, r)
        }
}

type AdminSiteSettingsData struct {
        AdminBase
        OnlineThreshold int
        IdleThreshold   int
        BTCEnabled      bool
        XMREnabled      bool
        XMRMaint        bool
        HDWalletActive  bool
        XMRRPCActive    bool
        Success         string
}

func AdminSiteSettingsHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        success := ""

        if r.Method == http.MethodPost {
                r.ParseForm()
                if v, err := strconv.Atoi(r.FormValue("online_threshold_seconds")); err == nil && v > 0 {
                        _ = db.SetSiteSetting("online_threshold_seconds", strconv.Itoa(v))
                }
                if v, err := strconv.Atoi(r.FormValue("idle_threshold_seconds")); err == nil && v > 0 {
                        _ = db.SetSiteSetting("idle_threshold_seconds", strconv.Itoa(v))
                }

                // Payment settings
                btcEnabled := "0"
                if r.FormValue("btc_enabled") == "1" {
                        btcEnabled = "1"
                }
                _ = db.SetSiteSetting("btc_enabled", btcEnabled)

                xmrEnabled := "0"
                if r.FormValue("xmr_enabled") == "1" {
                        xmrEnabled = "1"
                }
                _ = db.SetSiteSetting("xmr_enabled", xmrEnabled)

                xmrMaint := "0"
                if r.FormValue("xmr_maintenance") == "1" {
                        xmrMaint = "1"
                }
                _ = db.SetSiteSetting("xmr_maintenance", xmrMaint)

                success = "Settings saved successfully."
        }

        renderAdminPage(w, r, "admin-site-settings.html", "admin-site-settings", AdminSiteSettingsData{
                AdminBase:       AdminBase{CSRFToken: csrfToken, ActivePage: "settings", AdminPath: GetAdminPath()},
                OnlineThreshold: db.GetSiteSettingInt("online_threshold_seconds", 30),
                IdleThreshold:   db.GetSiteSettingInt("idle_threshold_seconds", 600),
                BTCEnabled:      db.GetSiteSettingInt("btc_enabled", 1) == 1,
                XMREnabled:      db.GetSiteSettingInt("xmr_enabled", 0) == 1,
                XMRMaint:        db.GetSiteSettingInt("xmr_maintenance", 1) == 1,
                HDWalletActive:  payment.HDEnabled(),
                XMRRPCActive:    payment.XMREnabled(),
                Success:         success,
        })
}

func AdminRouteHandler(w http.ResponseWriter, r *http.Request) {
        ap := GetAdminPath()
        path := r.URL.Path

        switch {
        case path == ap || path == ap+"/":
                AdminDashHandler(w, r)
        case path == ap+"/logout":
                AdminLogoutHandler(w, r)
        case path == ap+"/users" || path == ap+"/users/":
                AdminUsersHandler(w, r)
        case strings.HasPrefix(path, ap+"/users/"):
                AdminUserRouteHandler(w, r)
        case path == ap+"/settings":
                AdminSiteSettingsHandler(w, r)
        case strings.HasPrefix(path, ap+"/marketplace"):
                AdminMarketplaceHandler(w, r)
        case path == ap+"/payments":
                AdminPaymentsHandler(w, r)
        case strings.HasPrefix(path, ap+"/api/payments"):
                AdminPaymentsAPIHandler(w, r)
        case path == ap+"/support" || path == ap+"/support/":
                AdminSupportHandler(w, r)
        case strings.HasPrefix(path, ap+"/support/"):
                AdminSupportTicketHandler(w, r)
        default:
                http.Redirect(w, r, "/", http.StatusFound)
        }
}

// ── Admin Payments ────────────────────────────────────────────────────────────

type AdminPaymentsData struct {
        AdminBase
        WindowMinutes int
        CleanupHours  int
}

func AdminPaymentsHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        renderAdminPage(w, r, "admin-payments.html", "admin-payments", AdminPaymentsData{
                AdminBase:     AdminBase{CSRFToken: csrfToken, ActivePage: "payments", AdminPath: GetAdminPath()},
                WindowMinutes: db.GetSiteSettingInt("payment_window_minutes", 30),
                CleanupHours:  db.GetSiteSettingInt("payment_cleanup_hours", 5),
        })
}

func AdminPaymentsAPIHandler(w http.ResponseWriter, r *http.Request) {
        if !isAdminLoggedIn(r) {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        ap := GetAdminPath()
        path := strings.TrimPrefix(r.URL.Path, ap+"/api/payments")

        // POST /api/payments/settings
        if path == "/settings" && r.Method == http.MethodPost {
                r.ParseForm()
                if v, err := strconv.Atoi(r.FormValue("window_minutes")); err == nil && v >= 5 && v <= 1440 {
                        _ = db.SetSiteSetting("payment_window_minutes", strconv.Itoa(v))
                }
                if v, err := strconv.Atoi(r.FormValue("cleanup_hours")); err == nil && v >= 1 && v <= 720 {
                        _ = db.SetSiteSetting("payment_cleanup_hours", strconv.Itoa(v))
                }
                json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
                return
        }

        // GET /api/payments/{id}/detail
        if strings.HasSuffix(path, "/detail") {
                payID := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/detail")
                ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
                defer cancel()
                cp, err := db.GetCryptoPayment(ctx, payID)
                if err != nil || cp == nil {
                        http.Error(w, "not found", http.StatusNotFound)
                        return
                }
                // Fetch live balance
                var balanceSat int64
                var balanceBTC float64
                if cp.Coin == "btc" && cp.WalletAddress != "" && cp.WalletAddress != "XMR_MAINTENANCE" {
                        if sat, err := payment.GetAddressReceivedSatoshis(cp.WalletAddress); err == nil {
                                balanceSat = sat
                                balanceBTC = payment.SatoshisToBTC(sat)
                        }
                }
                secsLeft := 0
                if cp.Status == "pending" {
                        secsLeft = int(time.Until(cp.ExpiresAt).Seconds())
                        if secsLeft < 0 {
                                secsLeft = 0
                        }
                }
                json.NewEncoder(w).Encode(map[string]interface{}{
                        "id":            cp.ID,
                        "coin":          cp.Coin,
                        "wallet":        cp.WalletAddress,
                        "private_key":   cp.PrivateKeyWIF,
                        "amount_crypto": cp.AmountCrypto,
                        "amount_usd":    cp.AmountUSD,
                        "received":      cp.ReceivedAmount,
                        "balance_sat":   balanceSat,
                        "balance_btc":   balanceBTC,
                        "status":        cp.Status,
                        "created_at":    cp.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
                        "expires_at":    cp.ExpiresAt.UTC().Format("2006-01-02 15:04:05"),
                        "secs_left":     secsLeft,
                })
                return
        }

        // GET /api/payments?status=&search=
        status := r.URL.Query().Get("status")
        search := r.URL.Query().Get("search")
        ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
        defer cancel()
        rows, err := db.GetAdminPayments(ctx, status, search)
        if err != nil {
                http.Error(w, "db error", http.StatusInternalServerError)
                return
        }
        type row struct {
                ID           string  `json:"id"`
                Username     string  `json:"username"`
                ProductTitle string  `json:"product_title"`
                Coin         string  `json:"coin"`
                AmountUSD    float64 `json:"amount_usd"`
                AmountCrypto float64 `json:"amount_crypto"`
                Received     float64 `json:"received"`
                Status       string  `json:"status"`
                CreatedAt    string  `json:"created_at"`
                ExpiresAt    string  `json:"expires_at"`
                SecsLeft     int     `json:"secs_left"`
        }
        out := make([]row, 0, len(rows))
        for _, r := range rows {
                sl := 0
                if r.Status == "pending" {
                        sl = int(time.Until(r.ExpiresAt).Seconds())
                        if sl < 0 {
                                sl = 0
                        }
                }
                out = append(out, row{
                        ID:           r.ID,
                        Username:     r.Username,
                        ProductTitle: r.ProductTitle,
                        Coin:         r.Coin,
                        AmountUSD:    r.AmountUSD,
                        AmountCrypto: r.AmountCrypto,
                        Received:     r.ReceivedAmount,
                        Status:       r.Status,
                        CreatedAt:    r.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
                        ExpiresAt:    r.ExpiresAt.UTC().Format("2006-01-02 15:04:05"),
                        SecsLeft:     sl,
                })
        }
        json.NewEncoder(w).Encode(out)
}

// ── Admin Support ─────────────────────────────────────────────────────────────

type AdminSupportData struct {
        AdminBase
        Tickets    []db.SupportTicket
        Filter     string
        OpenCount  int
}

func AdminSupportHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        filter := r.URL.Query().Get("status")
        if filter == "" {
                filter = "open"
        }
        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()
        tickets, err := db.AdminGetAllTickets(ctx, filter)
        if err != nil {
                log.Printf("admin support tickets: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        openCount := db.AdminCountOpenTickets(ctx)
        renderAdminPage(w, r, "admin-support.html", "admin-support", AdminSupportData{
                AdminBase: AdminBase{CSRFToken: csrfToken, ActivePage: "support", AdminPath: GetAdminPath()},
                Tickets:   tickets,
                Filter:    filter,
                OpenCount: openCount,
        })
}

type AdminSupportTicketData struct {
        AdminBase
        Ticket  *db.SupportTicket
        Replies []db.SupportTicketReply
        Error   string
        Success string
}

func AdminSupportTicketHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        ap := GetAdminPath()

        idStr := strings.TrimPrefix(r.URL.Path, ap+"/support/")
        parts := strings.Split(idStr, "/")
        ticketID, err := strconv.Atoi(parts[0])
        if err != nil {
                http.Redirect(w, r, ap+"/support", http.StatusFound)
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        // Handle POST actions
        if r.Method == http.MethodPost {
                r.ParseForm()
                action := r.FormValue("action")
                switch action {
                case "reply":
                        body := strings.TrimSpace(r.FormValue("body"))
                        if body != "" {
                                _ = db.AddTicketReply(ctx, ticketID, nil, true, body)
                        }
                case "close":
                        _ = db.SetTicketStatus(ctx, ticketID, "closed")
                case "open":
                        _ = db.SetTicketStatus(ctx, ticketID, "open")
                }
                http.Redirect(w, r, ap+"/support/"+parts[0], http.StatusSeeOther)
                return
        }

        ticket, err := db.GetSupportTicket(ctx, ticketID)
        if err != nil || ticket == nil {
                http.Redirect(w, r, ap+"/support", http.StatusFound)
                return
        }
        replies, _ := db.GetTicketReplies(ctx, ticketID)

        renderAdminPage(w, r, "admin-support-ticket.html", "admin-support-ticket", AdminSupportTicketData{
                AdminBase: AdminBase{CSRFToken: csrfToken, ActivePage: "support", AdminPath: ap},
                Ticket:    ticket,
                Replies:   replies,
        })
}

func renderAdminPage(w http.ResponseWriter, r *http.Request, tplFile, tplName string, data interface{}) {
        funcMap := template.FuncMap{
                "formatPrice": func(p float64) string {
                        return fmt.Sprintf("%.2f", p)
                },
                "trimStaticPrefix": func(path string) string {
                        return strings.TrimPrefix(path, "static/")
                },
                "truncate": func(s string, n int) string {
                        if len([]rune(s)) <= n {
                                return s
                        }
                        return string([]rune(s)[:n]) + "…"
                },
        }
        tmpl, err := template.New(tplName).Funcs(funcMap).ParseFiles(
                filepath.Join("templates", "admin-base.html"),
                filepath.Join("templates", tplFile),
        )
        if err != nil {
                log.Printf("admin template %s: %v", tplFile, err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "admin-base", data); err != nil {
                log.Printf("admin render %s: %v", tplName, err)
        }
}
