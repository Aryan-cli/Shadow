package db

import (
        "context"
        "database/sql"
        "fmt"
        "log"
        "time"
)

type CryptoPayment struct {
        ID              string
        UserID          int
        ProductID       int
        Coin            string
        WalletAddress   string
        PrivateKeyWIF   string
        AmountCrypto    float64
        AmountUSD       float64
        Status          string
        ReceivedAmount  float64
        ExpiresAt       time.Time
        ConfirmedAt     *time.Time
        CreatedAt       time.Time
        ScheduledDelete *time.Time
        DerivationIndex int
}

func MigratePayments() error {
        queries := []string{
                `CREATE TABLE IF NOT EXISTS crypto_payments (
                        id TEXT PRIMARY KEY,
                        user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        product_id INTEGER NOT NULL,
                        coin VARCHAR(10) NOT NULL,
                        wallet_address TEXT NOT NULL,
                        private_key_wif TEXT NOT NULL DEFAULT '',
                        amount_crypto DOUBLE PRECISION NOT NULL DEFAULT 0,
                        amount_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
                        status VARCHAR(20) NOT NULL DEFAULT 'pending',
                        received_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
                        expires_at TIMESTAMPTZ NOT NULL,
                        confirmed_at TIMESTAMPTZ,
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        scheduled_delete TIMESTAMPTZ
                )`,
                `CREATE INDEX IF NOT EXISTS idx_crypto_payments_user ON crypto_payments(user_id)`,
                `CREATE INDEX IF NOT EXISTS idx_crypto_payments_status ON crypto_payments(status)`,
                `CREATE INDEX IF NOT EXISTS idx_crypto_payments_expires ON crypto_payments(expires_at)`,
                `CREATE INDEX IF NOT EXISTS idx_crypto_payments_delete ON crypto_payments(scheduled_delete)`,
                `INSERT INTO site_settings (key, value) VALUES ('btc_enabled', '1') ON CONFLICT (key) DO NOTHING`,
                `INSERT INTO site_settings (key, value) VALUES ('xmr_enabled', '0') ON CONFLICT (key) DO NOTHING`,
                `INSERT INTO site_settings (key, value) VALUES ('btc_main_wallet', '') ON CONFLICT (key) DO NOTHING`,
                `INSERT INTO site_settings (key, value) VALUES ('xmr_main_wallet', '') ON CONFLICT (key) DO NOTHING`,
                `INSERT INTO site_settings (key, value) VALUES ('xmr_maintenance', '1') ON CONFLICT (key) DO NOTHING`,
                // One-time fix: repair btc_enabled=0 caused by hidden-input checkbox bug (flag prevents re-running)
                `DO $$ BEGIN
                   IF NOT EXISTS (SELECT 1 FROM site_settings WHERE key = 'btc_checkbox_fix_v1') THEN
                     UPDATE site_settings SET value = '1' WHERE key = 'btc_enabled' AND value = '0';
                     INSERT INTO site_settings (key, value) VALUES ('btc_checkbox_fix_v1', 'done') ON CONFLICT DO NOTHING;
                   END IF;
                 END $$`,
                `INSERT INTO site_settings (key, value) VALUES ('payment_window_minutes', '30') ON CONFLICT (key) DO NOTHING`,
                `INSERT INTO site_settings (key, value) VALUES ('payment_cleanup_hours', '5') ON CONFLICT (key) DO NOTHING`,
                `CREATE TABLE IF NOT EXISTS payment_logs (
                        id TEXT PRIMARY KEY,
                        user_id INTEGER,
                        product_id INTEGER NOT NULL,
                        product_title TEXT NOT NULL DEFAULT '',
                        coin VARCHAR(10) NOT NULL,
                        wallet_address TEXT NOT NULL,
                        amount_crypto DOUBLE PRECISION NOT NULL DEFAULT 0,
                        amount_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
                        status VARCHAR(20) NOT NULL,
                        received_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
                        created_at TIMESTAMPTZ,
                        expires_at TIMESTAMPTZ,
                        confirmed_at TIMESTAMPTZ,
                        logged_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE INDEX IF NOT EXISTS idx_payment_logs_user ON payment_logs(user_id)`,
                `CREATE INDEX IF NOT EXISTS idx_payment_logs_created ON payment_logs(created_at)`,
                `ALTER TABLE crypto_payments ADD COLUMN IF NOT EXISTS derivation_index INTEGER NOT NULL DEFAULT -1`,
                `ALTER TABLE payment_logs ADD COLUMN IF NOT EXISTS derivation_index INTEGER NOT NULL DEFAULT -1`,
                `ALTER TABLE crypto_payments ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW()`,
                `INSERT INTO site_settings (key, value) VALUES ('btc_hd_index', '0') ON CONFLICT (key) DO NOTHING`,
        }
        for _, q := range queries {
                if _, err := DB.Exec(q); err != nil {
                        return err
                }
        }
        return nil
}

