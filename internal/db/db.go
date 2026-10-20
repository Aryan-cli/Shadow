package db

import (
        "database/sql"
        "fmt"
        "os"

        _ "github.com/lib/pq"
)

var DB *sql.DB

func Connect() error {
        dsn := os.Getenv("DATABASE_URL")
        if dsn == "" {
                return fmt.Errorf("DATABASE_URL environment variable is not set")
        }
        var err error
        DB, err = sql.Open("postgres", dsn)
        if err != nil {
                return fmt.Errorf("failed to open database: %w", err)
        }
        if err := DB.Ping(); err != nil {
                return fmt.Errorf("failed to ping database: %w", err)
        }
        DB.SetMaxOpenConns(25)
        DB.SetMaxIdleConns(5)
        return nil
}

func Migrate() error {
        queries := []string{
                `CREATE TABLE IF NOT EXISTS users (
                        id SERIAL PRIMARY KEY,
                        username VARCHAR(50) UNIQUE NOT NULL,
                        display_name VARCHAR(100),
                        password_hash TEXT NOT NULL,
                        bio TEXT DEFAULT '',
                        avatar_path TEXT DEFAULT '',
                        created_at TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE TABLE IF NOT EXISTS sessions (
                        id TEXT PRIMARY KEY,
                        user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        expires_at TIMESTAMPTZ NOT NULL,
                        last_seen TIMESTAMPTZ DEFAULT NOW()
                )`,
                `CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
                `CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
                `ALTER TABLE users ADD COLUMN IF NOT EXISTS banner_path TEXT DEFAULT ''`,
                `ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT DEFAULT ''`,
                `ALTER TABLE users ADD COLUMN IF NOT EXISTS banned BOOLEAN DEFAULT false`,
                `ALTER TABLE users ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ`,
                `ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ`,
                `ALTER TABLE users ADD COLUMN IF NOT EXISTS show_online_status BOOLEAN DEFAULT true`,
                `ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
                `CREATE TABLE IF NOT EXISTS site_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
                `INSERT INTO site_settings (key, value) VALUES ('online_threshold_seconds', '30')  ON CONFLICT (key) DO NOTHING`,
                `INSERT INTO site_settings (key, value) VALUES ('idle_threshold_seconds',   '600') ON CONFLICT (key) DO NOTHING`,
                `CREATE TABLE IF NOT EXISTS friendships (
                        id SERIAL PRIMARY KEY,
                        requester_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        addressee_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        status VARCHAR(20) DEFAULT 'pending',
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        UNIQUE(requester_id, addressee_id)
                )`,
                `CREATE INDEX IF NOT EXISTS idx_friendships_requester ON friendships(requester_id)`,
                `CREATE INDEX IF NOT EXISTS idx_friendships_addressee ON friendships(addressee_id)`,
                `CREATE TABLE IF NOT EXISTS messages (
                        id SERIAL PRIMARY KEY,
                        sender_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        receiver_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        content TEXT NOT NULL,
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        read_at TIMESTAMPTZ
                )`,
                `CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender_id)`,
                `CREATE INDEX IF NOT EXISTS idx_messages_receiver ON messages(receiver_id)`,
                `ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN DEFAULT FALSE`,
                `ALTER TABLE messages ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ`,
                `ALTER TABLE messages ADD COLUMN IF NOT EXISTS reply_to_id INTEGER REFERENCES messages(id) ON DELETE SET NULL`,
                `CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(sender_id, receiver_id, created_at DESC)`,
                `CREATE TABLE IF NOT EXISTS message_requests (
                        id SERIAL PRIMARY KEY,
                        from_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        to_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        initial_message TEXT NOT NULL DEFAULT '',
                        status TEXT DEFAULT 'pending',
                        created_at TIMESTAMPTZ DEFAULT NOW(),
                        UNIQUE(from_id, to_id)
                )`,
                `CREATE INDEX IF NOT EXISTS idx_msgreq_to ON message_requests(to_id, status)`,
                `CREATE TABLE IF NOT EXISTS hidden_conversations (
                        user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        other_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
                        hidden_at TIMESTAMPTZ DEFAULT NOW(),
                        PRIMARY KEY(user_id, other_user_id)
                )`,
        }
        for _, q := range queries {
                if _, err := DB.Exec(q); err != nil {
                        return fmt.Errorf("migration failed: %w", err)
                }
        }
        if err := MigrateForums(); err != nil {
                return err
        }
        if err := MigrateMarketplace(); err != nil {
                return err
        }
        if err := MigratePayments(); err != nil {
                return err
        }
        return MigrateSupport()
}
