package db

import (
        "context"
        "crypto/rand"
        "database/sql"
        "encoding/hex"
        "fmt"
        "strings"
        "time"
)

func dbAssetURL(path string) string {
        if path == "" {
                return ""
        }
        if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "http") {
                return path
        }
        return "/" + path
}

type ForumPost struct {
        ID              int
        AuthorID        int
        AuthorName      string
        AuthorAvatarURL string
        ServerID        sql.NullInt64
        ServerName      string
        ServerSlug      string
        CategoryID      sql.NullInt64
        CategoryName    string
        CategorySlug    string
        Title           string
        Body            string
        ImageURL        string
        Score           int
        CommentCount    int
        UserVote        int
        ViewCount       int
        CreatedAt       time.Time
        Hashtags        []string
}

type ForumCategory struct {
        ID        int
        Name      string
        Slug      string
        PostCount int
}

type ForumServer struct {
        ID           int
        PublicID     int64  // 10-digit random ID used in URLs
        Name         string
        Slug         string
        Handle       string // unique @handle e.g. "shadow-dev"
        Description  string
        CreatorID    int
        Initials     string
        IsFollowing  bool
        IsMember     bool
        MemberCount  int
        OnlineCount  int
        UserRole     string // "owner" | "partner" | "member" | ""
        UnreadCount  int
        LogoURL      string
        BannerURL    string
        FreeForAll   bool
        InviteToken  string
}

type ServerMember struct {
        UserID    int
        Username  string
        Role      string
        JoinedAt  time.Time
        IsOnline  bool
        ViaInvite bool
}

type ForumStats struct {
        TotalPosts      int
        TotalMembers    int
        OnlineNow       int
        TotalCategories int
}

type ForumComment struct {
        ID              int
        PostID          int
        AuthorID        int
        AuthorName      string
        AuthorAvatarURL string
        Body            string
        ParentID        sql.NullInt64
        CreatedAt       time.Time
        Replies         []ForumComment
        Score           int
        UserVote        int
}

type OnlineUser struct {
        Username   string
        Initials   string
        MinutesAgo int
}

type ForumSearchResult struct {
        Type        string
        ID          int
        Name        string
        Slug        string
        Snippet     string
        MemberCount int
        URL         string
}

