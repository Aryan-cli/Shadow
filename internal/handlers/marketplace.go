package handlers

import (
        "context"
        "encoding/json"
        "fmt"
        "html/template"
        "io"
        "log"
        "net/http"
        "os"
        "path/filepath"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/session"
        "strconv"
        "strings"
        "time"
)

const marketplacePageSize = 10

var marketplaceCategories = []string{
        "Digital", "Physical", "Services", "Software", "Art", "Other",
}

// ─── Public Marketplace Page ───────────────────────────────────────────────

type MarketplacePageData struct {
        PageData
        Products    []db.MarketplaceProduct
        TopProducts []db.MarketplaceProduct
        Search      string
        Category    string
        Categories  []string
        IsAdmin     bool
        AdminPath   string
}

func MarketplaceHandler(w http.ResponseWriter, r *http.Request) {
        data := pageData(w, r)
        search := strings.TrimSpace(r.URL.Query().Get("q"))
        category := strings.TrimSpace(r.URL.Query().Get("cat"))

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        products, err := db.GetMarketplaceProducts(ctx, search, category, 0, marketplacePageSize)
        if err != nil {
                log.Printf("marketplace list: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        topProducts, _ := db.GetTopMarketplaceProducts(ctx, 10)

        tmpl, err := parseTemplates("marketplace.html")
        if err != nil {
                log.Printf("marketplace template: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        adminLoggedIn := isAdminLoggedIn(r)
        adminPath := ""
        if adminLoggedIn {
                adminPath = GetAdminPath()
        }

        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _ = tmpl.ExecuteTemplate(w, "base", MarketplacePageData{
                PageData:    data,
                Products:    products,
                TopProducts: topProducts,
                Search:      search,
                Category:    category,
                Categories:  marketplaceCategories,
                IsAdmin:     adminLoggedIn,
                AdminPath:   adminPath,
        })
}

// ─── Infinite Scroll API ───────────────────────────────────────────────────

func APIMarketplaceProductsHandler(w http.ResponseWriter, r *http.Request) {
        search := strings.TrimSpace(r.URL.Query().Get("q"))
        category := strings.TrimSpace(r.URL.Query().Get("cat"))
        offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
        if offset < 0 {
                offset = 0
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        products, err := db.GetMarketplaceProducts(ctx, search, category, offset, marketplacePageSize)
        if err != nil {
                w.WriteHeader(http.StatusInternalServerError)
                return
        }

        hasMore := len(products) == marketplacePageSize
        nextOffset := offset + len(products)

        funcMap := template.FuncMap{
                "formatPrice": func(p float64) string { return fmt.Sprintf("%.2f", p) },
                "trimStaticPrefix": func(path string) string {
                        return strings.TrimPrefix(path, "static/")
                },
                "truncate": func(s string, n int) string {
                        if len(s) <= n {
                                return s
                        }
                        return s[:n] + "…"
                },
                "stars": starsFunc,
                "seq": func(n int) []int {
                        s := make([]int, n)
                        for i := range s {
                                s[i] = i
                        }
                        return s
                },
                "int": func(f float64) int { return int(f + 0.5) },
                "add1": func(i int) int { return i + 1 },
        }
        tmpl, err := template.New("mkt-cards").Funcs(funcMap).ParseFiles(
                filepath.Join("templates", "marketplace-cards.html"),
        )
        if err != nil {
                w.WriteHeader(http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _ = tmpl.ExecuteTemplate(w, "mkt-cards", struct {
                Products   []db.MarketplaceProduct
                HasMore    bool
                NextOffset int
                Search     string
                Category   string
        }{products, hasMore, nextOffset, search, category})
}

// ─── Product Detail Page ────────────────────────────────────────────────────

type ProductDetailData struct {
        PageData
        Product      *db.MarketplaceProduct
        Images       []db.MarketplaceImage
        Reviews      []db.MarketplaceReview
        UserRating   int
        LoggedIn     bool
        UserID       int
        HasPurchased bool
        CSRFToken    string
}

// ProductsRouter dispatches /products/{id} and /products/{id}/checkout
func ProductsRouter(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        if strings.HasSuffix(path, "/checkout") {
                ProductCheckoutHandler(w, r)
                return
        }
        ProductDetailHandler(w, r)
}

func ProductDetailHandler(w http.ResponseWriter, r *http.Request) {
        idStr := strings.TrimPrefix(r.URL.Path, "/products/")
        idStr = strings.Split(idStr, "/")[0]
        id, err := strconv.Atoi(idStr)
        if err != nil {
                http.Redirect(w, r, "/marketplace", http.StatusFound)
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        p, err := db.GetMarketplaceProduct(ctx, id)
        if err != nil || p == nil {
                http.Redirect(w, r, "/marketplace", http.StatusFound)
                return
        }

        // Increment view count in background
        go func() {
                bgCtx, bgCancel := context.WithTimeout(context.Background(), 3*time.Second)
                defer bgCancel()
                db.IncrementProductView(bgCtx, id)
        }()

        images, _ := db.GetProductImages(ctx, id)

        _, sessData, _ := session.FromRequest(r)
        loggedIn := sessData != nil
        userID := 0
        userRating := 0
        hasPurchased := false
        if loggedIn {
                userID = sessData.UserID
                userRating, _ = db.GetUserRatingForProduct(ctx, id, userID)
                hasPurchased, _ = db.HasUserPurchased(ctx, userID, id)
        }

        reviews, _ := db.GetProductReviews(ctx, id, userID)

        data := pageData(w, r)
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)

        tmpl, err := parseTemplates("product-detail.html")
        if err != nil {
                log.Printf("product detail template: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _ = tmpl.ExecuteTemplate(w, "base", ProductDetailData{
                PageData:     data,
                Product:      p,
                Images:       images,
                Reviews:      reviews,
                UserRating:   userRating,
                LoggedIn:     loggedIn,
                UserID:       userID,
                HasPurchased: hasPurchased,
                CSRFToken:    csrfToken,
        })
}

// ─── Purchase API ────────────────────────────────────────────────────────────

// APIPurchaseHandler is intentionally disabled.
// Purchases are only recorded by the background payment confirmation worker
// after on-chain verification. Direct purchase recording without payment is not allowed.
func APIPurchaseHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusForbidden)
        w.Write([]byte(`{"error":"forbidden"}`))
}

// ─── My Purchases Page ───────────────────────────────────────────────────────

type MyPurchasesData struct {
        PageData
        Purchases []db.Purchase
        Search    string
        LoggedIn  bool
}

func MyPurchasesHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Redirect(w, r, "/login", http.StatusFound)
                return
        }

        search := strings.TrimSpace(r.URL.Query().Get("q"))

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        purchases, err := db.GetUserPurchases(ctx, sessData.UserID, search)
        if err != nil {
                log.Printf("get purchases: %v", err)
        }

        data := pageData(w, r)
        tmpl, err := parseTemplates("my-purchases.html")
        if err != nil {
                log.Printf("my-purchases template: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _ = tmpl.ExecuteTemplate(w, "base", MyPurchasesData{
                PageData:  data,
                Purchases: purchases,
                Search:    search,
                LoggedIn:  true,
        })
}

// ─── Rating API ─────────────────────────────────────────────────────────────

func APIProductRatingHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")

        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                w.WriteHeader(http.StatusUnauthorized)
                w.Write([]byte(`{"error":"login required"}`))
                return
        }

        idStr := strings.TrimPrefix(r.URL.Path, "/api/marketplace/product/")
        idStr = strings.Split(idStr, "/")[0]
        productID, err := strconv.Atoi(idStr)
        if err != nil {
                w.WriteHeader(http.StatusBadRequest)
                w.Write([]byte(`{"error":"bad product id"}`))
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        // Only verified buyers can rate
        hasPurchased, _ := db.HasUserPurchased(ctx, sessData.UserID, productID)
        if !hasPurchased {
                w.WriteHeader(http.StatusForbidden)
                w.Write([]byte(`{"error":"only verified buyers can rate this product"}`))
                return
        }

        // DELETE: remove star rating
        if r.Method == http.MethodDelete {
                if err := db.DeleteRating(ctx, productID, sessData.UserID); err != nil {
                        log.Printf("delete rating: %v", err)
                        w.WriteHeader(http.StatusInternalServerError)
                        w.Write([]byte(`{"error":"failed to remove rating"}`))
                        return
                }
                w.Write([]byte(`{"ok":true}`))
                return
        }

        if r.Method != http.MethodPost {
                w.WriteHeader(http.StatusMethodNotAllowed)
                w.Write([]byte(`{"error":"method not allowed"}`))
                return
        }

        r.ParseForm()
        ratingVal, _ := strconv.Atoi(r.FormValue("rating"))
        if ratingVal < 1 || ratingVal > 5 {
                w.WriteHeader(http.StatusBadRequest)
                w.Write([]byte(`{"error":"rating must be 1-5"}`))
                return
        }

        if err := db.AddOrUpdateRating(ctx, productID, sessData.UserID, ratingVal, ""); err != nil {
                log.Printf("add rating: %v", err)
                w.WriteHeader(http.StatusInternalServerError)
                w.Write([]byte(`{"error":"failed to save rating"}`))
                return
        }

        w.Write([]byte(`{"ok":true}`))
}

func APIProductReviewHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")

        if r.Method != http.MethodPost {
                w.WriteHeader(http.StatusMethodNotAllowed)
                w.Write([]byte(`{"error":"method not allowed"}`))
                return
        }

        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                w.WriteHeader(http.StatusUnauthorized)
                w.Write([]byte(`{"error":"login required"}`))
                return
        }

        idStr := strings.TrimPrefix(r.URL.Path, "/api/marketplace/product/")
        idStr = strings.Split(idStr, "/")[0]
        productID, err := strconv.Atoi(idStr)
        if err != nil {
                w.WriteHeader(http.StatusBadRequest)
                w.Write([]byte(`{"error":"bad product id"}`))
                return
        }

        r.ParseForm()
        comment := strings.TrimSpace(r.FormValue("comment"))
        if comment == "" {
                w.WriteHeader(http.StatusBadRequest)
                w.Write([]byte(`{"error":"review cannot be empty"}`))
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        isVerifiedBuyer, _ := db.HasUserPurchased(ctx, sessData.UserID, productID)
        if err := db.AddReview(ctx, productID, sessData.UserID, comment, isVerifiedBuyer); err != nil {
                log.Printf("add review: %v", err)
                w.WriteHeader(http.StatusInternalServerError)
                w.Write([]byte(`{"error":"failed to post review"}`))
                return
        }

        w.Write([]byte(`{"ok":true}`))
}

func APIDeleteReviewHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")

        if r.Method != http.MethodDelete {
                w.WriteHeader(http.StatusMethodNotAllowed)
                w.Write([]byte(`{"error":"method not allowed"}`))
                return
        }

        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                w.WriteHeader(http.StatusUnauthorized)
                w.Write([]byte(`{"error":"login required"}`))
                return
        }

        // URL: /api/marketplace/product/{id}/review/{reviewID}
        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/marketplace/product/"), "/")
        if len(parts) < 3 {
                w.WriteHeader(http.StatusBadRequest)
                w.Write([]byte(`{"error":"bad request"}`))
                return
        }
        reviewID, err := strconv.Atoi(parts[2])
        if err != nil {
                w.WriteHeader(http.StatusBadRequest)
                w.Write([]byte(`{"error":"bad review id"}`))
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        if err := db.DeleteReview(ctx, reviewID, sessData.UserID); err != nil {
                log.Printf("delete review: %v", err)
                w.WriteHeader(http.StatusInternalServerError)
                w.Write([]byte(`{"error":"failed to delete review"}`))
                return
        }

        w.Write([]byte(`{"ok":true}`))
}

