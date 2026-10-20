package handlers

import (
        "context"
        "encoding/json"
        "fmt"
        "html/template"
        "log"
        "net/http"
        "path/filepath"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/payment"
        "shadow/internal/session"
        "strconv"
        "strings"
        "time"

        "crypto/rand"
        "encoding/hex"
)

// ── Coin Selection Page ───────────────────────────────────────────────────────

type CoinSelectData struct {
        PageData
        Product     *db.MarketplaceProduct
        CSRFToken   string
        BTCEnabled  bool
        XMREnabled  bool
        XMRMaint    bool
        PendingPayID string // if already has a pending payment
}

func ProductCheckoutHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Redirect(w, r, "/login", http.StatusFound)
                return
        }

        idStr := strings.TrimPrefix(r.URL.Path, "/products/")
        idStr = strings.Split(idStr, "/")[0]
        productID, err := strconv.Atoi(idStr)
        if err != nil {
                http.Redirect(w, r, "/marketplace", http.StatusFound)
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        p, err := db.GetMarketplaceProduct(ctx, productID)
        if err != nil || p == nil {
                http.Redirect(w, r, "/marketplace", http.StatusFound)
                return
        }

        // Check if already purchased
        bought, _ := db.HasUserPurchased(ctx, sessData.UserID, productID)
        if bought {
                http.Redirect(w, r, "/products/"+idStr, http.StatusFound)
                return
        }

        // Check for existing pending payment
        existing, _ := db.GetPendingPaymentsForUser(ctx, sessData.UserID, productID)

        if r.Method == http.MethodPost {
                r.ParseForm()
                coin := strings.ToLower(strings.TrimSpace(r.FormValue("coin")))
                if coin != "btc" && coin != "xmr" {
                        http.Redirect(w, r, "/products/"+idStr+"/checkout", http.StatusFound)
                        return
                }

                // Check coin enabled
                btcEnabled := db.GetSiteSettingInt("btc_enabled", 1) == 1
                xmrEnabled := db.GetSiteSettingInt("xmr_enabled", 0) == 1
                xmrMaint := db.GetSiteSettingInt("xmr_maintenance", 1) == 1

                if coin == "btc" && !btcEnabled {
                        http.Redirect(w, r, "/products/"+idStr+"/checkout", http.StatusFound)
                        return
                }
                if coin == "xmr" && (!xmrEnabled || xmrMaint) {
                        http.Redirect(w, r, "/products/"+idStr+"/checkout", http.StatusFound)
                        return
                }

                // If existing pending payment for same coin, reuse it
                if existing != nil && existing.Coin == coin {
                        http.Redirect(w, r, "/pay/"+existing.ID, http.StatusFound)
                        return
                }

                // Block if user has any active payment for a DIFFERENT product
                anyActive, _ := db.GetAnyActivePendingPaymentForUser(ctx, sessData.UserID)
                if anyActive != nil && anyActive.ProductID != productID {
                        http.Redirect(w, r, "/products/"+idStr+"/checkout?err=active_payment", http.StatusFound)
                        return
                }

                // Create new payment
                payID, err := createPayment(ctx, sessData.UserID, productID, p.Price, coin)
                if err != nil {
                        log.Printf("create payment: %v", err)
                        http.Error(w, "Failed to create payment. Please try again.", http.StatusInternalServerError)
                        return
                }
                http.Redirect(w, r, "/pay/"+payID, http.StatusFound)
                return
        }

        // GET: show coin selection
        if existing != nil {
                http.Redirect(w, r, "/pay/"+existing.ID, http.StatusFound)
                return
        }

        btcEnabled := db.GetSiteSettingInt("btc_enabled", 1) == 1
        xmrEnabled := db.GetSiteSettingInt("xmr_enabled", 0) == 1
        xmrMaint := db.GetSiteSettingInt("xmr_maintenance", 1) == 1

        data := pageData(w, r)
        csrfToken := middleware.GetOrCreateCSRFToken(w, r)

        tmpl, err := parsePaymentTemplate("payment-coin-select.html")
        if err != nil {
                log.Printf("coin select template: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _ = tmpl.ExecuteTemplate(w, "base", CoinSelectData{
                PageData:   data,
                Product:    p,
                CSRFToken:  csrfToken,
                BTCEnabled: btcEnabled,
                XMREnabled: xmrEnabled && !xmrMaint,
                XMRMaint:   xmrMaint,
        })
}