// GetAndIncrementHDIndex atomically reads and increments the BIP44 BTC address index counter.
func GetAndIncrementHDIndex(ctx context.Context) (int, error) {
        var idx int
        err := DB.QueryRowContext(ctx, `
                UPDATE site_settings
                SET value = (CAST(value AS INTEGER) + 1)::TEXT
                WHERE key = 'btc_hd_index'
                RETURNING CAST(value AS INTEGER) - 1
        `).Scan(&idx)
        return idx, err
}

// GetAndIncrementXMRIndex atomically reads and increments the XMR subaddress index counter.
func GetAndIncrementXMRIndex(ctx context.Context) (int, error) {
        // Ensure key exists
        _, _ = DB.ExecContext(ctx, `
                INSERT INTO site_settings (key, value) VALUES ('xmr_hd_index', '1')
                ON CONFLICT (key) DO NOTHING
        `)
        var idx int
        err := DB.QueryRowContext(ctx, `
                UPDATE site_settings
                SET value = (CAST(value AS INTEGER) + 1)::TEXT
                WHERE key = 'xmr_hd_index'
                RETURNING CAST(value AS INTEGER) - 1
        `).Scan(&idx)
        return idx, err
}

func CreateCryptoPayment(ctx context.Context, p *CryptoPayment) error {
        _, err := DB.ExecContext(ctx, `
                INSERT INTO crypto_payments
                        (id, user_id, product_id, coin, wallet_address, private_key_wif,
                         amount_crypto, amount_usd, status, expires_at, derivation_index)
                VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending',$9,$10)
        `, p.ID, p.UserID, p.ProductID, p.Coin, p.WalletAddress, p.PrivateKeyWIF,
                p.AmountCrypto, p.AmountUSD, p.ExpiresAt, p.DerivationIndex)
        return err
}

func GetCryptoPayment(ctx context.Context, id string) (*CryptoPayment, error) {
        row := DB.QueryRowContext(ctx, `
                SELECT id, COALESCE(user_id,0), product_id, coin, wallet_address, private_key_wif,
                       amount_crypto, amount_usd, status, received_amount,
                       expires_at, confirmed_at, created_at, scheduled_delete, derivation_index
                FROM crypto_payments WHERE id=$1
        `, id)
        return scanPayment(row)
}

func GetAnyActivePendingPaymentForUser(ctx context.Context, userID int) (*CryptoPayment, error) {
        row := DB.QueryRowContext(ctx, `
                SELECT id, COALESCE(user_id,0), product_id, coin, wallet_address, private_key_wif,
                       amount_crypto, amount_usd, status, received_amount,
                       expires_at, confirmed_at, created_at, scheduled_delete, derivation_index
                FROM crypto_payments
                WHERE user_id=$1 AND status='pending' AND expires_at > NOW()
                ORDER BY created_at DESC LIMIT 1
        `, userID)
        p, err := scanPayment(row)
        if err == sql.ErrNoRows || p == nil {
                return nil, nil
        }
        return p, err
}

func InsertPaymentLog(ctx context.Context, cp CryptoPayment, productTitle string) error {
        _, err := DB.ExecContext(ctx, `
                INSERT INTO payment_logs
                        (id, user_id, product_id, product_title, coin, wallet_address,
                         amount_crypto, amount_usd, status, received_amount,
                         created_at, expires_at, confirmed_at, derivation_index)
                VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
                ON CONFLICT (id) DO UPDATE SET
                        status=EXCLUDED.status,
                        received_amount=EXCLUDED.received_amount,
                        confirmed_at=EXCLUDED.confirmed_at,
                        logged_at=NOW()
        `, cp.ID, cp.UserID, cp.ProductID, productTitle, cp.Coin, cp.WalletAddress,
                cp.AmountCrypto, cp.AmountUSD, cp.Status, cp.ReceivedAmount,
                cp.CreatedAt, cp.ExpiresAt, cp.ConfirmedAt, cp.DerivationIndex)
        return err
}

func GetPendingPaymentsForUser(ctx context.Context, userID, productID int) (*CryptoPayment, error) {
        row := DB.QueryRowContext(ctx, `
                SELECT id, COALESCE(user_id,0), product_id, coin, wallet_address, private_key_wif,
                       amount_crypto, amount_usd, status, received_amount,
                       expires_at, confirmed_at, created_at, scheduled_delete, derivation_index
                FROM crypto_payments
                WHERE user_id=$1 AND product_id=$2 AND status='pending' AND expires_at > NOW()
                ORDER BY created_at DESC LIMIT 1
        `, userID, productID)
        p, err := scanPayment(row)
        if err == sql.ErrNoRows || p == nil {
                return nil, nil
        }
        return p, err
}

