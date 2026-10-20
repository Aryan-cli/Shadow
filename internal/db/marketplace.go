package db

import (
        "context"
        "database/sql"
        "os"
        "time"
)

type MarketplaceProduct struct {
        ID                 int
        Title              string
        Description        string
        Price              float64
        ThumbnailPath      string
        SellerName         string
        Category           string
        AfterBuyLink       string
        AfterBuyButtonName string
        AfterBuyComment    string
        ViewCount          int
        AvgRating          float64
        ReviewCount        int
        CreatedAt          time.Time
}

type MarketplaceImage struct {
        ID          int
        ProductID   int
        ImagePath   string
        IsThumbnail bool
        SortOrder   int
}

type MarketplaceRating struct {
        ID        int
        ProductID int
        UserID    int
        Username  string
        Rating    int
        Comment   string
        CreatedAt time.Time
}

type MarketplaceReview struct {
        ID              int
        ProductID       int
        UserID          int
        Username        string
        Comment         string
        IsVerifiedBuyer bool
        StarRating      int
        IsOwn           bool
        CreatedAt       time.Time
}

type Purchase struct {
        ID                 int
        UserID             int
        ProductID          int
        ProductTitle       string
        ProductPrice       float64
        ThumbnailPath      string
        AfterBuyLink       string
        AfterBuyButtonName string
        AfterBuyComment    string
        PurchasedAt        time.Time
}

func MigrateMarketplace() error {
        queries := []string{
                `CREATE TABLE IF NOT EXISTS marketplace_products (
                        id SERIAL PRIMARY KEY,
                        title VARCHAR(200) NOT NULL,
                        description TEXT DEFAULT '',
                        price NUMERIC(12,2) DEFAULT 0,
                        image_path TEXT DEFAULT '',
                        seller_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        category VARCHAR(100) DEFAULT '',
                        created_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE INDEX IF NOT EXISTS idx_marketplace_products_created ON marketplace_products(created_at DESC)`,
                `ALTER TABLE marketplace_products ADD COLUMN IF NOT EXISTS after_buy_link TEXT DEFAULT ''`,
                `ALTER TABLE marketplace_products ADD COLUMN IF NOT EXISTS after_buy_button_name VARCHAR(100) DEFAULT 'Download'`,
                `ALTER TABLE marketplace_products ADD COLUMN IF NOT EXISTS after_buy_comment TEXT DEFAULT ''`,
                `ALTER TABLE marketplace_products ADD COLUMN IF NOT EXISTS view_count INTEGER DEFAULT 0`,
                `CREATE TABLE IF NOT EXISTS marketplace_images (
                        id SERIAL PRIMARY KEY,
                        product_id INTEGER REFERENCES marketplace_products(id) ON DELETE CASCADE,
                        image_path TEXT NOT NULL,
                        is_thumbnail BOOLEAN DEFAULT false,
                        sort_order INTEGER DEFAULT 0
                )`,
                `CREATE INDEX IF NOT EXISTS idx_marketplace_images_product ON marketplace_images(product_id, sort_order)`,
                `CREATE TABLE IF NOT EXISTS marketplace_ratings (
                        id SERIAL PRIMARY KEY,
                        product_id INTEGER REFERENCES marketplace_products(id) ON DELETE CASCADE,
                        user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        rating INTEGER CHECK (rating >= 1 AND rating <= 5),
                        comment TEXT DEFAULT '',
                        created_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE INDEX IF NOT EXISTS idx_marketplace_ratings_product ON marketplace_ratings(product_id)`,
                `CREATE UNIQUE INDEX IF NOT EXISTS idx_marketplace_ratings_user_product ON marketplace_ratings(user_id, product_id)`,
                `CREATE TABLE IF NOT EXISTS marketplace_purchases (
                        id SERIAL PRIMARY KEY,
                        user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        product_id INTEGER,
                        product_title VARCHAR(200) NOT NULL DEFAULT '',
                        product_price NUMERIC(12,2) DEFAULT 0,
                        thumbnail_path TEXT DEFAULT '',
                        after_buy_link TEXT DEFAULT '',
                        after_buy_button_name VARCHAR(100) DEFAULT 'Download',
                        after_buy_comment TEXT DEFAULT '',
                        purchased_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE INDEX IF NOT EXISTS idx_marketplace_purchases_user ON marketplace_purchases(user_id, purchased_at DESC)`,
                `CREATE UNIQUE INDEX IF NOT EXISTS idx_marketplace_purchases_user_product ON marketplace_purchases(user_id, product_id) WHERE user_id IS NOT NULL`,
                `CREATE TABLE IF NOT EXISTS marketplace_reviews (
                        id SERIAL PRIMARY KEY,
                        product_id INTEGER REFERENCES marketplace_products(id) ON DELETE CASCADE,
                        user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        comment TEXT NOT NULL DEFAULT '',
                        is_verified_buyer BOOLEAN DEFAULT false,
                        created_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE INDEX IF NOT EXISTS idx_marketplace_reviews_product ON marketplace_reviews(product_id, created_at DESC)`,
        }
        for _, q := range queries {
                if _, err := DB.Exec(q); err != nil {
                        return err
                }
        }
        return nil
}