func createPayment(ctx context.Context, userID, productID int, priceUSD float64, coin string) (string, error) {
        payID := generatePaymentID()

        var walletAddress string
        var amountCrypto float64
        derivationIdx := -1

        if coin == "btc" {
                mnemonic := payment.GetHDMnemonic()
                if mnemonic == "" {
                        return "", fmt.Errorf("BTC_HD_MNEMONIC env variable not set — HD wallet not configured")
                }

                idx, err := db.GetAndIncrementHDIndex(ctx)
                if err != nil {
                        return "", fmt.Errorf("get HD index: %w", err)
                }
                derivationIdx = idx

                addr, err := payment.DeriveBTCAddress(mnemonic, uint32(idx))
                if err != nil {
                        return "", fmt.Errorf("derive BTC address: %w", err)
                }
                walletAddress = addr

                btcPrice, err := payment.GetBTCPrice()
                if err != nil {
                        return "", fmt.Errorf("get BTC price: %w", err)
                }
                amountCrypto = payment.USDToBTC(priceUSD, btcPrice)
                log.Printf("[payment] HD address index=%d address=%s", idx, addr)
        } else if coin == "xmr" {
                if !payment.XMREnabled() {
                        return "", fmt.Errorf("XMR_SPEND_KEY not set — XMR wallet not configured")
                }

                idx, err := db.GetAndIncrementXMRIndex(ctx)
                if err != nil {
                        return "", fmt.Errorf("get XMR index: %w", err)
                }
                derivationIdx = idx

                addr, err := payment.DeriveXMRSubaddressFromEnv(uint32(idx))
                if err != nil {
                        return "", fmt.Errorf("derive XMR subaddress: %w", err)
                }
                walletAddress = addr

                xmrPrice, err := payment.GetXMRPrice()
                if err != nil {
                        return "", fmt.Errorf("get XMR price: %w", err)
                }
                amountCrypto = payment.USDToXMR(priceUSD, xmrPrice)
                log.Printf("[payment] XMR subaddress index=%d address=%s", idx, addr)
        } else {
                return "", fmt.Errorf("unsupported coin: %s", coin)
        }

        cp := &db.CryptoPayment{
                ID:              payID,
                UserID:          userID,
                ProductID:       productID,
                Coin:            coin,
                WalletAddress:   walletAddress,
                PrivateKeyWIF:   "",
                AmountCrypto:    amountCrypto,
                AmountUSD:       priceUSD,
                ExpiresAt:       time.Now().Add(time.Duration(db.GetSiteSettingInt("payment_window_minutes", 30)) * time.Minute),
                DerivationIndex: derivationIdx,
        }

        if err := db.CreateCryptoPayment(ctx, cp); err != nil {
                return "", err
        }
        return payID, nil
}

func generatePaymentID() string {
        b := make([]byte, 16)
        _, _ = rand.Read(b)
        return hex.EncodeToString(b)
}

// ── Payment Page ──────────────────────────────────────────────────────────────

type PaymentPageData struct {
        PageData
        Payment      *db.CryptoPayment
        Product      *db.MarketplaceProduct
        SecondsLeft  int
        ExpiresISO   string
}

func PaymentPageHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Redirect(w, r, "/login", http.StatusFound)
                return
        }

        payID := strings.TrimPrefix(r.URL.Path, "/pay/")
        payID = strings.Split(payID, "/")[0]

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        cp, err := db.GetCryptoPayment(ctx, payID)
        if err != nil || cp == nil {
                http.Redirect(w, r, "/marketplace", http.StatusFound)
                return
        }

        // Only the owner can view their payment
        if cp.UserID != sessData.UserID {
                http.Redirect(w, r, "/marketplace", http.StatusFound)
                return
        }

        p, _ := db.GetMarketplaceProduct(ctx, cp.ProductID)

        secsLeft := int(time.Until(cp.ExpiresAt).Seconds())
        if secsLeft < 0 {
                secsLeft = 0
        }

        data := pageData(w, r)
        tmpl, err := parsePaymentTemplate("payment-page.html")
        if err != nil {
                log.Printf("payment page template: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _ = tmpl.ExecuteTemplate(w, "base", PaymentPageData{
                PageData:    data,
                Payment:     cp,
                Product:     p,
                SecondsLeft: secsLeft,
                ExpiresISO:  cp.ExpiresAt.UTC().Format(time.RFC3339),
        })
}

// ── Payment Status API ────────────────────────────────────────────────────────

func APIPaymentStatusHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")

        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                w.WriteHeader(http.StatusUnauthorized)
                json.NewEncoder(w).Encode(map[string]string{"error": "login required"})
                return
        }

        payID := strings.TrimPrefix(r.URL.Path, "/api/pay/")
        payID = strings.Split(payID, "/")[0]

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        cp, err := db.GetCryptoPayment(ctx, payID)
        if err != nil || cp == nil {
                w.WriteHeader(http.StatusNotFound)
                json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
                return
        }

        if cp.UserID != sessData.UserID {
                w.WriteHeader(http.StatusForbidden)
                json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
                return
        }

        secsLeft := int(time.Until(cp.ExpiresAt).Seconds())
        if secsLeft < 0 {
                secsLeft = 0
        }

        resp := map[string]interface{}{
                "status":          cp.Status,
                "received_amount": cp.ReceivedAmount,
                "amount_crypto":   cp.AmountCrypto,
                "seconds_left":    secsLeft,
                "product_id":      cp.ProductID,
        }

        // When confirmed, include after_buy data so the payment page can show the download link immediately.
        // We only expose this to the payment owner (verified above via cp.UserID == sessData.UserID).
        if cp.Status == "confirmed" {
                p, err := db.GetMarketplaceProduct(ctx, cp.ProductID)
                if err == nil && p != nil {
                        btnName := p.AfterBuyButtonName
                        if btnName == "" {
                                btnName = "Download"
                        }
                        resp["after_buy_link"] = p.AfterBuyLink
                        resp["after_buy_button"] = btnName
                        resp["after_buy_comment"] = p.AfterBuyComment
                }
        }

        json.NewEncoder(w).Encode(resp)
}

// ── Cancel Payment API ────────────────────────────────────────────────────────

func APIPaymentCancelHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")

        if r.Method != http.MethodPost {
                w.WriteHeader(http.StatusMethodNotAllowed)
                return
        }

        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                w.WriteHeader(http.StatusUnauthorized)
                json.NewEncoder(w).Encode(map[string]string{"error": "login required"})
                return
        }

        payID := strings.TrimPrefix(r.URL.Path, "/api/pay/")
        payID = strings.Split(payID, "/")[0]

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        if err := db.CancelPendingPayment(ctx, payID, sessData.UserID); err != nil {
                w.WriteHeader(http.StatusInternalServerError)
                json.NewEncoder(w).Encode(map[string]string{"error": "cancel failed"})
                return
        }

        json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// ── Active Payment Check API ──────────────────────────────────────────────────

func APIActivePendingPaymentHandler(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")

        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                json.NewEncoder(w).Encode(map[string]interface{}{"active": false})
                return
        }

        productIDStr := r.URL.Query().Get("product_id")
        productID, err := strconv.Atoi(productIDStr)
        if err != nil {
                json.NewEncoder(w).Encode(map[string]interface{}{"active": false})
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        cp, err := db.GetPendingPaymentsForUser(ctx, sessData.UserID, productID)
        if err != nil || cp == nil {
                json.NewEncoder(w).Encode(map[string]interface{}{"active": false})
                return
        }

        secsLeft := int(time.Until(cp.ExpiresAt).Seconds())
        if secsLeft < 0 {
                secsLeft = 0
        }

        json.NewEncoder(w).Encode(map[string]interface{}{
                "active":       true,
                "payment_id":   cp.ID,
                "coin":         cp.Coin,
                "seconds_left": secsLeft,
        })
}

// ── Background Workers ────────────────────────────────────────────────────────

// StartPaymentWorkers launches background goroutines for payment processing.
func StartPaymentWorkers() {
        go paymentCheckLoop()
        go paymentCleanupLoop()
}

func paymentCheckLoop() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()
        for range ticker.C {
                processPendingPayments()
        }
}

func paymentCleanupLoop() {
        ticker := time.NewTicker(15 * time.Minute)
        defer ticker.Stop()
        for range ticker.C {
                cleanupExpiredPayments()
        }
}

func processPendingPayments() {
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        payments, err := db.GetAllPendingPayments(ctx)
        if err != nil {
                log.Printf("[payment] list pending: %v", err)
                return
        }

        for _, cp := range payments {
                processSinglePayment(ctx, cp)
        }
}