func MigrateForums() error {
        queries := []string{
                `CREATE TABLE IF NOT EXISTS forum_categories (
                        id SERIAL PRIMARY KEY,
                        name VARCHAR(80) NOT NULL,
                        slug VARCHAR(80) UNIQUE NOT NULL,
                        created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        created_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `INSERT INTO forum_categories (name, slug) VALUES
                        ('Privacy',     'privacy'),
                        ('Marketplace', 'marketplace'),
                        ('Dev & Tools', 'dev-tools'),
                        ('Security',    'security'),
                        ('Crypto',      'crypto'),
                        ('Off-topic',   'off-topic')
                ON CONFLICT (slug) DO NOTHING`,
                `CREATE TABLE IF NOT EXISTS forum_hashtags (
                        id SERIAL PRIMARY KEY,
                        name VARCHAR(80) UNIQUE NOT NULL
                )`,
                `CREATE TABLE IF NOT EXISTS forum_servers (
                        id SERIAL PRIMARY KEY,
                        name VARCHAR(100) NOT NULL,
                        slug VARCHAR(100) NOT NULL,
                        description TEXT DEFAULT '',
                        creator_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        created_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE TABLE IF NOT EXISTS forum_server_follows (
                        server_id INTEGER REFERENCES forum_servers(id) ON DELETE CASCADE,
                        user_id   INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        followed_at TIMESTAMPTZ DEFAULT NOW(),
                        PRIMARY KEY (server_id, user_id)
                )`,
                `CREATE TABLE IF NOT EXISTS forum_server_members (
                        server_id INTEGER REFERENCES forum_servers(id) ON DELETE CASCADE,
                        user_id   INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        role      VARCHAR(20) NOT NULL DEFAULT 'member',
                        joined_at TIMESTAMPTZ DEFAULT NOW(),
                        PRIMARY KEY (server_id, user_id)
                )`,
                `CREATE TABLE IF NOT EXISTS forum_server_bans (
                        server_id  INTEGER REFERENCES forum_servers(id) ON DELETE CASCADE,
                        user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        banned_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
                        reason     TEXT DEFAULT '',
                        banned_at  TIMESTAMPTZ DEFAULT NOW(),
                        PRIMARY KEY (server_id, user_id)
                )`,
                `CREATE TABLE IF NOT EXISTS forum_user_follows (
                        follower_id  INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        followed_id  INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        followed_at  TIMESTAMPTZ DEFAULT NOW(),
                        PRIMARY KEY (follower_id, followed_id)
                )`,
                `CREATE TABLE IF NOT EXISTS forum_posts (
                        id          SERIAL PRIMARY KEY,
                        author_id   INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        server_id   INTEGER REFERENCES forum_servers(id) ON DELETE SET NULL,
                        category_id INTEGER REFERENCES forum_categories(id) ON DELETE SET NULL,
                        title       TEXT NOT NULL,
                        body        TEXT DEFAULT '',
                        image_url   TEXT DEFAULT '',
                        created_at  TIMESTAMPTZ DEFAULT NOW()
                )`,
                `ALTER TABLE forum_posts ADD COLUMN IF NOT EXISTS image_url TEXT DEFAULT ''`,
                `ALTER TABLE forum_posts ADD COLUMN IF NOT EXISTS view_count INTEGER DEFAULT 0`,
                `CREATE INDEX IF NOT EXISTS idx_fposts_author ON forum_posts(author_id)`,
                `CREATE INDEX IF NOT EXISTS idx_fposts_created ON forum_posts(created_at DESC)`,
                `CREATE INDEX IF NOT EXISTS idx_fposts_category ON forum_posts(category_id)`,
                `CREATE TABLE IF NOT EXISTS forum_post_hashtags (
                        post_id    INTEGER REFERENCES forum_posts(id) ON DELETE CASCADE,
                        hashtag_id INTEGER REFERENCES forum_hashtags(id) ON DELETE CASCADE,
                        PRIMARY KEY (post_id, hashtag_id)
                )`,
                `CREATE TABLE IF NOT EXISTS forum_votes (
                        post_id    INTEGER REFERENCES forum_posts(id) ON DELETE CASCADE,
                        user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        value      SMALLINT NOT NULL CHECK (value IN (-1, 1)),
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        PRIMARY KEY (post_id, user_id)
                )`,
                `CREATE TABLE IF NOT EXISTS forum_comments (
                        id        SERIAL PRIMARY KEY,
                        post_id   INTEGER REFERENCES forum_posts(id) ON DELETE CASCADE,
                        author_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        body      TEXT NOT NULL,
                        parent_id INTEGER REFERENCES forum_comments(id) ON DELETE CASCADE,
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        is_deleted BOOLEAN DEFAULT FALSE
                )`,
                `CREATE INDEX IF NOT EXISTS idx_fcomments_post ON forum_comments(post_id)`,
                `CREATE TABLE IF NOT EXISTS forum_comment_votes (
                        comment_id INTEGER REFERENCES forum_comments(id) ON DELETE CASCADE,
                        user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        value      SMALLINT NOT NULL CHECK (value IN (-1, 1)),
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        PRIMARY KEY (comment_id, user_id)
                )`,
                `ALTER TABLE forum_servers ADD COLUMN IF NOT EXISTS logo_url TEXT DEFAULT ''`,
                `ALTER TABLE forum_servers ADD COLUMN IF NOT EXISTS banner_url TEXT DEFAULT ''`,
                `ALTER TABLE forum_servers ADD COLUMN IF NOT EXISTS free_for_all BOOLEAN DEFAULT TRUE`,
                `ALTER TABLE forum_servers ADD COLUMN IF NOT EXISTS invite_token TEXT DEFAULT ''`,
                `ALTER TABLE forum_servers ADD COLUMN IF NOT EXISTS handle VARCHAR(50) DEFAULT NULL`,
                `CREATE UNIQUE INDEX IF NOT EXISTS forum_servers_handle_idx ON forum_servers(handle) WHERE handle IS NOT NULL`,
                `ALTER TABLE forum_server_members ADD COLUMN IF NOT EXISTS via_invite BOOLEAN DEFAULT FALSE`,
                // Migrate existing server owners into members table
                `INSERT INTO forum_server_members (server_id, user_id, role)
                 SELECT id, creator_id, 'owner' FROM forum_servers WHERE creator_id IS NOT NULL
                 ON CONFLICT (server_id, user_id) DO UPDATE SET role = 'owner'`,
                // Migrate existing followers into members table
                `INSERT INTO forum_server_members (server_id, user_id, role)
                 SELECT server_id, user_id, 'member' FROM forum_server_follows
                 ON CONFLICT (server_id, user_id) DO NOTHING`,
                // Populate handles from slugs for existing servers
                `UPDATE forum_servers SET handle = slug WHERE handle IS NULL`,
                `ALTER TABLE forum_servers ADD COLUMN IF NOT EXISTS public_id BIGINT`,
                `CREATE UNIQUE INDEX IF NOT EXISTS forum_servers_public_id_idx ON forum_servers(public_id) WHERE public_id IS NOT NULL`,
        }
        // Populate public_id for existing servers that don't have one yet
        rows, _ := DB.QueryContext(context.Background(), `SELECT id FROM forum_servers WHERE public_id IS NULL`)
        if rows != nil {
                defer rows.Close()
                var ids []int
                for rows.Next() {
                        var id int
                        if rows.Scan(&id) == nil {
                                ids = append(ids, id)
                        }
                }
                rows.Close()
                for _, id := range ids {
                        pid := generatePublicID(context.Background())
                        DB.ExecContext(context.Background(), `UPDATE forum_servers SET public_id=$2 WHERE id=$1`, id, pid)
                }
        }
        for _, q := range queries {
                if _, err := DB.Exec(q); err != nil {
                        return fmt.Errorf("forum migration: %w", err)
                }
        }
        return nil
}

func GetForumPosts(ctx context.Context, categorySlug string, serverID int, hashtagName string, search string, viewerID int, limit, offset int, sortBy string, dateFilter string) ([]ForumPost, error) {
        args := []interface{}{}
        idx := 1

        baseQ := `
                SELECT
                        p.id, p.author_id, COALESCE(u.display_name, u.username), COALESCE(u.avatar_path,''),
                        p.server_id,
                        COALESCE(s.name,''), COALESCE(s.slug,''), p.category_id, COALESCE(c.name,''), COALESCE(c.slug,''),
                        p.title, p.body, COALESCE(p.image_url,''), p.created_at,
                        (SELECT COALESCE(SUM(value),0) FROM forum_votes WHERE post_id = p.id) AS score,
                        (SELECT COUNT(*) FROM forum_comments WHERE post_id = p.id AND is_deleted = FALSE) AS comment_count,
                        COALESCE((SELECT value FROM forum_votes WHERE post_id = p.id AND user_id = $1), 0) AS user_vote,
                        COALESCE(p.view_count, 0) AS view_count
                FROM forum_posts p
                JOIN users u ON u.id = p.author_id
                LEFT JOIN forum_servers s ON s.id = p.server_id
                LEFT JOIN forum_categories c ON c.id = p.category_id
        `
        args = append(args, viewerID)
        idx++

        joins := ""
        wheres := []string{"u.deleted_at IS NULL"}

        if categorySlug != "" && categorySlug != "all" {
                joins += fmt.Sprintf(" JOIN forum_categories fc2 ON fc2.id = p.category_id AND fc2.slug = $%d", idx)
                args = append(args, categorySlug)
                idx++
        }
        if serverID > 0 {
                wheres = append(wheres, fmt.Sprintf("p.server_id = $%d", idx))
                args = append(args, serverID)
                idx++
        }
        if hashtagName != "" {
                joins += fmt.Sprintf(` JOIN forum_post_hashtags ph ON ph.post_id = p.id
                        JOIN forum_hashtags ht ON ht.id = ph.hashtag_id AND ht.name = $%d`, idx)
                args = append(args, hashtagName)
                idx++
        }
        if search != "" {
                wheres = append(wheres, fmt.Sprintf("(p.title ILIKE $%d OR p.body ILIKE $%d)", idx, idx+1))
                args = append(args, "%"+search+"%", "%"+search+"%")
                idx += 2
        }
        switch dateFilter {
        case "today":
                wheres = append(wheres, "p.created_at > NOW() - INTERVAL '1 day'")
        case "week":
                wheres = append(wheres, "p.created_at > NOW() - INTERVAL '7 days'")
        case "month":
                wheres = append(wheres, "p.created_at > NOW() - INTERVAL '30 days'")
        case "year":
                wheres = append(wheres, "p.created_at > NOW() - INTERVAL '1 year'")
        }

        q := baseQ + joins
        if len(wheres) > 0 {
                q += " WHERE " + strings.Join(wheres, " AND ")
        }
        orderBy := "p.created_at DESC"
        switch sortBy {
        case "top":
                orderBy = "score DESC, p.created_at DESC"
        case "hot":
                orderBy = "comment_count DESC, p.created_at DESC"
        default:
                // ── Personalized feed (default, no explicit sort chosen) ──────────────
                // Formula combines five signals, all derived on-the-fly from existing tables.
                //
                //  A. Time-decayed hot score   — Reddit-style: votes / (age_hours+2)^1.5
                //     Keeps fresh quality posts visible; old high-vote posts don't dominate.
                //
                //  B. Category affinity        — user's net votes in this post's category / 10,
                //     clamped 0–1, used as a multiplier on A (so 1× – 2× boost).
                //
                //  C. Hashtag/tag bonus        — additive: +0.15 per upvote, -0.08 per downvote
                //     in any tag shared with this post.
                //
                //  D. Exploration boost        — +1.5 for posts < 12h old with ≥1 net upvote
                //     in channels where the user has < 3 total interactions (unfamiliar channels).
                //
                //  E. Per-user downvote penalty— -8.0 if THIS user already downvoted this post.
                //
                //  F. Random nudge             — random()*0.3 for variation on every reload.
                //
                // For anonymous users: simple hot score only (A).
                // $1 is viewerID (already in args as the first parameter).
                if viewerID > 0 {
                        orderBy = `(
                                GREATEST(
                                        (SELECT COALESCE(SUM(value),0) FROM forum_votes WHERE post_id = p.id)::float,
                                        0
                                ) / POWER(
                                        GREATEST(EXTRACT(EPOCH FROM (NOW() - p.created_at)) / 3600.0, 0) + 2,
                                        1.5
                                )
                                * (1.0 + LEAST(GREATEST(
                                        COALESCE((
                                                SELECT SUM(fv_cat.value)::float
                                                FROM forum_votes fv_cat
                                                JOIN forum_posts fp_cat ON fp_cat.id = fv_cat.post_id
                                                WHERE fv_cat.user_id = $1 AND fp_cat.category_id = p.category_id
                                        ), 0.0) / 10.0,
                                0.0), 1.0))
                                + COALESCE((
                                        SELECT SUM(CASE WHEN fv_tag.value = 1 THEN 0.15 WHEN fv_tag.value = -1 THEN -0.08 ELSE 0 END)
                                        FROM forum_votes fv_tag
                                        JOIN forum_posts fp_tag ON fp_tag.id = fv_tag.post_id
                                        JOIN forum_post_hashtags ph_tag ON ph_tag.post_id = fp_tag.id
                                        WHERE fv_tag.user_id = $1
                                          AND ph_tag.hashtag_id IN (SELECT hashtag_id FROM forum_post_hashtags WHERE post_id = p.id)
                                ), 0)
                                + CASE
                                        WHEN p.created_at > NOW() - INTERVAL '12 hours'
                                         AND (SELECT COALESCE(SUM(value),0) FROM forum_votes WHERE post_id = p.id) >= 1
                                         AND COALESCE((
                                                SELECT ABS(SUM(fv_ex.value))
                                                FROM forum_votes fv_ex
                                                JOIN forum_posts fp_ex ON fp_ex.id = fv_ex.post_id
                                                WHERE fv_ex.user_id = $1 AND fp_ex.category_id = p.category_id
                                         ), 0) < 3
                                        THEN 1.5
                                        ELSE 0
                                  END
                                + COALESCE((
                                        SELECT CASE WHEN value = -1 THEN -8.0 ELSE 0.0 END
                                        FROM forum_votes WHERE post_id = p.id AND user_id = $1
                                ), 0)
                                + random() * 0.3
                        ) DESC, p.created_at DESC`
                } else {
                        // Anonymous: simple hot score — time-decayed net votes
                        orderBy = `GREATEST(
                                (SELECT COALESCE(SUM(value),0) FROM forum_votes WHERE post_id = p.id)::float,
                                0
                        ) / POWER(
                                GREATEST(EXTRACT(EPOCH FROM (NOW() - p.created_at)) / 3600.0, 0) + 2,
                                1.5
                        ) DESC, p.created_at DESC`
                }
        }
        q += fmt.Sprintf(`
                GROUP BY p.id, u.display_name, u.username, u.avatar_path, s.name, s.slug, c.name, c.slug
                ORDER BY %s
                LIMIT $%d OFFSET $%d`, orderBy, idx, idx+1)
        args = append(args, limit, offset)

        rows, err := DB.QueryContext(ctx, q, args...)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var posts []ForumPost
        for rows.Next() {
                var p ForumPost
                if err := rows.Scan(&p.ID, &p.AuthorID, &p.AuthorName, &p.AuthorAvatarURL, &p.ServerID,
                        &p.ServerName, &p.ServerSlug, &p.CategoryID, &p.CategoryName, &p.CategorySlug,
                        &p.Title, &p.Body, &p.ImageURL, &p.CreatedAt,
                        &p.Score, &p.CommentCount, &p.UserVote, &p.ViewCount); err != nil {
                        return nil, err
                }
                p.AuthorAvatarURL = dbAssetURL(p.AuthorAvatarURL)
                posts = append(posts, p)
        }

        for i := range posts {
                tags, _ := getPostHashtags(ctx, posts[i].ID)
                posts[i].Hashtags = tags
        }

        return posts, nil
}

func getPostHashtags(ctx context.Context, postID int) ([]string, error) {
        rows, err := DB.QueryContext(ctx,
                `SELECT h.name FROM forum_hashtags h
                 JOIN forum_post_hashtags ph ON ph.hashtag_id = h.id
                 WHERE ph.post_id = $1`, postID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var tags []string
        for rows.Next() {
                var t string
                if err := rows.Scan(&t); err != nil {
                        return nil, err
                }
                tags = append(tags, t)
        }
        return tags, nil
}

func GetPostByID(ctx context.Context, postID, viewerID int) (*ForumPost, error) {
        var p ForumPost
        err := DB.QueryRowContext(ctx, `
                SELECT
                        p.id, p.author_id, COALESCE(u.display_name, u.username), COALESCE(u.avatar_path,''),
                        p.server_id,
                        COALESCE(s.name,''), COALESCE(s.slug,''), p.category_id, COALESCE(c.name,''), COALESCE(c.slug,''),
                        p.title, p.body, COALESCE(p.image_url,''), p.created_at,
                        (SELECT COALESCE(SUM(value),0) FROM forum_votes WHERE post_id = p.id),
                        (SELECT COUNT(*) FROM forum_comments WHERE post_id = p.id AND is_deleted = FALSE),
                        COALESCE((SELECT value FROM forum_votes WHERE post_id = p.id AND user_id = $2), 0),
                        COALESCE(p.view_count, 0)
                FROM forum_posts p
                JOIN users u ON u.id = p.author_id
                LEFT JOIN forum_servers s ON s.id = p.server_id
                LEFT JOIN forum_categories c ON c.id = p.category_id
                WHERE p.id = $1
                GROUP BY p.id, u.display_name, u.username, u.avatar_path, s.name, s.slug, c.name, c.slug`,
                postID, viewerID,
        ).Scan(&p.ID, &p.AuthorID, &p.AuthorName, &p.AuthorAvatarURL, &p.ServerID,
                &p.ServerName, &p.ServerSlug, &p.CategoryID, &p.CategoryName, &p.CategorySlug,
                &p.Title, &p.Body, &p.ImageURL, &p.CreatedAt,
                &p.Score, &p.CommentCount, &p.UserVote, &p.ViewCount)
        if err != nil {
                return nil, err
        }
        p.AuthorAvatarURL = dbAssetURL(p.AuthorAvatarURL)
        tags, _ := getPostHashtags(ctx, p.ID)
        p.Hashtags = tags
        return &p, nil
}

func CreateForumPost(ctx context.Context, authorID int, serverID sql.NullInt64, categoryID sql.NullInt64, title, body, imageURL string) (int, error) {
        var id int
        err := DB.QueryRowContext(ctx,
                `INSERT INTO forum_posts (author_id, server_id, category_id, title, body, image_url)
                 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
                authorID, serverID, categoryID, title, body, imageURL,
        ).Scan(&id)
        return id, err
}

func AttachHashtagsToPost(ctx context.Context, postID int, tags []string) error {
        for _, tag := range tags {
                tag = strings.TrimSpace(strings.ToLower(tag))
                if tag == "" {
                        continue
                }
                var hid int
                err := DB.QueryRowContext(ctx,
                        `INSERT INTO forum_hashtags (name) VALUES ($1)
                         ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id`, tag).Scan(&hid)
                if err != nil {
                        return err
                }
                _, err = DB.ExecContext(ctx,
                        `INSERT INTO forum_post_hashtags (post_id, hashtag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
                        postID, hid)
                if err != nil {
                        return err
                }
        }
        return nil
}

func VoteOnPost(ctx context.Context, postID, userID, value int) (int, error) {
        if value == 0 {
                _, err := DB.ExecContext(ctx,
                        `DELETE FROM forum_votes WHERE post_id = $1 AND user_id = $2`, postID, userID)
                if err != nil {
                        return 0, err
                }
        } else {
                _, err := DB.ExecContext(ctx,
                        `INSERT INTO forum_votes (post_id, user_id, value) VALUES ($1, $2, $3)
                         ON CONFLICT (post_id, user_id) DO UPDATE SET value = EXCLUDED.value`,
                        postID, userID, value)
                if err != nil {
                        return 0, err
                }
        }
        var score int
        err := DB.QueryRowContext(ctx,
                `SELECT COALESCE(SUM(value),0) FROM forum_votes WHERE post_id = $1`, postID).Scan(&score)
        return score, err
}

func GetForumStats(ctx context.Context) (ForumStats, error) {
        var s ForumStats
        _ = DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM forum_posts`).Scan(&s.TotalPosts)
        _ = DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&s.TotalMembers)
        _ = DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM forum_categories`).Scan(&s.TotalCategories)
        threshold := 120
        _ = DB.QueryRowContext(ctx,
                `SELECT COUNT(*) FROM users WHERE last_seen_at > NOW() - ($1 || ' seconds')::interval AND deleted_at IS NULL`, threshold).Scan(&s.OnlineNow)
        return s, nil
}

func GetTopCategories(ctx context.Context, limit int) ([]ForumCategory, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT c.id, c.name, c.slug, COUNT(p.id) AS post_count
                FROM forum_categories c
                LEFT JOIN forum_posts p ON p.category_id = c.id
                GROUP BY c.id ORDER BY post_count DESC LIMIT $1`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var cats []ForumCategory
        for rows.Next() {
                var c ForumCategory
                if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.PostCount); err != nil {
                        return nil, err
                }
                cats = append(cats, c)
        }
        return cats, nil
}

