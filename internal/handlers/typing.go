package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"shadow/internal/db"
	"shadow/internal/session"
	"strings"
	"sync"
	"time"
)

var typingMu sync.Mutex
var typingStore = map[string]time.Time{}

func APITypingHandler(w http.ResponseWriter, r *http.Request) {
	_, sessData, _ := session.FromRequest(r)
	if sessData == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodPost {
		r.ParseForm()
		toUsername := strings.TrimSpace(r.FormValue("to"))
		if toUsername == "" {
			http.Error(w, "missing to", http.StatusBadRequest)
			return
		}
		toUser, err := db.GetUserByUsername(toUsername)
		if err != nil || toUser == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		key := fmt.Sprintf("%d:%d", sessData.UserID, toUser.ID)
		typingMu.Lock()
		typingStore[key] = time.Now()
		typingMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
		return
	}

	withUsername := strings.TrimSpace(r.URL.Query().Get("with"))
	if withUsername == "" {
		http.Error(w, "missing with", http.StatusBadRequest)
		return
	}
	withUser, err := db.GetUserByUsername(withUsername)
	if err != nil || withUser == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	key := fmt.Sprintf("%d:%d", withUser.ID, sessData.UserID)
	typingMu.Lock()
	t, ok := typingStore[key]
	if ok && time.Since(t) >= 4*time.Second {
		delete(typingStore, key)
		ok = false
	}
	typingMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"typing": ok})
}
