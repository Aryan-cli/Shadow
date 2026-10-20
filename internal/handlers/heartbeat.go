package handlers

import (
	"net/http"
	"shadow/internal/db"
	"shadow/internal/session"
)

func HeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	_, sessData, _ := session.FromRequest(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if sessData == nil {
		w.Write([]byte(`{"ok":false}`))
		return
	}
	_ = db.UpdateLastSeen(sessData.UserID)
	w.Write([]byte(`{"ok":true}`))
}