func processSinglePayment(ctx context.Context, cp db.CryptoPayment) {
        now := time.Now()
        isExpired := now.After(cp.ExpiresAt)

        if cp.Coin == "btc" {
                receivedSat, err := payment.GetAddressReceivedSatoshis(cp.WalletAddress)
                if err != nil {
                        log.Printf("[payment] check balance %s: %v", cp.ID, err)
                        if isExpired {
                                _ = db.ExpirePayment(ctx, cp.ID)
                        }
                        return
                }

                receivedBTC := payment.SatoshisToBTC(receivedSat)
                if receivedBTC != cp.ReceivedAmount {
                        _ = db.UpdatePaymentReceived(ctx, cp.ID, receivedBTC)
                }

                requiredSat := payment.BTCToSatoshis(cp.AmountCrypto)
                if receivedSat >= requiredSat {
                        // Payment confirmed!
                        confirmPayment(ctx, cp)
                        return
                }

                if isExpired {
                        log.Printf("[payment] expired %s, received %.8f BTC (funds remain in HD address index=%d)", cp.ID, receivedBTC, cp.DerivationIndex)
                        _ = db.ExpirePayment(ctx, cp.ID)
                }
        } else if cp.Coin == "xmr" {
                if !payment.XMREnabled() {
                        return
                }
                receivedPico, err := payment.ScanXMRSubaddressDirect("", "", uint32(cp.DerivationIndex))
                if err != nil {
                        log.Printf("[payment] xmr check %s: %v", cp.ID, err)
                        if isExpired {
                                _ = db.ExpirePayment(ctx, cp.ID)
                        }
                        return
                }

                receivedXMR := payment.PiconeroToXMR(receivedPico)
                if receivedXMR != cp.ReceivedAmount {
                        _ = db.UpdatePaymentReceived(ctx, cp.ID, receivedXMR)
                }

                requiredPico := payment.XMRToPiconero(cp.AmountCrypto)
                if receivedPico >= requiredPico {
                        confirmPayment(ctx, cp)
                        return
                }

                if isExpired {
                        log.Printf("[payment] xmr expired %s received=%.12f XMR (subaddr index=%d)", cp.ID, receivedXMR, cp.DerivationIndex)
                        _ = db.ExpirePayment(ctx, cp.ID)
                }
        }
}

func confirmPayment(ctx context.Context, cp db.CryptoPayment) {
        log.Printf("[payment] confirming %s (HD index=%d address=%s)", cp.ID, cp.DerivationIndex, cp.WalletAddress)
        if err := db.UpdatePaymentStatus(ctx, cp.ID, "confirmed"); err != nil {
                log.Printf("[payment] confirm status update: %v", err)
                return
        }

        // Record purchase
        p, err := db.GetMarketplaceProduct(ctx, cp.ProductID)
        if err != nil || p == nil {
                log.Printf("[payment] product not found for payment %s", cp.ID)
        } else {
                if err := db.RecordPurchase(ctx, cp.UserID, cp.ProductID,
                        p.Title, p.Price, p.ThumbnailPath,
                        p.AfterBuyLink, p.AfterBuyButtonName, p.AfterBuyComment); err != nil {
                        log.Printf("[payment] record purchase: %v", err)
                }
        }
        // Funds remain in the HD-derived address — no sweep needed.
}

func cleanupExpiredPayments() {
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        scheduled, err := db.GetPaymentsScheduledForDelete(ctx)
        if err != nil {
                log.Printf("[payment] cleanup list: %v", err)
                return
        }

        for _, cp := range scheduled {
                // Save to permanent logs before deleting
                prd, _ := db.GetMarketplaceProduct(ctx, cp.ProductID)
                title := ""
                if prd != nil {
                        title = prd.Title
                }
                if err := db.InsertPaymentLog(ctx, cp, title); err != nil {
                        log.Printf("[payment] log %s: %v", cp.ID, err)
                }
                if err := db.DeletePayment(ctx, cp.ID); err != nil {
                        log.Printf("[payment] delete %s: %v", cp.ID, err)
                } else {
                        log.Printf("[payment] cleaned up wallet %s (status=%s, log saved)", cp.ID, cp.Status)
                }
        }
}

// ── API Router ────────────────────────────────────────────────────────────────

// APIPaymentRouter handles /api/pay/{id}/status and /api/pay/{id}/cancel
func APIPaymentRouter(w http.ResponseWriter, r *http.Request) {
        path := strings.TrimPrefix(r.URL.Path, "/api/pay/")
        parts := strings.SplitN(path, "/", 2)
        if len(parts) < 2 {
                // /api/pay/active?product_id=X
                APIActivePendingPaymentHandler(w, r)
                return
        }
        action := parts[1]
        switch action {
        case "status":
                APIPaymentStatusHandler(w, r)
        case "cancel":
                APIPaymentCancelHandler(w, r)
        default:
                http.NotFound(w, r)
        }
}

// ── Template helper ───────────────────────────────────────────────────────────

func parsePaymentTemplate(name string) (*template.Template, error) {
        funcMap := template.FuncMap{
                "formatPrice":      func(p float64) string { return fmt.Sprintf("%.2f", p) },
                "formatCrypto":     func(f float64) string { return fmt.Sprintf("%.8f", f) },
                "upper":            strings.ToUpper,
                "trimStaticPrefix": func(path string) string { return strings.TrimPrefix(path, "static/") },
        }
        return template.New("base").Funcs(funcMap).ParseFiles(
                filepath.Join("templates", "base.html"),
                filepath.Join("templates", name),
        )
}
