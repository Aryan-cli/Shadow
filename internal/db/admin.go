package db

import (
        "fmt"
        "time"
)

type AdminUser struct {
        ID           int
        Username     string
        DisplayName  string
        Email        string
        Bio          string
        AvatarPath   string
        BannerPath   string
        CreatedAt    time.Time
        Banned       bool
        SessionCount int
        IsOnline     bool
        IsIdle       bool
}

func AdminCountUsers() (int, error) {
        var count int
        err := DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
        return count, err
}

func AdminCountOnlineUsers() (int, error) {
        onlineSecs := GetSiteSettingInt("online_threshold_seconds", 30)
        var count int
        err := DB.QueryRow(fmt.Sprintf(`
                SELECT COUNT(*) FROM users
                WHERE last_seen_at > NOW() - INTERVAL '%d seconds'
                  AND deleted_at IS NULL
        `, onlineSecs)).Scan(&count)
        return count, err
}

func AdminListUsers(search string) ([]AdminUser, error) {
        query := `
                SELECT u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.email,''),
                       COALESCE(u.bio,''), COALESCE(u.avatar_path,''), COALESCE(u.banner_path,''),
                       u.created_at, COALESCE(u.banned, false),
                       COUNT(s.id) FILTER (WHERE s.expires_at > NOW()) AS active_sessions,
                       COALESCE(u.last_seen_at > NOW() - INTERVAL '%d seconds', false) AS is_online,
                       COALESCE(u.last_seen_at > NOW() - INTERVAL '%d seconds' AND u.last_seen_at <= NOW() - INTERVAL '%d seconds', false) AS is_idle
                FROM users u
                LEFT JOIN sessions s ON s.user_id = u.id
        `
        onlineSecs := GetSiteSettingInt("online_threshold_seconds", 30)
        idleSecs   := GetSiteSettingInt("idle_threshold_seconds", 600)
        query = fmt.Sprintf(query, onlineSecs, idleSecs, onlineSecs)
        args := []interface{}{}
        if search != "" {
                query += ` WHERE u.username ILIKE $1 OR u.display_name ILIKE $1 OR u.email ILIKE $1`
                args = append(args, "%"+search+"%")
        }
        query += ` GROUP BY u.id ORDER BY u.created_at DESC`

        rows, err := DB.Query(query, args...)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var users []AdminUser
        for rows.Next() {
                var u AdminUser
                if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email,
                        &u.Bio, &u.AvatarPath, &u.BannerPath, &u.CreatedAt,
                        &u.Banned, &u.SessionCount, &u.IsOnline, &u.IsIdle); err != nil {
                        return nil, err
                }
                users = append(users, u)
        }
        return users, rows.Err()
}

func AdminGetUser(id int) (*AdminUser, error) {
        onlineSecs := GetSiteSettingInt("online_threshold_seconds", 30)
        idleSecs   := GetSiteSettingInt("idle_threshold_seconds", 600)
        var u AdminUser
        q := fmt.Sprintf(`
                SELECT u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.email,''),
                       COALESCE(u.bio,''), COALESCE(u.avatar_path,''), COALESCE(u.banner_path,''),
                       u.created_at, COALESCE(u.banned, false),
                       COUNT(s.id) FILTER (WHERE s.expires_at > NOW()) AS active_sessions,
                       COALESCE(u.last_seen_at > NOW() - INTERVAL '%d seconds', false) AS is_online,
                       COALESCE(u.last_seen_at > NOW() - INTERVAL '%d seconds' AND u.last_seen_at <= NOW() - INTERVAL '%d seconds', false) AS is_idle
                FROM users u
                LEFT JOIN sessions s ON s.user_id = u.id
                WHERE u.id = $1
                GROUP BY u.id
        `, onlineSecs, idleSecs, onlineSecs)
        err := DB.QueryRow(q, id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email,
                &u.Bio, &u.AvatarPath, &u.BannerPath, &u.CreatedAt,
                &u.Banned, &u.SessionCount, &u.IsOnline, &u.IsIdle)
        if err != nil {
                return nil, err
        }
        return &u, nil
}

func AdminBanUser(id int) error {
        _, err := DB.Exec(`UPDATE users SET banned = true WHERE id = $1`, id)
        return err
}

func AdminUnbanUser(id int) error {
        _, err := DB.Exec(`UPDATE users SET banned = false WHERE id = $1`, id)
        return err
}

func AdminDeleteUser(id int) error {
        _, err := DB.Exec(`DELETE FROM users WHERE id = $1`, id)
        return err
}