func GetAllPendingPayments(ctx context.Context) ([]CryptoPayment, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT id, COALESCE(user_id,0), product_id, coin, wallet_address, private_key_wif,
                       amount_crypto, amount_usd, status, received_amount,
                       expires_at, confirmed_at, created_at, scheduled_delete, derivation_index
                FROM crypto_payments WHERE status='pending'
        `)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var payments []CryptoPayment
        for rows.Next() {
                p, err := scanPaymentRow(rows)
                if err != nil {
                        continue
                }
                payments = append(payments, p)
        }
        return payments, rows.Err()
}

func GetPaymentsScheduledForDelete(ctx context.Context) ([]CryptoPayment, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT id, COALESCE(user_id,0), product_id, coin, wallet_address, private_key_wif,
                       amount_crypto, amount_usd, status, received_amount,
                       expires_at, confirmed_at, created_at, scheduled_delete, derivation_index
                FROM crypto_payments
                WHERE scheduled_delete IS NOT NULL AND scheduled_delete <= NOW()
                  AND status IN ('confirmed','expired','cancelled')
        `)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var payments []CryptoPayment
        for rows.Next() {
                p, err := scanPaymentRow(rows)
                if err != nil {
                        continue
                }
                payments = append(payments, p)
        }
        return payments, rows.Err()
}

func UpdatePaymentStatus(ctx context.Context, id, status string) error {
        if status == "confirmed" {
                hours := GetSiteSettingInt("payment_cleanup_hours", 5)
                _, err := DB.ExecContext(ctx, `
                        UPDATE crypto_payments SET status=$2, confirmed_at=NOW(),
                        scheduled_delete=NOW() + ($3 || ' hours')::interval
                        WHERE id=$1
                `, id, status, hours)
                return err
        }
        _, err := DB.ExecContext(ctx, `UPDATE crypto_payments SET status=$2 WHERE id=$1`, id, status)
        return err
}

func UpdatePaymentReceived(ctx context.Context, id string, received float64) error {
        _, err := DB.ExecContext(ctx, `
                UPDATE crypto_payments SET received_amount=$2 WHERE id=$1
        `, id, received)
        return err
}

func ExpirePayment(ctx context.Context, id string) error {
        hours := GetSiteSettingInt("payment_cleanup_hours", 5)
        _, err := DB.ExecContext(ctx, `
                UPDATE crypto_payments SET status='expired',
                scheduled_delete=NOW() + ($2 || ' hours')::interval
                WHERE id=$1 AND status='pending'
        `, id, hours)
        return err
}

// AdminPaymentRow includes joined username for the admin view.
type AdminPaymentRow struct {
        CryptoPayment
        Username string
        ProductTitle string
}

func GetAdminPayments(ctx context.Context, status, search string) ([]AdminPaymentRow, error) {
        q := `
                SELECT cp.id, COALESCE(cp.user_id,0), cp.product_id, cp.coin, cp.wallet_address, cp.private_key_wif,
                       cp.amount_crypto, cp.amount_usd, cp.status, cp.received_amount,
                       cp.expires_at, cp.confirmed_at, cp.created_at, cp.scheduled_delete, cp.derivation_index,
                       COALESCE(u.username,'(deleted)') AS username,
                       COALESCE(mp.title,'') AS product_title
                FROM crypto_payments cp
                LEFT JOIN users u ON u.id = cp.user_id
                LEFT JOIN marketplace_products mp ON mp.id = cp.product_id
                WHERE 1=1`
        args := []interface{}{}
        idx := 1

        if status != "" && status != "all" {
                q += fmt.Sprintf(" AND cp.status=$%d", idx)
                args = append(args, status)
                idx++
        }
        if search != "" {
                q += fmt.Sprintf(" AND (cp.id ILIKE $%d OR u.username ILIKE $%d OR mp.title ILIKE $%d)", idx, idx, idx)
                args = append(args, "%"+search+"%")
                idx++
        }
        q += " ORDER BY cp.created_at DESC LIMIT 200"

        rows, err := DB.QueryContext(ctx, q, args...)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var result []AdminPaymentRow
        for rows.Next() {
                var r AdminPaymentRow
                err := rows.Scan(
                        &r.ID, &r.UserID, &r.ProductID, &r.Coin, &r.WalletAddress, &r.PrivateKeyWIF,
                        &r.AmountCrypto, &r.AmountUSD, &r.Status, &r.ReceivedAmount,
                        &r.ExpiresAt, &r.ConfirmedAt, &r.CreatedAt, &r.ScheduledDelete, &r.DerivationIndex,
                        &r.Username, &r.ProductTitle,
                )
                if err != nil {
                        continue
                }
                result = append(result, r)
        }
        return result, rows.Err()
}