func GetAllCategories(ctx context.Context) ([]ForumCategory, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT c.id, c.name, c.slug, COUNT(p.id) AS post_count
                FROM forum_categories c
                LEFT JOIN forum_posts p ON p.category_id = c.id
                GROUP BY c.id ORDER BY c.name ASC`)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var cats []ForumCategory
        for rows.Next() {
                var c ForumCategory
                if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.PostCount); err != nil {
                        return nil, err
                }
                cats = append(cats, c)
        }
        return cats, nil
}

func GetCategoryBySlug(ctx context.Context, slug string) (*ForumCategory, error) {
        var c ForumCategory
        err := DB.QueryRowContext(ctx,
                `SELECT id, name, slug FROM forum_categories WHERE slug = $1`, slug).
                Scan(&c.ID, &c.Name, &c.Slug)
        if err != nil {
                return nil, err
        }
        return &c, nil
}

func CreateCategory(ctx context.Context, name string, createdBy int) (*ForumCategory, error) {
        slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
        slug = strings.Map(func(r rune) rune {
                if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
                        return r
                }
                return -1
        }, slug)
        if slug == "" {
                return nil, fmt.Errorf("invalid category name")
        }
        var c ForumCategory
        err := DB.QueryRowContext(ctx,
                `INSERT INTO forum_categories (name, slug, created_by) VALUES ($1, $2, $3)
                 ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name RETURNING id, name, slug`,
                name, slug, createdBy).Scan(&c.ID, &c.Name, &c.Slug)
        if err != nil {
                return nil, err
        }
        return &c, nil
}

func serverInitials(name string) string {
        if len(name) >= 2 {
                return strings.ToUpper(name[:2])
        } else if len(name) == 1 {
                return strings.ToUpper(name[:1])
        }
        return "?"
}

func GetAllForumServers(ctx context.Context, userID int) ([]ForumServer, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT s.id, COALESCE(s.public_id,0), s.name, s.slug, COALESCE(s.description,''),
                        COALESCE(s.creator_id, 0),
                        CASE WHEN f.user_id IS NOT NULL THEN TRUE ELSE FALSE END AS is_following,
                        CASE WHEN m.user_id IS NOT NULL THEN TRUE ELSE FALSE END AS is_member,
                        COALESCE(m.role, '') AS user_role,
                        COUNT(DISTINCT mc.user_id) AS member_count,
                        COALESCE(s.logo_url,'')
                FROM forum_servers s
                LEFT JOIN forum_server_follows f ON f.server_id = s.id AND f.user_id = $1
                LEFT JOIN forum_server_members m ON m.server_id = s.id AND m.user_id = $1
                LEFT JOIN forum_server_members mc ON mc.server_id = s.id
                GROUP BY s.id, f.user_id, m.user_id, m.role
                ORDER BY s.name ASC`, userID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var servers []ForumServer
        for rows.Next() {
                var sv ForumServer
                if err := rows.Scan(&sv.ID, &sv.PublicID, &sv.Name, &sv.Slug, &sv.Description,
                        &sv.CreatorID, &sv.IsFollowing, &sv.IsMember, &sv.UserRole, &sv.MemberCount, &sv.LogoURL); err != nil {
                        return nil, err
                }
                sv.Initials = serverInitials(sv.Name)
                servers = append(servers, sv)
        }
        return servers, nil
}

