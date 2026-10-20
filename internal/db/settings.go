package db

import "strconv"

func GetSiteSettingInt(key string, def int) int {
	var val string
	err := DB.QueryRow(`SELECT value FROM site_settings WHERE key = $1`, key).Scan(&val)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return n
}

func GetSiteSettingStr(key, def string) string {
	var val string
	err := DB.QueryRow(`SELECT value FROM site_settings WHERE key = $1`, key).Scan(&val)
	if err != nil {
		return def
	}
	return val
}

func SetSiteSetting(key, value string) error {
	_, err := DB.Exec(`
		INSERT INTO site_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
	`, key, value)
	return err
}