// ─── Buy API ───────────────────────────────────────────────────────────────

func APIMarketplaceProductHandler(w http.ResponseWriter, r *http.Request) {
        parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/marketplace/product/"), "/")
        if len(parts) >= 2 && parts[1] == "rate" {
                APIProductRatingHandler(w, r)
                return
        }
        if len(parts) >= 2 && parts[1] == "purchase" {
                APIPurchaseHandler(w, r)
                return
        }
        if len(parts) >= 2 && parts[1] == "review" {
                if r.Method == http.MethodDelete && len(parts) >= 3 {
                        APIDeleteReviewHandler(w, r)
                } else {
                        APIProductReviewHandler(w, r)
                }
                return
        }

        id, err := strconv.Atoi(parts[0])
        if err != nil {
                http.NotFound(w, r)
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        p, err := db.GetMarketplaceProduct(ctx, id)
        if err != nil || p == nil {
                http.NotFound(w, r)
                return
        }

        // Only return after_buy content if the user is authenticated and has purchased this product.
        _, sessData, _ := session.FromRequest(r)
        var afterBuyLink, afterBuyButtonName, afterBuyComment string
        if sessData != nil {
                purchased, _ := db.HasUserPurchased(ctx, sessData.UserID, id)
                if purchased {
                        afterBuyLink = p.AfterBuyLink
                        afterBuyButtonName = p.AfterBuyButtonName
                        afterBuyComment = p.AfterBuyComment
                }
        }

        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]string{
                "after_buy_link":        afterBuyLink,
                "after_buy_button_name": afterBuyButtonName,
                "after_buy_comment":     afterBuyComment,
        })
}