func GetFollowedServers(ctx context.Context, userID int) ([]ForumServer, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT s.id, COALESCE(s.public_id,0), s.name, s.slug, COALESCE(s.description,''), COALESCE(s.creator_id,0),
                        COALESCE(m.role,'member') AS user_role, COALESCE(s.logo_url,'')
                FROM forum_servers s
                JOIN forum_server_follows f ON f.server_id = s.id AND f.user_id = $1
                LEFT JOIN forum_server_members m ON m.server_id = s.id AND m.user_id = $1
                ORDER BY s.name ASC`, userID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var servers []ForumServer
        for rows.Next() {
                var sv ForumServer
                if err := rows.Scan(&sv.ID, &sv.PublicID, &sv.Name, &sv.Slug, &sv.Description, &sv.CreatorID, &sv.UserRole, &sv.LogoURL); err != nil {
                        return nil, err
                }
                sv.Initials = serverInitials(sv.Name)
                sv.IsFollowing = true
                sv.IsMember = true
                servers = append(servers, sv)
        }
        return servers, nil
}

func generateInviteToken() string {
        b := make([]byte, 8)
        _, _ = rand.Read(b)
        return hex.EncodeToString(b)
}

func generatePublicID(ctx context.Context) int64 {
        const min int64 = 1000000000
        const max int64 = 9999999999
        for {
                b := make([]byte, 5)
                _, _ = rand.Read(b)
                n := int64(b[0])<<32 | int64(b[1])<<24 | int64(b[2])<<16 | int64(b[3])<<8 | int64(b[4])
                pid := min + (n % (max - min + 1))
                if pid < 0 { pid = -pid }
                pid = min + (pid % (max - min + 1))
                var exists bool
                _ = DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM forum_servers WHERE public_id=$1)`, pid).Scan(&exists)
                if !exists {
                        return pid
                }
        }
}

func GetServerByID(ctx context.Context, serverID, viewerID int) (*ForumServer, error) {
        var sv ForumServer
        err := DB.QueryRowContext(ctx, `
                SELECT s.id, COALESCE(s.public_id,0), s.name, s.slug, COALESCE(s.description,''),
                        COALESCE(s.creator_id,0),
                        CASE WHEN f.user_id IS NOT NULL THEN TRUE ELSE FALSE END AS is_following,
                        CASE WHEN m.user_id IS NOT NULL THEN TRUE ELSE FALSE END AS is_member,
                        COALESCE(m.role,'') AS user_role,
                        COUNT(DISTINCT mc.user_id) AS member_count,
                        COUNT(DISTINCT CASE WHEN u.last_seen_at > NOW() - INTERVAL '2 minutes' THEN mc.user_id END) AS online_count,
                        COALESCE(s.logo_url,''), COALESCE(s.banner_url,''),
                        COALESCE(s.free_for_all, TRUE), COALESCE(s.invite_token,''), COALESCE(s.handle,'')
                FROM forum_servers s
                LEFT JOIN forum_server_follows f ON f.server_id = s.id AND f.user_id = $2
                LEFT JOIN forum_server_members m ON m.server_id = s.id AND m.user_id = $2
                LEFT JOIN forum_server_members mc ON mc.server_id = s.id
                LEFT JOIN users u ON u.id = mc.user_id AND u.deleted_at IS NULL
                WHERE s.id = $1
                GROUP BY s.id, f.user_id, m.user_id, m.role`, serverID, viewerID,
        ).Scan(&sv.ID, &sv.PublicID, &sv.Name, &sv.Slug, &sv.Description, &sv.CreatorID,
                &sv.IsFollowing, &sv.IsMember, &sv.UserRole, &sv.MemberCount, &sv.OnlineCount,
                &sv.LogoURL, &sv.BannerURL, &sv.FreeForAll, &sv.InviteToken, &sv.Handle)
        if err != nil {
                return nil, err
        }
        sv.Initials = serverInitials(sv.Name)
        // Lazily assign public_id if missing (servers created before migration)
        if sv.PublicID == 0 {
                sv.PublicID = generatePublicID(ctx)
                _, _ = DB.ExecContext(ctx, `UPDATE forum_servers SET public_id=$2 WHERE id=$1`, sv.ID, sv.PublicID)
        }
        return &sv, nil
}

func GetServerByInviteToken(ctx context.Context, token string, viewerID int) (*ForumServer, error) {
        var id int
        err := DB.QueryRowContext(ctx, `SELECT id FROM forum_servers WHERE invite_token = $1`, token).Scan(&id)
        if err != nil {
                return nil, err
        }
        return GetServerByID(ctx, id, viewerID)
}