const productSelectCols = `
        SELECT mp.id, mp.title, mp.description, mp.price,
               COALESCE(
                   (SELECT image_path FROM marketplace_images WHERE product_id = mp.id AND is_thumbnail = true ORDER BY sort_order LIMIT 1),
                   (SELECT image_path FROM marketplace_images WHERE product_id = mp.id ORDER BY sort_order LIMIT 1),
                   ''
               ) AS thumbnail,
               COALESCE(u.display_name, u.username, 'Admin') AS seller_name,
               mp.category, mp.after_buy_link, mp.after_buy_button_name, mp.after_buy_comment,
               COALESCE(mp.view_count, 0),
               COALESCE((SELECT AVG(rating) FROM marketplace_ratings WHERE product_id = mp.id), 0),
               COALESCE((SELECT COUNT(*) FROM marketplace_ratings WHERE product_id = mp.id), 0),
               mp.created_at
        FROM marketplace_products mp
        LEFT JOIN users u ON u.id = mp.seller_id
`

func scanProduct(row interface{ Scan(...interface{}) error }) (MarketplaceProduct, error) {
        var p MarketplaceProduct
        err := row.Scan(&p.ID, &p.Title, &p.Description, &p.Price,
                &p.ThumbnailPath, &p.SellerName, &p.Category,
                &p.AfterBuyLink, &p.AfterBuyButtonName, &p.AfterBuyComment,
                &p.ViewCount, &p.AvgRating, &p.ReviewCount,
                &p.CreatedAt)
        return p, err
}

func GetMarketplaceProducts(ctx context.Context, search, category string, offset, limit int) ([]MarketplaceProduct, error) {
        rows, err := DB.QueryContext(ctx, productSelectCols+`
                WHERE ($1 = '' OR mp.title ILIKE '%' || $1 || '%'
                              OR mp.description ILIKE '%' || $1 || '%'
                              OR mp.category ILIKE '%' || $1 || '%')
                  AND ($2 = '' OR mp.category = $2)
                ORDER BY mp.created_at DESC
                LIMIT $3 OFFSET $4
        `, search, category, limit, offset)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var products []MarketplaceProduct
        for rows.Next() {
                p, err := scanProduct(rows)
                if err != nil {
                        continue
                }
                products = append(products, p)
        }
        return products, rows.Err()
}

