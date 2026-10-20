package main

import (
        "context"
        "log"
        "net/http"
        "os"
        "shadow/internal/db"
        "shadow/internal/handlers"
        "shadow/internal/middleware"
        "shadow/internal/session"
        "strings"
        "time"
)

var knownRoutes = []string{
        "/",
        "/login",
        "/register",
        "/logout",
        "/api/status",
        "/api/heartbeat",
        "/marketplace",
        "/purchases",
        "/api/marketplace/products",
        "/api/admin/mkt/images",
        "/forums",
        "/forums/post",
        "/forums/category/create",
        "/forums/server/create",
        "/settings",
        "/settings/display-name",
        "/settings/bio",
        "/settings/email",
        "/settings/avatar",
        "/settings/avatar/remove",
        "/settings/banner",
        "/settings/password",
        "/settings/privacy",
        "/settings/delete-account",
        "/profile",
        "/profile/update",
        "/profile/avatar",
        "/profile/banner",
        "/support",
        "/support/ticket",
        "/chat",
        "/api/chat/friends",
        "/api/chat/requests",
        "/api/chat/messages",
        "/api/chat/message/send",
        "/api/chat/message/edit",
        "/api/chat/message/delete",
        "/api/chat/friend/send",
        "/api/chat/friend/accept",
        "/api/chat/friend/reject",
        "/api/chat/search",
        "/api/chat/typing",
        "/api/chat/friends/list",
        "/api/chat/msg-requests/count",
        "/api/chat/msg-requests",
        "/api/chat/msg-request/accept",
        "/api/chat/msg-request/decline",
        "/api/chat/conversation/hide",
        "/api/chat/notifications/poll",
}

var knownPrefixes = []string{
        "/static/",
        "/user/",
        "/support/ticket/",
        "/profile/",
        "/forums/post/",
        "/forums/u/",
        "/forums/server/",
        "/api/forums/",
        "/invite/",
        "/api/marketplace/product/",
        "/products/",
        "/pay/",
        "/api/pay/",
}

func isKnownRoute(path string) bool {
        for _, r := range knownRoutes {
                if path == r {
                        return true
                }
        }
        for _, p := range knownPrefixes {
                if strings.HasPrefix(path, p) {
                        return true
                }
        }
        return false
}