func RegenerateInviteToken(ctx context.Context, serverID int) (string, error) {
        token := generateInviteToken()
        _, err := DB.ExecContext(ctx, `UPDATE forum_servers SET invite_token=$2 WHERE id=$1`, serverID, token)
        return token, err
}

// EnsureInviteTokens generates invite tokens for any server that has an empty one.
func EnsureInviteTokens(ctx context.Context) error {
        rows, err := DB.QueryContext(ctx, `SELECT id FROM forum_servers WHERE invite_token = '' OR invite_token IS NULL`)
        if err != nil {
                return err
        }
        defer rows.Close()
        var ids []int
        for rows.Next() {
                var id int
                if err := rows.Scan(&id); err != nil {
                        return err
                }
                ids = append(ids, id)
        }
        for _, id := range ids {
                token := generateInviteToken()
                _, _ = DB.ExecContext(ctx, `UPDATE forum_servers SET invite_token=$2 WHERE id=$1`, id, token)
        }
        return nil
}

func GetServerBySlug(ctx context.Context, slug string, viewerID int) (*ForumServer, error) {
        var id int
        err := DB.QueryRowContext(ctx, `SELECT id FROM forum_servers WHERE slug = $1`, slug).Scan(&id)
        if err != nil {
                return nil, err
        }
        return GetServerByID(ctx, id, viewerID)
}

func GetServerByPublicID(ctx context.Context, publicID int64, viewerID int) (*ForumServer, error) {
        var id int
        err := DB.QueryRowContext(ctx, `SELECT id FROM forum_servers WHERE public_id = $1`, publicID).Scan(&id)
        if err != nil {
                return nil, err
        }
        return GetServerByID(ctx, id, viewerID)
}

func GetServerMembers(ctx context.Context, serverID int) ([]ServerMember, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT m.user_id, COALESCE(u.display_name, u.username), m.role, m.joined_at,
                        CASE WHEN u.last_seen_at > NOW() - INTERVAL '2 minutes' AND u.show_online_status = TRUE THEN TRUE ELSE FALSE END AS is_online,
                        COALESCE(m.via_invite, FALSE)
                FROM forum_server_members m
                JOIN users u ON u.id = m.user_id AND u.deleted_at IS NULL
                WHERE m.server_id = $1
                ORDER BY
                        CASE m.role WHEN 'owner' THEN 0 WHEN 'partner' THEN 1 ELSE 2 END,
                        u.username ASC`, serverID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var members []ServerMember
        for rows.Next() {
                var sm ServerMember
                if err := rows.Scan(&sm.UserID, &sm.Username, &sm.Role, &sm.JoinedAt, &sm.IsOnline, &sm.ViaInvite); err != nil {
                        return nil, err
                }
                members = append(members, sm)
        }
        return members, nil
}

func GetMemberRole(ctx context.Context, serverID, userID int) string {
        var role string
        _ = DB.QueryRowContext(ctx,
                `SELECT role FROM forum_server_members WHERE server_id=$1 AND user_id=$2`,
                serverID, userID).Scan(&role)
        return role
}

func SetMemberRole(ctx context.Context, serverID, targetUserID int, role string) error {
        _, err := DB.ExecContext(ctx,
                `UPDATE forum_server_members SET role=$3 WHERE server_id=$1 AND user_id=$2`,
                serverID, targetUserID, role)
        return err
}

func KickMember(ctx context.Context, serverID, targetUserID int) error {
        _, err := DB.ExecContext(ctx,
                `DELETE FROM forum_server_members WHERE server_id=$1 AND user_id=$2`, serverID, targetUserID)
        if err != nil {
                return err
        }
        _, err = DB.ExecContext(ctx,
                `DELETE FROM forum_server_follows WHERE server_id=$1 AND user_id=$2`, serverID, targetUserID)
        return err
}

func BanMember(ctx context.Context, serverID, targetUserID, bannedBy int, reason string) error {
        if err := KickMember(ctx, serverID, targetUserID); err != nil {
                return err
        }
        _, err := DB.ExecContext(ctx,
                `INSERT INTO forum_server_bans (server_id, user_id, banned_by, reason) VALUES ($1,$2,$3,$4)
                 ON CONFLICT (server_id, user_id) DO UPDATE SET reason=EXCLUDED.reason, banned_at=NOW()`,
                serverID, targetUserID, bannedBy, reason)
        return err
}

func UnbanMember(ctx context.Context, serverID, targetUserID int) error {
        _, err := DB.ExecContext(ctx,
                `DELETE FROM forum_server_bans WHERE server_id=$1 AND user_id=$2`, serverID, targetUserID)
        return err
}

func IsBanned(ctx context.Context, serverID, userID int) bool {
        var exists bool
        _ = DB.QueryRowContext(ctx,
                `SELECT EXISTS(SELECT 1 FROM forum_server_bans WHERE server_id=$1 AND user_id=$2)`,
                serverID, userID).Scan(&exists)
        return exists
}

func UpdateServerInfo(ctx context.Context, serverID int, name, description string, freeForAll bool) error {
        _, err := DB.ExecContext(ctx,
                `UPDATE forum_servers SET name=$2, description=$3, free_for_all=$4 WHERE id=$1`,
                serverID, name, description, freeForAll)
        return err
}

func normalizeHandle(h string) string {
        h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(h), "@")))
        return strings.Map(func(r rune) rune {
                if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
                        return r
                }
                return -1
        }, h)
}

// GetInternalServerID resolves a public_id (10-digit) to the internal DB id.
// Returns 0 if not found.
func GetInternalServerID(ctx context.Context, publicID int64) int {
        var id int
        _ = DB.QueryRowContext(ctx, `SELECT id FROM forum_servers WHERE public_id=$1`, publicID).Scan(&id)
        return id
}

func DeleteForumServer(ctx context.Context, internalID int, ownerID int) error {
        var actualOwnerID int
        err := DB.QueryRowContext(ctx, `SELECT creator_id FROM forum_servers WHERE id=$1`, internalID).Scan(&actualOwnerID)
        if err != nil {
                return fmt.Errorf("server not found")
        }
        if actualOwnerID != ownerID {
                return fmt.Errorf("forbidden")
        }
        _, err = DB.ExecContext(ctx, `DELETE FROM forum_servers WHERE id=$1`, internalID)
        return err
}

func CheckHandleAvailable(ctx context.Context, handle string, excludeServerID int) bool {
        handle = normalizeHandle(handle)
        if handle == "" {
                return false
        }
        var exists bool
        if excludeServerID > 0 {
                _ = DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM forum_servers WHERE handle=$1 AND id != $2)`, handle, excludeServerID).Scan(&exists)
        } else {
                _ = DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM forum_servers WHERE handle=$1)`, handle).Scan(&exists)
        }
        return !exists
}

func GetServerByHandle(ctx context.Context, handle string, viewerID int) (*ForumServer, error) {
        handle = normalizeHandle(handle)
        var id int
        err := DB.QueryRowContext(ctx, `SELECT id FROM forum_servers WHERE handle=$1`, handle).Scan(&id)
        if err != nil {
                return nil, err
        }
        return GetServerByID(ctx, id, viewerID)
}

func UpdateServerHandle(ctx context.Context, serverID int, handle string) error {
        handle = normalizeHandle(handle)
        if handle == "" {
                return fmt.Errorf("invalid handle")
        }
        _, err := DB.ExecContext(ctx, `UPDATE forum_servers SET handle=$2 WHERE id=$1`, serverID, handle)
        return err
}

func CreateForumServer(ctx context.Context, name, description, handle string, creatorID int) (*ForumServer, error) {
        slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
        slug = strings.Map(func(r rune) rune {
                if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
                        return r
                }
                return -1
        }, slug)
        if slug == "" {
                return nil, fmt.Errorf("invalid server name")
        }
        // Generate or normalize handle
        userProvidedHandle := strings.TrimSpace(handle) != ""
        if handle == "" {
                handle = normalizeHandle(name)
        } else {
                handle = normalizeHandle(handle)
        }
        if handle == "" {
                handle = slug
        }
        // If user explicitly provided a handle, error on conflict instead of silently renaming
        if userProvidedHandle {
                if !CheckHandleAvailable(ctx, handle, 0) {
                        return nil, fmt.Errorf("handle already exists: @%s", handle)
                }
        } else {
                // Auto-generated handle: append numbers until unique
                baseHandle := handle
                for i := 1; !CheckHandleAvailable(ctx, handle, 0); i++ {
                        handle = fmt.Sprintf("%s%d", baseHandle, i)
                }
        }
        token := generateInviteToken()
        var sv ForumServer
        publicID := generatePublicID(ctx)
        err := DB.QueryRowContext(ctx,
                `INSERT INTO forum_servers (name, slug, description, creator_id, invite_token, handle, public_id) VALUES ($1, $2, $3, $4, $5, $6, $7)
                 RETURNING id, COALESCE(public_id,0), name, slug, invite_token, COALESCE(handle,'')`,
                name, slug, description, creatorID, token, handle, publicID).Scan(&sv.ID, &sv.PublicID, &sv.Name, &sv.Slug, &sv.InviteToken, &sv.Handle)
        if err != nil {
                return nil, err
        }
        _, _ = DB.ExecContext(ctx,
                `INSERT INTO forum_server_follows (server_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
                sv.ID, creatorID)
        _, _ = DB.ExecContext(ctx,
                `INSERT INTO forum_server_members (server_id, user_id, role) VALUES ($1, $2, 'owner')
                 ON CONFLICT (server_id, user_id) DO UPDATE SET role='owner'`,
                sv.ID, creatorID)
        sv.Initials = serverInitials(sv.Name)
        return &sv, nil
}