func GetTopMarketplaceProducts(ctx context.Context, limit int) ([]MarketplaceProduct, error) {
        rows, err := DB.QueryContext(ctx, productSelectCols+`
                ORDER BY COALESCE(mp.view_count, 0) DESC, mp.created_at DESC
                LIMIT $1
        `, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var products []MarketplaceProduct
        for rows.Next() {
                p, err := scanProduct(rows)
                if err != nil {
                        continue
                }
                products = append(products, p)
        }
        return products, rows.Err()
}

func GetMarketplaceProduct(ctx context.Context, id int) (*MarketplaceProduct, error) {
        row := DB.QueryRowContext(ctx, productSelectCols+`WHERE mp.id = $1`, id)
        p, err := scanProduct(row)
        if err == sql.ErrNoRows {
                return nil, nil
        }
        if err != nil {
                return nil, err
        }
        return &p, nil
}

func IncrementProductView(ctx context.Context, id int) {
        _, _ = DB.ExecContext(ctx, `UPDATE marketplace_products SET view_count = COALESCE(view_count,0)+1 WHERE id=$1`, id)
}

func AdminListMarketplaceProducts(ctx context.Context) ([]MarketplaceProduct, error) {
        rows, err := DB.QueryContext(ctx, productSelectCols+`ORDER BY mp.created_at DESC`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var products []MarketplaceProduct
        for rows.Next() {
                p, err := scanProduct(rows)
                if err != nil {
                        continue
                }
                products = append(products, p)
        }
        return products, rows.Err()
}

func AddMarketplaceProduct(ctx context.Context, title, description, category string, price float64, sellerID int, afterBuyLink, afterBuyButtonName, afterBuyComment string) (int, error) {
        var id int
        var sid *int
        if sellerID > 0 {
                sid = &sellerID
        }
        err := DB.QueryRowContext(ctx, `
                INSERT INTO marketplace_products (title, description, price, seller_id, category, after_buy_link, after_buy_button_name, after_buy_comment)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                RETURNING id
        `, title, description, price, sid, category, afterBuyLink, afterBuyButtonName, afterBuyComment).Scan(&id)
        return id, err
}

func UpdateMarketplaceProduct(ctx context.Context, id int, title, description, category string, price float64, afterBuyLink, afterBuyButtonName, afterBuyComment string) error {
        _, err := DB.ExecContext(ctx, `
                UPDATE marketplace_products
                SET title=$2, description=$3, price=$4, category=$5,
                    after_buy_link=$6, after_buy_button_name=$7, after_buy_comment=$8
                WHERE id=$1
        `, id, title, description, price, category, afterBuyLink, afterBuyButtonName, afterBuyComment)
        return err
}

func DeleteMarketplaceProduct(ctx context.Context, id int) error {
        imgs, _ := GetProductImages(ctx, id)
        for _, img := range imgs {
                _ = os.Remove(img.ImagePath)
        }
        _, err := DB.ExecContext(ctx, `DELETE FROM marketplace_products WHERE id = $1`, id)
        return err
}

func AddProductImage(ctx context.Context, productID int, imagePath string, isThumbnail bool, sortOrder int) error {
        _, err := DB.ExecContext(ctx, `
                INSERT INTO marketplace_images (product_id, image_path, is_thumbnail, sort_order)
                VALUES ($1, $2, $3, $4)
        `, productID, imagePath, isThumbnail, sortOrder)
        return err
}

func GetProductImages(ctx context.Context, productID int) ([]MarketplaceImage, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT id, product_id, image_path, is_thumbnail, sort_order
                FROM marketplace_images
                WHERE product_id = $1
                ORDER BY sort_order, id
        `, productID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var imgs []MarketplaceImage
        for rows.Next() {
                var img MarketplaceImage
                if err := rows.Scan(&img.ID, &img.ProductID, &img.ImagePath, &img.IsThumbnail, &img.SortOrder); err != nil {
                        continue
                }
                imgs = append(imgs, img)
        }
        return imgs, rows.Err()
}

func DeleteProductImage(ctx context.Context, imageID, productID int) error {
        var imgPath string
        err := DB.QueryRowContext(ctx, `
                DELETE FROM marketplace_images WHERE id=$1 AND product_id=$2 RETURNING image_path
        `, imageID, productID).Scan(&imgPath)
        if err == nil {
                _ = os.Remove(imgPath)
        }
        if err == sql.ErrNoRows {
                return nil
        }
        return err
}

func SetThumbnail(ctx context.Context, imageID, productID int) error {
        _, err := DB.ExecContext(ctx, `
                UPDATE marketplace_images SET is_thumbnail = (id = $1) WHERE product_id = $2
        `, imageID, productID)
        return err
}

func AddOrUpdateRating(ctx context.Context, productID, userID, rating int, comment string) error {
        _, err := DB.ExecContext(ctx, `
                INSERT INTO marketplace_ratings (product_id, user_id, rating, comment)
                VALUES ($1, $2, $3, $4)
                ON CONFLICT (user_id, product_id) DO UPDATE
                SET rating = EXCLUDED.rating, comment = EXCLUDED.comment, created_at = NOW()
        `, productID, userID, rating, comment)
        return err
}

func DeleteRating(ctx context.Context, productID, userID int) error {
        _, err := DB.ExecContext(ctx, `DELETE FROM marketplace_ratings WHERE product_id=$1 AND user_id=$2`, productID, userID)
        return err
}

func AddReview(ctx context.Context, productID, userID int, comment string, isVerifiedBuyer bool) error {
        _, err := DB.ExecContext(ctx, `
                INSERT INTO marketplace_reviews (product_id, user_id, comment, is_verified_buyer)
                VALUES ($1, $2, $3, $4)
        `, productID, userID, comment, isVerifiedBuyer)
        return err
}

func GetProductReviews(ctx context.Context, productID, currentUserID int) ([]MarketplaceReview, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT mr.id, mr.product_id, COALESCE(mr.user_id,0), COALESCE(u.username,'Anonymous'),
                       mr.comment, mr.is_verified_buyer, COALESCE(mrat.rating, 0),
                       (mr.user_id IS NOT NULL AND mr.user_id = $2),
                       mr.created_at
                FROM marketplace_reviews mr
                LEFT JOIN users u ON u.id = mr.user_id
                LEFT JOIN marketplace_ratings mrat ON mrat.user_id = mr.user_id AND mrat.product_id = mr.product_id
                WHERE mr.product_id = $1
                ORDER BY mr.is_verified_buyer DESC, mr.created_at DESC
                LIMIT 100
        `, productID, currentUserID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var reviews []MarketplaceReview
        for rows.Next() {
                var r MarketplaceReview
                if err := rows.Scan(&r.ID, &r.ProductID, &r.UserID, &r.Username,
                        &r.Comment, &r.IsVerifiedBuyer, &r.StarRating, &r.IsOwn, &r.CreatedAt); err != nil {
                        continue
                }
                reviews = append(reviews, r)
        }
        return reviews, rows.Err()
}

func DeleteReview(ctx context.Context, reviewID, userID int) error {
        _, err := DB.ExecContext(ctx, `DELETE FROM marketplace_reviews WHERE id=$1 AND user_id=$2`, reviewID, userID)
        return err
}

func GetProductRatings(ctx context.Context, productID int) ([]MarketplaceRating, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT mr.id, mr.product_id, COALESCE(mr.user_id,0), COALESCE(u.username,'Anonymous'),
                       mr.rating, mr.comment, mr.created_at
                FROM marketplace_ratings mr
                LEFT JOIN users u ON u.id = mr.user_id
                WHERE mr.product_id = $1
                ORDER BY mr.created_at DESC
                LIMIT 50
        `, productID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var ratings []MarketplaceRating
        for rows.Next() {
                var r MarketplaceRating
                if err := rows.Scan(&r.ID, &r.ProductID, &r.UserID, &r.Username, &r.Rating, &r.Comment, &r.CreatedAt); err != nil {
                        continue
                }
                ratings = append(ratings, r)
        }
        return ratings, rows.Err()
}

func GetUserRatingForProduct(ctx context.Context, productID, userID int) (int, error) {
        var rating int
        err := DB.QueryRowContext(ctx, `SELECT rating FROM marketplace_ratings WHERE product_id=$1 AND user_id=$2`, productID, userID).Scan(&rating)
        if err == sql.ErrNoRows {
                return 0, nil
        }
        return rating, err
}

func RecordPurchase(ctx context.Context, userID, productID int, title string, price float64, thumbnail, afterBuyLink, afterBuyButtonName, afterBuyComment string) error {
        _, err := DB.ExecContext(ctx, `
                INSERT INTO marketplace_purchases
                        (user_id, product_id, product_title, product_price, thumbnail_path, after_buy_link, after_buy_button_name, after_buy_comment)
                SELECT $1, $2, $3, $4, $5, $6, $7, $8
                WHERE NOT EXISTS (
                        SELECT 1 FROM marketplace_purchases WHERE user_id = $1 AND product_id = $2
                )
        `, userID, productID, title, price, thumbnail, afterBuyLink, afterBuyButtonName, afterBuyComment)
        return err
}

func HasUserPurchased(ctx context.Context, userID, productID int) (bool, error) {
        var exists bool
        err := DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM marketplace_purchases WHERE user_id=$1 AND product_id=$2)`, userID, productID).Scan(&exists)
        return exists, err
}

func GetUserPurchases(ctx context.Context, userID int, search string) ([]Purchase, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT id, COALESCE(user_id,0), COALESCE(product_id,0),
                       product_title, CAST(product_price AS FLOAT8),
                       thumbnail_path, after_buy_link, after_buy_button_name, after_buy_comment, purchased_at
                FROM marketplace_purchases
                WHERE user_id = $1
                  AND ($2 = '' OR product_title ILIKE '%' || $2 || '%')
                ORDER BY purchased_at DESC
        `, userID, search)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var purchases []Purchase
        for rows.Next() {
                var p Purchase
                if err := rows.Scan(&p.ID, &p.UserID, &p.ProductID, &p.ProductTitle, &p.ProductPrice,
                        &p.ThumbnailPath, &p.AfterBuyLink, &p.AfterBuyButtonName, &p.AfterBuyComment, &p.PurchasedAt); err != nil {
                        continue
                }
                purchases = append(purchases, p)
        }
        return purchases, rows.Err()
}
