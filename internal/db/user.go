package db

import (
	"database/sql"
	"time"
)

type User struct {
	ID               int
	Username         string
	DisplayName      string
	Email            string
	Bio              string
	AvatarPath       string
	BannerPath       string
	CreatedAt        time.Time
	LastSeenAt       *time.Time
	ShowOnlineStatus bool
}

func GetUserByUsername(username string) (*User, error) {
	row := DB.QueryRow(
		`SELECT id, username, COALESCE(display_name,''), COALESCE(email,''), COALESCE(bio,''), COALESCE(avatar_path,''), COALESCE(banner_path,''), created_at, last_seen_at, COALESCE(show_online_status, true)
		 FROM users WHERE username = $1 AND deleted_at IS NULL`, username)
	return scanUser(row)
}

func GetUserByID(id int) (*User, error) {
	row := DB.QueryRow(
		`SELECT id, username, COALESCE(display_name,''), COALESCE(email,''), COALESCE(bio,''), COALESCE(avatar_path,''), COALESCE(banner_path,''), created_at, last_seen_at, COALESCE(show_online_status, true)
		 FROM users WHERE id = $1`, id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Bio, &u.AvatarPath, &u.BannerPath, &u.CreatedAt, &u.LastSeenAt, &u.ShowOnlineStatus)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func UpdateDisplayName(id int, name string) error {
	_, err := DB.Exec(`UPDATE users SET display_name=$1 WHERE id=$2`, name, id)
	return err
}

func UpdateBio(id int, bio string) error {
	_, err := DB.Exec(`UPDATE users SET bio=$1 WHERE id=$2`, bio, id)
	return err
}

func UpdateEmail(id int, email string) error {
	_, err := DB.Exec(`UPDATE users SET email=$1 WHERE id=$2`, email, id)
	return err
}

func UpdateAvatarPath(id int, path string) error {
	_, err := DB.Exec(`UPDATE users SET avatar_path=$1 WHERE id=$2`, path, id)
	return err
}

func UpdateBannerPath(id int, path string) error {
	_, err := DB.Exec(`UPDATE users SET banner_path=$1 WHERE id=$2`, path, id)
	return err
}

func RemoveAvatarPath(id int) error {
	_, err := DB.Exec(`UPDATE users SET avatar_path='' WHERE id=$1`, id)
	return err
}

func GetPasswordHash(id int) (string, error) {
	var hash string
	err := DB.QueryRow(`SELECT password_hash FROM users WHERE id=$1`, id).Scan(&hash)
	return hash, err
}

func UpdatePassword(id int, hash string) error {
	_, err := DB.Exec(`UPDATE users SET password_hash=$1 WHERE id=$2`, hash, id)
	return err
}

func GetUserBanned(id int) (bool, error) {
	var banned bool
	err := DB.QueryRow(`SELECT COALESCE(banned, false) FROM users WHERE id = $1`, id).Scan(&banned)
	if err == sql.ErrNoRows {
		return false, sql.ErrNoRows
	}
	return banned, err
}

func UpdateLastSeen(id int) error {
	_, err := DB.Exec(`UPDATE users SET last_seen_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	return err
}

func UpdateLastLogin(id int) error {
	_, err := DB.Exec(`UPDATE users SET last_login_at = NOW() WHERE id = $1`, id)
	return err
}

func UpdateShowOnlineStatus(id int, show bool) error {
	_, err := DB.Exec(`UPDATE users SET show_online_status = $1 WHERE id = $2`, show, id)
	return err
}

func GetShowOnlineStatus(id int) (bool, error) {
	var show bool
	err := DB.QueryRow(`SELECT COALESCE(show_online_status, true) FROM users WHERE id = $1`, id).Scan(&show)
	return show, err
}

func SoftDeleteUser(id int) error {
	_, err := DB.Exec(`UPDATE users SET deleted_at = NOW() WHERE id = $1`, id)
	return err
}

func RestoreUser(id int) error {
	_, err := DB.Exec(`UPDATE users SET deleted_at = NULL WHERE id = $1`, id)
	return err
}

func GetLastActivity(userID int) *time.Time {
	var t time.Time
	err := DB.QueryRow(`SELECT COALESCE(MAX(last_seen), MAX(created_at)) FROM sessions WHERE user_id = $1`, userID).Scan(&t)
	if err != nil || t.IsZero() {
		return nil
	}
	return &t
}