// JoinServer adds a user to a server. inviteToken must be the server's real
// invite token when the server is not free_for_all; pass "" for free servers.
func JoinServer(ctx context.Context, serverID, userID int, inviteToken string) error {
        if IsBanned(ctx, serverID, userID) {
                return fmt.Errorf("you are banned from this server")
        }

        // Verify invite requirement server-side — never trust a client boolean.
        var freeForAll bool
        var storedToken string
        err := DB.QueryRowContext(ctx,
                `SELECT COALESCE(free_for_all, TRUE), COALESCE(invite_token,'') FROM forum_servers WHERE id=$1`,
                serverID).Scan(&freeForAll, &storedToken)
        if err != nil {
                return fmt.Errorf("server not found")
        }
        viaInvite := false
        if !freeForAll {
                if inviteToken == "" || storedToken == "" || inviteToken != storedToken {
                        return fmt.Errorf("this server requires a valid invite link")
                }
                viaInvite = true
        }

        _, err = DB.ExecContext(ctx,
                `INSERT INTO forum_server_follows (server_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
                serverID, userID)
        if err != nil {
                return err
        }
        _, err = DB.ExecContext(ctx,
                `INSERT INTO forum_server_members (server_id, user_id, role, via_invite) VALUES ($1,$2,'member',$3) ON CONFLICT (server_id, user_id) DO NOTHING`,
                serverID, userID, viaInvite)
        return err
}

func LeaveServer(ctx context.Context, serverID, userID int) error {
        role := GetMemberRole(ctx, serverID, userID)
        if role == "owner" {
                return fmt.Errorf("owner cannot leave the server")
        }
        _, err := DB.ExecContext(ctx,
                `DELETE FROM forum_server_follows WHERE server_id=$1 AND user_id=$2`, serverID, userID)
        if err != nil {
                return err
        }
        _, err = DB.ExecContext(ctx,
                `DELETE FROM forum_server_members WHERE server_id=$1 AND user_id=$2`, serverID, userID)
        return err
}

func ToggleServerFollow(ctx context.Context, serverID, userID int) (bool, error) {
        var exists bool
        err := DB.QueryRowContext(ctx,
                `SELECT EXISTS(SELECT 1 FROM forum_server_follows WHERE server_id=$1 AND user_id=$2)`,
                serverID, userID).Scan(&exists)
        if err != nil {
                return false, err
        }
        if exists {
                if err := LeaveServer(ctx, serverID, userID); err != nil {
                        // If owner can't leave, just unfollow from feed
                        _, _ = DB.ExecContext(ctx,
                                `DELETE FROM forum_server_follows WHERE server_id=$1 AND user_id=$2`, serverID, userID)
                }
                return false, nil
        }
        return true, JoinServer(ctx, serverID, userID, "")
}

func ToggleUserFollow(ctx context.Context, followerID, followedID int) (bool, error) {
        var exists bool
        err := DB.QueryRowContext(ctx,
                `SELECT EXISTS(SELECT 1 FROM forum_user_follows WHERE follower_id=$1 AND followed_id=$2)`,
                followerID, followedID).Scan(&exists)
        if err != nil {
                return false, err
        }
        if exists {
                _, err = DB.ExecContext(ctx,
                        `DELETE FROM forum_user_follows WHERE follower_id=$1 AND followed_id=$2`, followerID, followedID)
                return false, err
        }
        _, err = DB.ExecContext(ctx,
                `INSERT INTO forum_user_follows (follower_id, followed_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
                followerID, followedID)
        return true, err
}

func GetPostComments(ctx context.Context, postID, viewerID int) ([]ForumComment, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT c.id, c.post_id, c.author_id, COALESCE(u.display_name, u.username), COALESCE(u.avatar_path,''),
                        c.body, c.parent_id, c.created_at,
                        COALESCE(SUM(cv.value), 0) AS score,
                        COALESCE(mv.value, 0) AS user_vote
                FROM forum_comments c
                JOIN users u ON u.id = c.author_id
                LEFT JOIN forum_comment_votes cv ON cv.comment_id = c.id
                LEFT JOIN forum_comment_votes mv ON mv.comment_id = c.id AND mv.user_id = $2
                WHERE c.post_id = $1 AND c.is_deleted = FALSE
                GROUP BY c.id, u.display_name, u.username, u.avatar_path, mv.value
                ORDER BY c.created_at ASC`, postID, viewerID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var all []ForumComment
        for rows.Next() {
                var cm ForumComment
                if err := rows.Scan(&cm.ID, &cm.PostID, &cm.AuthorID, &cm.AuthorName, &cm.AuthorAvatarURL,
                        &cm.Body, &cm.ParentID, &cm.CreatedAt, &cm.Score, &cm.UserVote); err != nil {
                        return nil, err
                }
                cm.AuthorAvatarURL = dbAssetURL(cm.AuthorAvatarURL)
                all = append(all, cm)
        }
        idxMap := make(map[int]int)
        for i := range all {
                idxMap[all[i].ID] = i
        }
        // Reverse iterate: deepest comments first so children are already
        // populated when their parent copies them (avoids stale-copy bug).
        for i := len(all) - 1; i >= 0; i-- {
                if all[i].ParentID.Valid {
                        parentIdx, ok := idxMap[int(all[i].ParentID.Int64)]
                        if ok {
                                all[parentIdx].Replies = append([]ForumComment{all[i]}, all[parentIdx].Replies...)
                        }
                }
        }
        var roots []ForumComment
        for i := range all {
                if !all[i].ParentID.Valid {
                        roots = append(roots, all[i])
                }
        }
        return roots, nil
}

func VoteOnComment(ctx context.Context, commentID, userID, value int) (int, int, error) {
        if value == 0 {
                _, err := DB.ExecContext(ctx,
                        `DELETE FROM forum_comment_votes WHERE comment_id=$1 AND user_id=$2`, commentID, userID)
                if err != nil {
                        return 0, 0, err
                }
        } else {
                _, err := DB.ExecContext(ctx,
                        `INSERT INTO forum_comment_votes (comment_id, user_id, value) VALUES ($1,$2,$3)
                         ON CONFLICT (comment_id, user_id) DO UPDATE SET value=EXCLUDED.value`,
                        commentID, userID, value)
                if err != nil {
                        return 0, 0, err
                }
        }
        var score int
        err := DB.QueryRowContext(ctx,
                `SELECT COALESCE(SUM(value),0) FROM forum_comment_votes WHERE comment_id=$1`, commentID).Scan(&score)
        return score, value, err
}

func IsUserFollowing(ctx context.Context, followerID, followedID int) (bool, error) {
        var count int
        err := DB.QueryRowContext(ctx,
                `SELECT COUNT(*) FROM forum_user_follows WHERE follower_id=$1 AND followed_id=$2`,
                followerID, followedID).Scan(&count)
        return count > 0, err
}

func GetFollowedUsers(ctx context.Context, userID int) ([]User, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.email,''),
                        COALESCE(u.bio,''), COALESCE(u.avatar_path,''), COALESCE(u.banner_path,''),
                        u.created_at, u.last_seen_at, COALESCE(u.show_online_status, true)
                FROM forum_user_follows f
                JOIN users u ON u.id = f.followed_id AND u.deleted_at IS NULL
                WHERE f.follower_id = $1
                ORDER BY u.username ASC`, userID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var users []User
        for rows.Next() {
                var u User
                if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email,
                        &u.Bio, &u.AvatarPath, &u.BannerPath, &u.CreatedAt,
                        &u.LastSeenAt, &u.ShowOnlineStatus); err != nil {
                        return nil, err
                }
                users = append(users, u)
        }
        return users, nil
}