// UserPaymentRow is used for the user-facing payment history in settings.
type UserPaymentRow struct {
        ID           string
        ProductID    int
        ProductTitle string
        Coin         string
        AmountCrypto float64
        AmountUSD    float64
        Status       string
        CreatedAt    time.Time
        ExpiresAt    time.Time
        ConfirmedAt  *time.Time
        SecsLeft     int
        IsPending    bool
}

// GetUserPaymentHistory returns all payments for a user: active (from crypto_payments) +
// historical cleaned-up ones (from payment_logs), deduped. Active pending payments appear first.
func GetUserPaymentHistory(ctx context.Context, userID int) ([]UserPaymentRow, error) {
        q := `
                SELECT id, product_id, product_title, coin, amount_crypto, amount_usd,
                       status, created_at, expires_at, confirmed_at
                FROM (
                        SELECT cp.id, cp.product_id, COALESCE(mp.title,'Unknown Product') AS product_title,
                               cp.coin, cp.amount_crypto, cp.amount_usd, cp.status,
                               cp.created_at, cp.expires_at, cp.confirmed_at
                        FROM crypto_payments cp
                        LEFT JOIN marketplace_products mp ON mp.id = cp.product_id
                        WHERE cp.user_id = $1

                        UNION ALL

                        SELECT pl.id, pl.product_id, pl.product_title,
                               pl.coin, pl.amount_crypto, pl.amount_usd, pl.status,
                               pl.created_at, pl.expires_at, pl.confirmed_at
                        FROM payment_logs pl
                        WHERE pl.user_id = $1
                          AND NOT EXISTS (SELECT 1 FROM crypto_payments WHERE id = pl.id)
                ) t
                ORDER BY
                  CASE WHEN status = 'pending' AND expires_at > NOW() THEN 0 ELSE 1 END,
                  created_at DESC NULLS LAST
                LIMIT 200
        `
        rows, err := DB.QueryContext(ctx, q, userID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        now := time.Now()
        var out []UserPaymentRow
        for rows.Next() {
                var r UserPaymentRow
                var createdAt sql.NullTime
                var expiresAt sql.NullTime
                var confirmedAt sql.NullTime
                if err := rows.Scan(&r.ID, &r.ProductID, &r.ProductTitle,
                        &r.Coin, &r.AmountCrypto, &r.AmountUSD, &r.Status,
                        &createdAt, &expiresAt, &confirmedAt); err != nil {
                        log.Printf("[payments] scan error for user %d: %v", userID, err)
                        continue
                }
                if createdAt.Valid {
                        r.CreatedAt = createdAt.Time
                }
                if expiresAt.Valid {
                        r.ExpiresAt = expiresAt.Time
                }
                if confirmedAt.Valid {
                        t := confirmedAt.Time
                        r.ConfirmedAt = &t
                }
                if r.Status == "pending" && r.ExpiresAt.After(now) {
                        r.IsPending = true
                        r.SecsLeft = int(r.ExpiresAt.Sub(now).Seconds())
                }
                out = append(out, r)
        }
        return out, rows.Err()
}

func DeletePayment(ctx context.Context, id string) error {
        _, err := DB.ExecContext(ctx, `DELETE FROM crypto_payments WHERE id=$1`, id)
        return err
}

func CancelPendingPayment(ctx context.Context, id string, userID int) error {
        _, err := DB.ExecContext(ctx, `
                UPDATE crypto_payments SET status='cancelled'
                WHERE id=$1 AND user_id=$2 AND status='pending'
        `, id, userID)
        return err
}

func scanPayment(row interface{ Scan(...interface{}) error }) (*CryptoPayment, error) {
        p := &CryptoPayment{}
        err := row.Scan(&p.ID, &p.UserID, &p.ProductID, &p.Coin, &p.WalletAddress, &p.PrivateKeyWIF,
                &p.AmountCrypto, &p.AmountUSD, &p.Status, &p.ReceivedAmount,
                &p.ExpiresAt, &p.ConfirmedAt, &p.CreatedAt, &p.ScheduledDelete, &p.DerivationIndex)
        if err == sql.ErrNoRows {
                return nil, nil
        }
        return p, err
}

func scanPaymentRow(rows interface{ Scan(...interface{}) error }) (CryptoPayment, error) {
        var p CryptoPayment
        err := rows.Scan(&p.ID, &p.UserID, &p.ProductID, &p.Coin, &p.WalletAddress, &p.PrivateKeyWIF,
                &p.AmountCrypto, &p.AmountUSD, &p.Status, &p.ReceivedAmount,
                &p.ExpiresAt, &p.ConfirmedAt, &p.CreatedAt, &p.ScheduledDelete, &p.DerivationIndex)
        return p, err
}