// ─── Admin: Images API ─────────────────────────────────────────────────────

func APIAdminMktImagesHandler(w http.ResponseWriter, r *http.Request) {
        if !isAdminLoggedIn(r) {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        idStr := r.URL.Query().Get("id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
                http.Error(w, "bad id", http.StatusBadRequest)
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        imgs, err := db.GetProductImages(ctx, id)
        if err != nil {
                http.Error(w, "error", http.StatusInternalServerError)
                return
        }

        type imgJSON struct {
                ID          int    `json:"id"`
                URL         string `json:"url"`
                IsThumbnail bool   `json:"is_thumbnail"`
        }
        out := make([]imgJSON, 0, len(imgs))
        for _, img := range imgs {
                out = append(out, imgJSON{
                        ID:          img.ID,
                        URL:         "/static/" + strings.TrimPrefix(img.ImagePath, "static/"),
                        IsThumbnail: img.IsThumbnail,
                })
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(out)
}

// ─── Admin: Product List ───────────────────────────────────────────────────

type AdminMarketplaceListData struct {
        AdminBase
        Products   []db.MarketplaceProduct
        Categories []string
        Error      string
        Success    string
}

func AdminMarketplaceHandler(w http.ResponseWriter, r *http.Request) {
        if !adminGuard(w, r) {
                return
        }
        ap := GetAdminPath()
        path := r.URL.Path
        suffix := strings.TrimPrefix(path, ap+"/marketplace")

        switch {
        case suffix == "" || suffix == "/":
                adminMktList(w, r)

        case suffix == "/add":
                adminMktAdd(w, r)

        default:
                parts := strings.Split(strings.Trim(suffix, "/"), "/")
                if len(parts) < 2 {
                        http.Redirect(w, r, ap+"/marketplace", http.StatusFound)
                        return
                }
                id, err := strconv.Atoi(parts[0])
                if err != nil {
                        http.Redirect(w, r, ap+"/marketplace", http.StatusFound)
                        return
                }
                action := parts[1]
                switch action {
                case "edit":
                        adminMktEdit(w, r, id)
                case "delete":
                        adminMktDelete(w, r, id)
                case "image-delete":
                        if len(parts) >= 3 {
                                imgID, _ := strconv.Atoi(parts[2])
                                adminMktImageDelete(w, r, id, imgID)
                        }
                case "set-thumbnail":
                        if len(parts) >= 3 {
                                imgID, _ := strconv.Atoi(parts[2])
                                adminMktSetThumbnail(w, r, id, imgID)
                        }
                default:
                        http.Redirect(w, r, ap+"/marketplace", http.StatusFound)
                }
        }
}

func adminMktList(w http.ResponseWriter, r *http.Request) {
        ap := GetAdminPath()
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        products, err := db.AdminListMarketplaceProducts(ctx)
        if err != nil {
                log.Printf("admin mkt list: %v", err)
        }

        renderAdminPage(w, r, "admin-marketplace.html", "admin-marketplace", AdminMarketplaceListData{
                AdminBase:  AdminBase{CSRFToken: csrfToken, ActivePage: "marketplace", AdminPath: ap},
                Products:   products,
                Categories: marketplaceCategories,
                Success:    r.URL.Query().Get("success"),
                Error:      r.URL.Query().Get("error"),
        })
}

func adminMktAdd(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Redirect(w, r, GetAdminPath()+"/marketplace", http.StatusFound)
                return
        }
        ap := GetAdminPath()
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)

        if err := r.ParseMultipartForm(50 << 20); err != nil {
                log.Printf("admin mkt add parse: %v", err)
                adminMktRedirectError(w, r, "Failed to parse form.")
                return
        }

        title := strings.TrimSpace(r.FormValue("title"))
        description := strings.TrimSpace(r.FormValue("description"))
        category := strings.TrimSpace(r.FormValue("category"))
        priceStr := strings.TrimSpace(r.FormValue("price"))
        afterBuyLink := strings.TrimSpace(r.FormValue("after_buy_link"))
        afterBuyBtn := strings.TrimSpace(r.FormValue("after_buy_button_name"))
        afterBuyComment := strings.TrimSpace(r.FormValue("after_buy_comment"))
        _ = csrfToken

        if title == "" {
                adminMktRedirectError(w, r, "Title is required.")
                return
        }
        if afterBuyBtn == "" {
                afterBuyBtn = "Download"
        }

        price, _ := strconv.ParseFloat(priceStr, 64)
        if price < 0 {
                price = 0
        }

        ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
        defer cancel()

        productID, err := db.AddMarketplaceProduct(ctx, title, description, category, price, 0, afterBuyLink, afterBuyBtn, afterBuyComment)
        if err != nil {
                log.Printf("admin mkt add product: %v", err)
                adminMktRedirectError(w, r, "Failed to save product.")
                return
        }

        files := r.MultipartForm.File["images"]
        thumbnailIndex := 0
        thumbStr := strings.TrimSpace(r.FormValue("thumbnail_index"))
        if idx, err := strconv.Atoi(thumbStr); err == nil && idx >= 0 {
                thumbnailIndex = idx
        }

        for i, fh := range files {
                f, err := fh.Open()
                if err != nil {
                        continue
                }
                imgPath, err := saveUploadedImage(f, fh.Filename)
                f.Close()
                if err != nil {
                        continue
                }
                isThumbnail := (i == thumbnailIndex)
                _ = db.AddProductImage(ctx, productID, imgPath, isThumbnail, i)
        }

        http.Redirect(w, r, ap+"/marketplace?success=Product+added+successfully", http.StatusSeeOther)
}

func adminMktEdit(w http.ResponseWriter, r *http.Request, id int) {
        ap := GetAdminPath()
        if r.Method != http.MethodPost {
                http.Redirect(w, r, ap+"/marketplace", http.StatusFound)
                return
        }

        if err := r.ParseMultipartForm(50 << 20); err != nil {
                adminMktRedirectError(w, r, "Failed to parse form.")
                return
        }

        title := strings.TrimSpace(r.FormValue("title"))
        description := strings.TrimSpace(r.FormValue("description"))
        category := strings.TrimSpace(r.FormValue("category"))
        priceStr := strings.TrimSpace(r.FormValue("price"))
        afterBuyLink := strings.TrimSpace(r.FormValue("after_buy_link"))
        afterBuyBtn := strings.TrimSpace(r.FormValue("after_buy_button_name"))
        afterBuyComment := strings.TrimSpace(r.FormValue("after_buy_comment"))

        if title == "" {
                adminMktRedirectError(w, r, "Title is required.")
                return
        }
        if afterBuyBtn == "" {
                afterBuyBtn = "Download"
        }

        price, _ := strconv.ParseFloat(priceStr, 64)

        ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
        defer cancel()

        if err := db.UpdateMarketplaceProduct(ctx, id, title, description, category, price, afterBuyLink, afterBuyBtn, afterBuyComment); err != nil {
                log.Printf("admin mkt edit: %v", err)
                adminMktRedirectError(w, r, "Failed to update product.")
                return
        }

        files := r.MultipartForm.File["images"]
        existingCount := 0
        if imgs, err := db.GetProductImages(ctx, id); err == nil {
                existingCount = len(imgs)
        }
        thumbnailIndex := -1
        thumbStr := strings.TrimSpace(r.FormValue("thumbnail_index"))
        if idx, err := strconv.Atoi(thumbStr); err == nil {
                thumbnailIndex = idx
        }
        for i, fh := range files {
                f, err := fh.Open()
                if err != nil {
                        continue
                }
                imgPath, err := saveUploadedImage(f, fh.Filename)
                f.Close()
                if err != nil {
                        continue
                }
                isThumbnail := (thumbnailIndex == existingCount+i)
                _ = db.AddProductImage(ctx, id, imgPath, isThumbnail, existingCount+i)
        }

        http.Redirect(w, r, ap+"/marketplace?success=Product+updated+successfully", http.StatusSeeOther)
}

func adminMktDelete(w http.ResponseWriter, r *http.Request, id int) {
        if r.Method != http.MethodPost {
                http.Redirect(w, r, GetAdminPath()+"/marketplace", http.StatusFound)
                return
        }
        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()
        if err := db.DeleteMarketplaceProduct(ctx, id); err != nil {
                log.Printf("admin mkt delete: %v", err)
                adminMktRedirectError(w, r, "Failed to delete product.")
                return
        }
        http.Redirect(w, r, GetAdminPath()+"/marketplace?success=Product+deleted", http.StatusSeeOther)
}

func adminMktImageDelete(w http.ResponseWriter, r *http.Request, productID, imageID int) {
        if r.Method != http.MethodPost {
                w.WriteHeader(http.StatusMethodNotAllowed)
                return
        }
        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()
        _ = db.DeleteProductImage(ctx, imageID, productID)
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func adminMktSetThumbnail(w http.ResponseWriter, r *http.Request, productID, imageID int) {
        if r.Method != http.MethodPost {
                w.WriteHeader(http.StatusMethodNotAllowed)
                return
        }
        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()
        _ = db.SetThumbnail(ctx, imageID, productID)
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func adminMktRedirectError(w http.ResponseWriter, r *http.Request, msg string) {
        http.Redirect(w, r, GetAdminPath()+"/marketplace?error="+msg, http.StatusSeeOther)
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func starsFunc(rating float64, max int) []int {
        s := make([]int, max)
        full := int(rating + 0.5)
        for i := range s {
                if i < full {
                        s[i] = 1
                } else {
                        s[i] = 0
                }
        }
        return s
}

func saveUploadedImage(f io.Reader, filename string) (string, error) {
        ext := strings.ToLower(filepath.Ext(filename))
        allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
        if !allowed[ext] {
                return "", fmt.Errorf("unsupported file type: %s", ext)
        }
        if err := os.MkdirAll("static/uploads/marketplace", 0755); err != nil {
                return "", err
        }
        fname := fmt.Sprintf("mkt_%d%s", time.Now().UnixNano(), ext)
        dest := filepath.Join("static/uploads/marketplace", fname)
        out, err := os.Create(dest)
        if err != nil {
                return "", err
        }
        defer out.Close()
        _, err = io.Copy(out, f)
        return "static/uploads/marketplace/" + fname, err
}