func SeedForumDemoData(_ context.Context) error {
        return nil
}

func UpdateServerLogo(ctx context.Context, serverID int, url string) error {
        _, err := DB.ExecContext(ctx, `UPDATE forum_servers SET logo_url=$2 WHERE id=$1`, serverID, url)
        return err
}

func UpdateServerBanner(ctx context.Context, serverID int, url string) error {
        _, err := DB.ExecContext(ctx, `UPDATE forum_servers SET banner_url=$2 WHERE id=$1`, serverID, url)
        return err
}

func GetUserPublicProfile(ctx context.Context, username string) (*User, error) {
        var u User
        err := DB.QueryRowContext(ctx, `
                SELECT id, COALESCE(display_name,''), username, COALESCE(bio,''),
                        COALESCE(avatar_path,''), COALESCE(banner_path,''), last_seen_at, COALESCE(show_online_status,true)
                FROM users WHERE username=$1 AND deleted_at IS NULL`, username).
                Scan(&u.ID, &u.DisplayName, &u.Username, &u.Bio, &u.AvatarPath, &u.BannerPath, &u.LastSeenAt, &u.ShowOnlineStatus)
        if err != nil {
                return nil, err
        }
        return &u, nil
}

func CreateComment(ctx context.Context, postID, authorID int, body string, parentID sql.NullInt64) (int, error) {
        // Validate parent belongs to the same post to prevent cross-post injection
        if parentID.Valid {
                var parentPostID int
                err := DB.QueryRowContext(ctx,
                        `SELECT post_id FROM forum_comments WHERE id=$1 AND is_deleted=FALSE`,
                        parentID.Int64).Scan(&parentPostID)
                if err != nil || parentPostID != postID {
                        return 0, fmt.Errorf("invalid parent comment")
                }
        }
        var id int
        err := DB.QueryRowContext(ctx,
                `INSERT INTO forum_comments (post_id, author_id, body, parent_id)
                 VALUES ($1, $2, $3, $4) RETURNING id`,
                postID, authorID, body, parentID).Scan(&id)
        return id, err
}

func GetHashtags(ctx context.Context, limit int) ([]ForumCategory, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT h.id, h.name, h.name, COUNT(ph.post_id)
                FROM forum_hashtags h
                LEFT JOIN forum_post_hashtags ph ON ph.hashtag_id = h.id
                GROUP BY h.id ORDER BY COUNT(ph.post_id) DESC LIMIT $1`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var tags []ForumCategory
        for rows.Next() {
                var c ForumCategory
                if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.PostCount); err != nil {
                        return nil, err
                }
                tags = append(tags, c)
        }
        return tags, nil
}

func GetOnlineUsersForForum(ctx context.Context, limit int) ([]OnlineUser, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT COALESCE(u.display_name, u.username),
                        EXTRACT(EPOCH FROM (NOW() - u.last_seen_at))/60
                FROM users u
                WHERE u.last_seen_at > NOW() - INTERVAL '10 minutes'
                        AND u.deleted_at IS NULL
                        AND u.show_online_status = TRUE
                ORDER BY u.last_seen_at DESC
                LIMIT $1`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var users []OnlineUser
        for rows.Next() {
                var u OnlineUser
                var mins float64
                if err := rows.Scan(&u.Username, &mins); err != nil {
                        return nil, err
                }
                u.MinutesAgo = int(mins)
                if len(u.Username) >= 2 {
                        u.Initials = strings.ToUpper(u.Username[:2])
                } else if len(u.Username) == 1 {
                        u.Initials = strings.ToUpper(u.Username[:1])
                }
                users = append(users, u)
        }
        return users, nil
}

type ForumSearchResponse struct {
        Suggestions []string           `json:"suggestions"`
        Results     []ForumSearchResult `json:"results"`
}

func SearchForums(ctx context.Context, query, filterType string, viewerID, limit int) (*ForumSearchResponse, error) {
        resp := &ForumSearchResponse{
                Suggestions: []string{},
                Results:     []ForumSearchResult{},
        }
        q := strings.TrimSpace(query)
        if q == "" {
                return resp, nil
        }

        // Always fetch title suggestions from posts (shown as text completions)
        if filterType == "" || filterType == "posts" {
                srows, serr := DB.QueryContext(ctx, `
                        SELECT DISTINCT LEFT(p.title, 70)
                        FROM forum_posts p
                        JOIN users u ON u.id = p.author_id AND u.deleted_at IS NULL
                        WHERE p.title ILIKE $1
                        ORDER BY 1 ASC
                        LIMIT 5`, "%"+q+"%")
                if serr == nil {
                        defer srows.Close()
                        for srows.Next() {
                                var s string
                                if srows.Scan(&s) == nil {
                                        resp.Suggestions = append(resp.Suggestions, s)
                                }
                        }
                }
        }

        if filterType == "servers" || filterType == "" {
                rows, err := DB.QueryContext(ctx, `
                        SELECT s.id, COALESCE(s.public_id, 0), s.name, s.slug, COALESCE(s.description,''),
                                COUNT(DISTINCT m.user_id) AS member_count
                        FROM forum_servers s
                        LEFT JOIN forum_server_members m ON m.server_id = s.id
                        WHERE s.name ILIKE $1 OR s.slug ILIKE $1 OR s.description ILIKE $1 OR s.handle ILIKE $1
                        GROUP BY s.id
                        ORDER BY member_count DESC, s.name ASC
                        LIMIT $2`, "%"+q+"%", limit)
                if err == nil {
                        defer rows.Close()
                        for rows.Next() {
                                var r ForumSearchResult
                                var srvPublicID int64
                                r.Type = "server"
                                if err := rows.Scan(&r.ID, &srvPublicID, &r.Name, &r.Slug, &r.Snippet, &r.MemberCount); err == nil {
                                        r.URL = fmt.Sprintf("/forums/server/%d", srvPublicID)
                                        resp.Results = append(resp.Results, r)
                                }
                        }
                }
                if filterType == "servers" {
                        return resp, nil
                }
        }

        if filterType == "users" || filterType == "" {
                rows, err := DB.QueryContext(ctx, `
                        SELECT u.id, COALESCE(u.display_name, u.username), u.username, COALESCE(u.bio,'')
                        FROM users u
                        WHERE (u.username ILIKE $1 OR u.display_name ILIKE $1)
                                AND u.deleted_at IS NULL
                        ORDER BY u.username ASC
                        LIMIT $2`, "%"+q+"%", limit)
                if err == nil {
                        defer rows.Close()
                        for rows.Next() {
                                var r ForumSearchResult
                                r.Type = "user"
                                if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.Snippet); err == nil {
                                        r.URL = "/profile/" + r.Slug
                                        resp.Results = append(resp.Results, r)
                                }
                        }
                }
                if filterType == "users" {
                        return resp, nil
                }
        }

        // Post results (shown as cards in dropdown)
        if filterType == "" || filterType == "posts" {
                rows, err := DB.QueryContext(ctx, `
                        SELECT p.id, p.title, '', LEFT(COALESCE(p.body,''), 80)
                        FROM forum_posts p
                        JOIN users u ON u.id = p.author_id AND u.deleted_at IS NULL
                        WHERE p.title ILIKE $1 OR p.body ILIKE $1
                        ORDER BY p.created_at DESC
                        LIMIT $2`, "%"+q+"%", 4)
                if err == nil {
                        defer rows.Close()
                        for rows.Next() {
                                var r ForumSearchResult
                                r.Type = "post"
                                if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.Snippet); err == nil {
                                        r.URL = fmt.Sprintf("/forums/post/%d", r.ID)
                                        resp.Results = append(resp.Results, r)
                                }
                        }
                }
        }

        return resp, nil
}