func main() {
        if err := db.Connect(); err != nil {
                log.Fatalf("database: %v", err)
        }
        log.Println("database connected")

        if err := db.Migrate(); err != nil {
                log.Fatalf("migrate: %v", err)
        }
        log.Println("database migrated")
        {
                tokenCtx, tokenCancel := context.WithTimeout(context.Background(), 10*time.Second)
                _ = db.EnsureInviteTokens(tokenCtx)
                tokenCancel()
        }

        {
                seedCtx, seedCancel := context.WithTimeout(context.Background(), 30*time.Second)
                if err := db.SeedForumDemoData(seedCtx); err != nil {
                        log.Printf("seed demo data (non-fatal): %v", err)
                } else {
                        log.Println("demo data seeded")
                }
                seedCancel()
        }

        if err := session.Init(); err != nil {
                log.Fatalf("session store: %v", err)
        }
        log.Println("session store connected")

        handlers.InitAdminPath()
        handlers.StartPaymentWorkers()

        // Background: hard-delete accounts whose 24h recovery window has passed
        go func() {
                ticker := time.NewTicker(1 * time.Hour)
                defer ticker.Stop()
                for range ticker.C {
                        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
                        _, _ = db.DB.ExecContext(ctx, `DELETE FROM users WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - INTERVAL '24 hours'`)
                        cancel()
                }
        }()

        mux := http.NewServeMux()

        fs := http.FileServer(http.Dir("static"))
        mux.Handle("/static/", http.StripPrefix("/static/", fs))

        mux.HandleFunc("/support", handlers.SupportHandler)
        mux.HandleFunc("/support/ticket/", handlers.SupportTicketDetailHandler)
        mux.HandleFunc("/login", handlers.LoginHandler)
        mux.HandleFunc("/register", handlers.RegisterHandler)
        mux.HandleFunc("/logout", handlers.LogoutHandler)
        mux.HandleFunc("/api/status", handlers.StatusCheckHandler)
        mux.HandleFunc("/api/heartbeat", handlers.HeartbeatHandler)
        mux.HandleFunc("/settings", handlers.SettingsHandler)
        mux.HandleFunc("/settings/display-name", handlers.SettingsDisplayNameHandler)
        mux.HandleFunc("/settings/bio", handlers.SettingsBioHandler)
        mux.HandleFunc("/settings/email", handlers.SettingsEmailHandler)
        mux.HandleFunc("/settings/avatar", handlers.SettingsAvatarHandler)
        mux.HandleFunc("/settings/avatar/remove", handlers.SettingsRemoveAvatarHandler)
        mux.HandleFunc("/settings/banner", handlers.SettingsBannerHandler)
        mux.HandleFunc("/settings/password", handlers.SettingsPasswordHandler)
        mux.HandleFunc("/settings/privacy", handlers.SettingsPrivacyHandler)
        mux.HandleFunc("/settings/delete-account", handlers.SettingsDeleteAccountHandler)
        mux.HandleFunc("/user/", handlers.UserProfileHandler)
        mux.HandleFunc("/profile", handlers.ProfileHandler)
        mux.HandleFunc("/profile/", handlers.ProfileHandler)
        mux.HandleFunc("/profile/update", handlers.ProfileUpdateHandler)
        mux.HandleFunc("/profile/avatar", handlers.ProfileAvatarHandler)
        mux.HandleFunc("/profile/banner", handlers.ProfileBannerHandler)
        mux.HandleFunc("/chat", handlers.ChatPageHandler)
        mux.HandleFunc("/api/chat/friends", handlers.APIChatFriendsHandler)
        mux.HandleFunc("/api/chat/requests", handlers.APIChatRequestsHandler)
        mux.HandleFunc("/api/chat/messages", handlers.APIChatMessagesHandler)
        mux.HandleFunc("/api/chat/message/send", handlers.APIChatSendMessageHandler)
        mux.HandleFunc("/api/chat/message/edit", handlers.APIChatMessageEditHandler)
        mux.HandleFunc("/api/chat/message/delete", handlers.APIChatMessageDeleteHandler)
        mux.HandleFunc("/api/chat/friend/send", handlers.APIChatFriendSendHandler)
        mux.HandleFunc("/api/chat/friend/accept", handlers.APIChatFriendAcceptHandler)
        mux.HandleFunc("/api/chat/friend/reject", handlers.APIChatFriendRejectHandler)
        mux.HandleFunc("/api/chat/friend/sent", handlers.APIChatFriendSentHandler)
        mux.HandleFunc("/api/chat/search", handlers.APIChatSearchHandler)
        mux.HandleFunc("/api/chat/userstatus", handlers.APIChatUserStatusHandler)
        mux.HandleFunc("/api/chat/typing", handlers.APITypingHandler)
        mux.HandleFunc("/api/chat/friends/list", handlers.APIChatFriendsListHandler)
        mux.HandleFunc("/api/chat/msg-requests/count", handlers.APIChatMsgRequestsCountHandler)
        mux.HandleFunc("/api/chat/msg-requests", handlers.APIChatMsgRequestsHandler)
        mux.HandleFunc("/api/chat/msg-request/accept", handlers.APIChatAcceptMsgRequestHandler)
        mux.HandleFunc("/api/chat/msg-request/decline", handlers.APIChatDeclineMsgRequestHandler)
        mux.HandleFunc("/api/chat/conversation/hide", handlers.APIChatHideConvHandler)
        mux.HandleFunc("/api/chat/notifications/poll", handlers.APIChatNotificationsPollHandler)

        // Invite link
        mux.HandleFunc("/invite/", handlers.InviteHandler)

        // Marketplace
        mux.HandleFunc("/marketplace", handlers.MarketplaceHandler)
        mux.HandleFunc("/products/", handlers.ProductsRouter)
        mux.HandleFunc("/purchases", handlers.MyPurchasesHandler)
        mux.HandleFunc("/api/marketplace/products", handlers.APIMarketplaceProductsHandler)
        mux.HandleFunc("/api/marketplace/product/", handlers.APIMarketplaceProductHandler)
        mux.HandleFunc("/api/admin/mkt/images", handlers.APIAdminMktImagesHandler)

        // Crypto Payments
        mux.HandleFunc("/pay/", handlers.PaymentPageHandler)
        mux.HandleFunc("/api/pay/", handlers.APIPaymentRouter)

        // Forums
        mux.HandleFunc("/forums", handlers.ForumsHandler)
        mux.HandleFunc("/forums/post", handlers.ForumsPostCreateHandler)
        mux.HandleFunc("/forums/post/", handlers.ForumsPostDetailHandler)
        mux.HandleFunc("/forums/u/", handlers.ForumUserProfileHandler)
        mux.HandleFunc("/forums/server/", handlers.ForumsServerPageHandler)
        mux.HandleFunc("/forums/category/create", handlers.ForumsCategoryCreateHandler)
        mux.HandleFunc("/forums/server/create", handlers.ForumsServerCreateHandler)
        mux.HandleFunc("/api/forums/post/", handlers.ForumsVoteHandler)
        mux.HandleFunc("/api/forums/server/", handlers.ForumsServerAPIDispatch)
        mux.HandleFunc("/api/forums/comment/", handlers.ForumsCommentVoteHandler)
        mux.HandleFunc("/api/forums/user/", handlers.ForumsUserFollowHandler)
        mux.HandleFunc("/api/forums/search", handlers.ForumsSearchAPIHandler)
    mux.HandleFunc("/api/forums/handle-check", handlers.ForumsHandleCheckHandler)
        mux.HandleFunc("/api/forums/posts", handlers.ForumsPostsAPIHandler)

        // Catch-all: handles home, dynamic admin path, and unknown → redirect home
        mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                path := r.URL.Path

                if path == "/" {
                        handlers.HomeHandler(w, r)
                        return
                }

                ap := handlers.GetAdminPath()
                if path == ap || path == ap+"/" || strings.HasPrefix(path, ap+"/") {
                        handlers.AdminRouteHandler(w, r)
                        return
                }

                if !isKnownRoute(path) {
                        http.Redirect(w, r, "/", http.StatusFound)
                        return
                }

                http.Redirect(w, r, "/", http.StatusFound)
        })

        handler := middleware.SecurityHeaders(middleware.CSRF(mux))

        port := os.Getenv("PORT")
        if port == "" {
                port = "8080"
        }

        log.Printf("Shadow listening on :%s", port)
        if err := http.ListenAndServe(":"+port, handler); err != nil {
                log.Fatalf("server: %v", err)
        }
}