func IncrementPostView(ctx context.Context, postID int) {
        DB.ExecContext(ctx, `UPDATE forum_posts SET view_count = COALESCE(view_count,0) + 1 WHERE id = $1`, postID)
}

func GetUserForumPosts(ctx context.Context, username string, viewerID, limit, offset int) ([]ForumPost, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT
                        p.id, p.author_id, COALESCE(u.display_name, u.username), COALESCE(u.avatar_path,''),
                        p.server_id,
                        COALESCE(s.name,''), COALESCE(s.slug,''), p.category_id, COALESCE(c.name,''), COALESCE(c.slug,''),
                        p.title, p.body, COALESCE(p.image_url,''), p.created_at,
                        (SELECT COALESCE(SUM(value),0) FROM forum_votes WHERE post_id = p.id) AS score,
                        (SELECT COUNT(*) FROM forum_comments WHERE post_id = p.id AND is_deleted = FALSE) AS comment_count,
                        COALESCE((SELECT value FROM forum_votes WHERE post_id = p.id AND user_id = $2), 0) AS user_vote,
                        COALESCE(p.view_count, 0) AS view_count
                FROM forum_posts p
                JOIN users u ON u.id = p.author_id AND u.username = $1
                LEFT JOIN forum_servers s ON s.id = p.server_id
                LEFT JOIN forum_categories c ON c.id = p.category_id
                WHERE u.deleted_at IS NULL
                GROUP BY p.id, u.display_name, u.username, u.avatar_path, s.name, s.slug, c.name, c.slug
                ORDER BY p.created_at DESC
                LIMIT $3 OFFSET $4`, username, viewerID, limit, offset)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var posts []ForumPost
        for rows.Next() {
                var p ForumPost
                if err := rows.Scan(&p.ID, &p.AuthorID, &p.AuthorName, &p.AuthorAvatarURL, &p.ServerID,
                        &p.ServerName, &p.ServerSlug, &p.CategoryID, &p.CategoryName, &p.CategorySlug,
                        &p.Title, &p.Body, &p.ImageURL, &p.CreatedAt,
                        &p.Score, &p.CommentCount, &p.UserVote, &p.ViewCount); err != nil {
                        return nil, err
                }
                p.AuthorAvatarURL = dbAssetURL(p.AuthorAvatarURL)
                posts = append(posts, p)
        }
        for i := range posts {
                tags, _ := getPostHashtags(ctx, posts[i].ID)
                posts[i].Hashtags = tags
        }
        return posts, nil
}

func GetUserForumReplies(ctx context.Context, username string, viewerID, limit, offset int) ([]ForumComment, error) {
        rows, err := DB.QueryContext(ctx, `
                SELECT c.id, c.post_id, c.author_id, COALESCE(u.display_name, u.username), COALESCE(u.avatar_path,''),
                        c.body, c.parent_id, c.created_at,
                        COALESCE(SUM(cv.value), 0) AS score,
                        COALESCE(mv.value, 0) AS user_vote
                FROM forum_comments c
                JOIN users u ON u.id = c.author_id AND u.username = $1
                LEFT JOIN forum_comment_votes cv ON cv.comment_id = c.id
                LEFT JOIN forum_comment_votes mv ON mv.comment_id = c.id AND mv.user_id = $2
                WHERE c.is_deleted = FALSE AND u.deleted_at IS NULL
                GROUP BY c.id, u.display_name, u.username, u.avatar_path, mv.value
                ORDER BY c.created_at DESC
                LIMIT $3 OFFSET $4`, username, viewerID, limit, offset)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var comments []ForumComment
        for rows.Next() {
                var cm ForumComment
                if err := rows.Scan(&cm.ID, &cm.PostID, &cm.AuthorID, &cm.AuthorName, &cm.AuthorAvatarURL,
                        &cm.Body, &cm.ParentID, &cm.CreatedAt, &cm.Score, &cm.UserVote); err != nil {
                        return nil, err
                }
                cm.AuthorAvatarURL = dbAssetURL(cm.AuthorAvatarURL)
                comments = append(comments, cm)
        }
        return comments, nil
}

func DeleteForumPost(ctx context.Context, postID, userID int) error {
        res, err := DB.ExecContext(ctx, `DELETE FROM forum_posts WHERE id=$1 AND author_id=$2`, postID, userID)
        if err != nil {
                return err
        }
        n, _ := res.RowsAffected()
        if n == 0 {
                return fmt.Errorf("not found or not authorized")
        }
        return nil
}

func UpdateForumPost(ctx context.Context, postID, userID int, title, body string) error {
        res, err := DB.ExecContext(ctx, `UPDATE forum_posts SET title=$1, body=$2 WHERE id=$3 AND author_id=$4`, title, body, postID, userID)
        if err != nil {
                return err
        }
        n, _ := res.RowsAffected()
        if n == 0 {
                return fmt.Errorf("not found or not authorized")
        }
        return nil
}

func DeleteForumComment(ctx context.Context, commentID, userID int) error {
        res, err := DB.ExecContext(ctx, `UPDATE forum_comments SET is_deleted=TRUE, body='[deleted]' WHERE id=$1 AND author_id=$2`, commentID, userID)
        if err != nil {
                return err
        }
        n, _ := res.RowsAffected()
        if n == 0 {
                return fmt.Errorf("not found or not authorized")
        }
        return nil
}

func UpdateForumComment(ctx context.Context, commentID, userID int, body string) error {
        res, err := DB.ExecContext(ctx, `UPDATE forum_comments SET body=$1 WHERE id=$2 AND author_id=$3 AND is_deleted=FALSE`, body, commentID, userID)
        if err != nil {
                return err
        }
        n, _ := res.RowsAffected()
        if n == 0 {
                return fmt.Errorf("not found or not authorized")
        }
        return nil
}

func GetUserForumStats(ctx context.Context, username string) (postCount int, replyCount int, totalScore int) {
        DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM forum_posts p JOIN users u ON u.id=p.author_id WHERE u.username=$1 AND u.deleted_at IS NULL`, username).Scan(&postCount)
        DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM forum_comments c JOIN users u ON u.id=c.author_id WHERE u.username=$1 AND c.is_deleted=FALSE AND u.deleted_at IS NULL`, username).Scan(&replyCount)
        DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(v.value),0) FROM forum_votes v JOIN forum_posts p ON p.id=v.post_id JOIN users u ON u.id=p.author_id WHERE u.username=$1 AND u.deleted_at IS NULL`, username).Scan(&totalScore)
        return
}
