package handlers

import (
        "encoding/json"
        "fmt"
        "html/template"
        "log"
        "net/http"
        "path/filepath"
        "shadow/internal/db"
        "shadow/internal/middleware"
        "shadow/internal/session"
        "strconv"
        "strings"
        "time"
)

type ChatFriendJSON struct {
        Username    string `json:"username"`
        DisplayName string `json:"display_name"`
        AvatarURL   string `json:"avatar_url"`
        LastMsg     string `json:"last_msg"`
        LastMsgTime string `json:"last_msg_time"`
        Unread      int    `json:"unread"`
}

type ChatRequestJSON struct {
        Username    string `json:"username"`
        DisplayName string `json:"display_name"`
        AvatarURL   string `json:"avatar_url"`
}

type ChatMessageJSON struct {
        ID             int    `json:"id"`
        SenderID       int    `json:"sender_id"`
        Content        string `json:"content"`
        CreatedAt      string `json:"created_at"`
        IsMine         bool   `json:"is_mine"`
        IsRead         bool   `json:"is_read"`
        IsDeleted      bool   `json:"is_deleted"`
        IsEdited       bool   `json:"is_edited"`
        ReplyToID      int    `json:"reply_to_id,omitempty"`
        ReplyToContent string `json:"reply_to_content,omitempty"`
        ReplyToIsMine  bool   `json:"reply_to_is_mine,omitempty"`
}

type ChatMessagesResp struct {
        Messages  []ChatMessageJSON `json:"messages"`
        HasMore   bool              `json:"has_more"`
        ReadUpTo  int               `json:"read_up_to"`
}

type ChatMsgRequestJSON struct {
        FromUsername    string `json:"from_username"`
        FromDisplayName string `json:"from_display_name"`
        FromAvatarURL   string `json:"from_avatar_url"`
        InitialMessage  string `json:"initial_message"`
}

type ChatPageData struct {
        Session         *session.Data
        CSRFToken       string
        NavInitials     string
        NavAvatarURL    string
        Friends         []ChatFriendJSON
        PendingCount    int
        MsgRequestCount int
        OpenWith        string
        OpenUser        *db.User
        OpenAvatarURL   string
}

func ChatPageHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Redirect(w, r, "/login", http.StatusSeeOther)
                return
        }

        csrfToken := middleware.GetOrCreateCSRFToken(w, r)
        navInitials, navAvatarURL := navFieldsForViewer(sessData)

        convs, _ := db.GetAllConversations(sessData.UserID)
        pending, _ := db.GetPendingRequests(sessData.UserID)
        msgReqCount, _ := db.GetMessageRequestCount(sessData.UserID)

        friendsJSON := make([]ChatFriendJSON, 0, len(convs))
        for _, f := range convs {
                last, _ := db.GetLastMessage(sessData.UserID, f.OtherUserID)
                unread, _ := db.GetUnreadCountFrom(sessData.UserID, f.OtherUserID)
                lastMsg := ""
                lastMsgTime := ""
                if last != nil {
                        if last.IsDeleted {
                                lastMsg = "Message deleted"
                        } else {
                                lastMsg = last.Content
                                if len(lastMsg) > 40 {
                                        lastMsg = lastMsg[:40] + "…"
                                }
                        }
                        lastMsgTime = formatMsgTime(last.CreatedAt)
                }
                dn := f.OtherDisplayName
                if dn == "" {
                        dn = f.OtherUsername
                }
                friendsJSON = append(friendsJSON, ChatFriendJSON{
                        Username:    f.OtherUsername,
                        DisplayName: dn,
                        AvatarURL:   assetURL(f.OtherAvatarPath),
                        LastMsg:     lastMsg,
                        LastMsgTime: lastMsgTime,
                        Unread:      unread,
                })
        }

        openWith := r.URL.Query().Get("with")
        var openUser *db.User
        var openAvatarURL string
        if openWith != "" {
                openUser, _ = db.GetUserByUsername(openWith)
                if openUser != nil {
                        openAvatarURL = assetURL(openUser.AvatarPath)
                        // allow opening chat with anyone (non-friends get a pending state in JS)
                } else {
                        openWith = ""
                }
        }

        data := ChatPageData{
                Session:         sessData,
                CSRFToken:       csrfToken,
                NavInitials:     navInitials,
                NavAvatarURL:    navAvatarURL,
                Friends:         friendsJSON,
                PendingCount:    len(pending),
                MsgRequestCount: msgReqCount,
                OpenWith:        openWith,
                OpenUser:        openUser,
                OpenAvatarURL:   openAvatarURL,
        }

        tmpl, err := template.New("chat").ParseFiles(filepath.Join("templates", "chat.html"))
        if err != nil {
                log.Printf("chat template error: %v", err)
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := tmpl.ExecuteTemplate(w, "chat", data); err != nil {
                log.Printf("chat render error: %v", err)
        }
}

func APIChatFriendsHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        convs, _ := db.GetAllConversations(sessData.UserID)
        result := make([]ChatFriendJSON, 0, len(convs))
        for _, f := range convs {
                last, _ := db.GetLastMessage(sessData.UserID, f.OtherUserID)
                unread, _ := db.GetUnreadCountFrom(sessData.UserID, f.OtherUserID)
                lastMsg := ""
                lastMsgTime := ""
                if last != nil {
                        if last.IsDeleted {
                                lastMsg = "Message deleted"
                        } else {
                                lastMsg = last.Content
                                if len(lastMsg) > 40 {
                                        lastMsg = lastMsg[:40] + "…"
                                }
                        }
                        lastMsgTime = formatMsgTime(last.CreatedAt)
                }
                dn := f.OtherDisplayName
                if dn == "" {
                        dn = f.OtherUsername
                }
                result = append(result, ChatFriendJSON{
                        Username:    f.OtherUsername,
                        DisplayName: dn,
                        AvatarURL:   assetURL(f.OtherAvatarPath),
                        LastMsg:     lastMsg,
                        LastMsgTime: lastMsgTime,
                        Unread:      unread,
                })
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
}

func APIChatRequestsHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        reqs, _ := db.GetPendingRequests(sessData.UserID)
        result := make([]ChatRequestJSON, 0, len(reqs))
        for _, req := range reqs {
                dn := req.OtherDisplayName
                if dn == "" {
                        dn = req.OtherUsername
                }
                result = append(result, ChatRequestJSON{
                        Username:    req.OtherUsername,
                        DisplayName: dn,
                        AvatarURL:   assetURL(req.OtherAvatarPath),
                })
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
}

func APIChatMessagesHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        withUsername := r.URL.Query().Get("with")
        if withUsername == "" {
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(ChatMessagesResp{Messages: []ChatMessageJSON{}})
                return
        }

        otherUser, err := db.GetUserByUsername(withUsername)
        if err != nil || otherUser == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        if !db.CanChat(sessData.UserID, otherUser.ID) {
                http.Error(w, "Cannot chat with this user", http.StatusForbidden)
                return
        }

        beforeID, _ := strconv.Atoi(r.URL.Query().Get("before_id"))
        afterID, _ := strconv.Atoi(r.URL.Query().Get("after_id"))

        readUpTo := db.GetReadUpTo(sessData.UserID, otherUser.ID)

        var msgs []db.Message
        hasMore := false

        if afterID > 0 {
                msgs, err = db.GetMessagesAfter(sessData.UserID, otherUser.ID, afterID)
        } else {
                msgs, hasMore, err = db.GetMessagesPaginated(sessData.UserID, otherUser.ID, beforeID, 30)
        }
        if err != nil {
                log.Printf("get messages: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        result := make([]ChatMessageJSON, 0, len(msgs))
        for _, m := range msgs {
                replyToID := 0
                replyToIsMine := false
                if m.ReplyToID != nil {
                        replyToID = *m.ReplyToID
                        replyToIsMine = m.ReplyToSenderID == sessData.UserID
                }
                result = append(result, ChatMessageJSON{
                        ID:             m.ID,
                        SenderID:       m.SenderID,
                        Content:        m.Content,
                        CreatedAt:      formatMsgTimeFull(m.CreatedAt),
                        IsMine:         m.SenderID == sessData.UserID,
                        IsRead:         m.ReadAt != nil,
                        IsDeleted:      m.IsDeleted,
                        IsEdited:       m.EditedAt != nil,
                        ReplyToID:      replyToID,
                        ReplyToContent: m.ReplyToContent,
                        ReplyToIsMine:  replyToIsMine,
                })
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(ChatMessagesResp{
                Messages: result,
                HasMore:  hasMore,
                ReadUpTo: readUpTo,
        })
}

func APIChatSendMessageHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        toUsername := strings.TrimSpace(r.FormValue("to"))
        content := strings.TrimSpace(r.FormValue("content"))

        if toUsername == "" || content == "" {
                http.Error(w, "Missing fields", http.StatusBadRequest)
                return
        }
        if len(content) > 2000 {
                content = content[:2000]
        }

        otherUser, err := db.GetUserByUsername(toUsername)
        if err != nil || otherUser == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        canChat := db.CanChat(sessData.UserID, otherUser.ID)
        areFriends := db.AreFriends(sessData.UserID, otherUser.ID)
        receiverHasDeleted := db.HasHiddenConversation(otherUser.ID, sessData.UserID)

        // Accept/decline request only for non-friends who haven't chatted before,
        // or non-friends where the receiver deleted the conversation.
        if !canChat || (receiverHasDeleted && !areFriends) {
                if err := db.CreateMessageRequest(sessData.UserID, otherUser.ID, content); err != nil {
                        log.Printf("create message request: %v", err)
                        http.Error(w, "Server error", http.StatusInternalServerError)
                        return
                }
                db.UnhideConversation(sessData.UserID, otherUser.ID)
                w.Header().Set("Content-Type", "application/json")
                w.Write([]byte(`{"ok":true,"type":"request"}`))
                return
        }

        // Friends (or prior chat partner): send directly.
        // Unhide sender's side always; unhide receiver's side too if they had deleted it.
        db.UnhideConversation(sessData.UserID, otherUser.ID)
        if receiverHasDeleted {
                db.UnhideConversation(otherUser.ID, sessData.UserID)
        }

        replyToID, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("reply_to_id")))
        if err := db.SendMessage(sessData.UserID, otherUser.ID, content, replyToID); err != nil {
                log.Printf("send message: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true,"type":"message"}`))
}

func APIChatMessageEditHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        msgID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("id")))
        if err != nil || msgID <= 0 {
                http.Error(w, "Invalid message id", http.StatusBadRequest)
                return
        }
        content := strings.TrimSpace(r.FormValue("content"))
        if content == "" {
                http.Error(w, "Content required", http.StatusBadRequest)
                return
        }
        if len(content) > 2000 {
                content = content[:2000]
        }

        if err := db.EditMessage(msgID, sessData.UserID, content); err != nil {
                log.Printf("edit message: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func APIChatMessageDeleteHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        msgID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("id")))
        if err != nil || msgID <= 0 {
                http.Error(w, "Invalid message id", http.StatusBadRequest)
                return
        }

        if err := db.DeleteMessage(msgID, sessData.UserID); err != nil {
                log.Printf("delete message: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func APIChatFriendSendHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        targetUsername := strings.TrimSpace(r.FormValue("username"))
        if targetUsername == "" {
                http.Error(w, "Missing username", http.StatusBadRequest)
                return
        }
        if targetUsername == sessData.Username {
                http.Error(w, "Cannot add yourself", http.StatusBadRequest)
                return
        }

        target, err := db.GetUserByUsername(targetUsername)
        if err != nil || target == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        if err := db.SendFriendRequest(sessData.UserID, target.ID); err != nil {
                log.Printf("send friend request: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func APIChatFriendSentHandler(w http.ResponseWriter, r *http.Request) {
	_, sessData, _ := session.FromRequest(r)
	if sessData == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	reqs, _ := db.GetOutgoingFriendRequests(sessData.UserID)
	type item struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	out := make([]item, 0)
	for _, r := range reqs {
		dn := r.OtherDisplayName
		if dn == "" {
			dn = r.OtherUsername
		}
		out = append(out, item{
			Username:    r.OtherUsername,
			DisplayName: dn,
			AvatarURL:   assetURL(r.OtherAvatarPath),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func APIChatFriendAcceptHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        fromUsername := strings.TrimSpace(r.FormValue("username"))
        if fromUsername == "" {
                http.Error(w, "Missing username", http.StatusBadRequest)
                return
        }

        fromUser, err := db.GetUserByUsername(fromUsername)
        if err != nil || fromUser == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        if err := db.AcceptFriendRequest(sessData.UserID, fromUser.ID); err != nil {
                log.Printf("accept friend: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func APIChatFriendRejectHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        otherUsername := strings.TrimSpace(r.FormValue("username"))
        if otherUsername == "" {
                http.Error(w, "Missing username", http.StatusBadRequest)
                return
        }

        otherUser, err := db.GetUserByUsername(otherUsername)
        if err != nil || otherUser == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        if err := db.RejectFriendRequest(sessData.UserID, otherUser.ID); err != nil {
                log.Printf("reject friend: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func APIChatSearchHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        q := strings.TrimSpace(r.URL.Query().Get("q"))
        if q == "" {
                w.Header().Set("Content-Type", "application/json")
                w.Write([]byte("[]"))
                return
        }

        users, err := db.SearchUsersForChat(q, 8)
        if err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        type UserResult struct {
                Username    string `json:"username"`
                DisplayName string `json:"display_name"`
                AvatarURL   string `json:"avatar_url"`
                Status      string `json:"status"`
        }

        result := make([]UserResult, 0)
        for _, u := range users {
                if u.ID == sessData.UserID {
                        continue
                }
                dn := u.DisplayName
                if dn == "" {
                        dn = u.Username
                }
                status, _ := db.FriendshipStatus(sessData.UserID, u.ID)
                result = append(result, UserResult{
                        Username:    u.Username,
                        DisplayName: dn,
                        AvatarURL:   assetURL(u.AvatarPath),
                        Status:      status,
                })
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
}

/* ── API: user online status ── */
func APIChatUserStatusHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
        }
        username := strings.TrimSpace(r.URL.Query().Get("username"))
        if username == "" {
                http.Error(w, "missing username", http.StatusBadRequest)
                return
        }
        user, err := db.GetUserByUsername(username)
        if err != nil || user == nil {
                http.Error(w, "not found", http.StatusNotFound)
                return
        }

        type statusResp struct {
                Status string `json:"status"`
                Text   string `json:"text"`
        }

        var status, text string
        if user.LastSeenAt == nil {
                status = "offline"
                text = "never been online"
        } else {
                diff := time.Since(*user.LastSeenAt)
                onlineSec := db.GetSiteSettingInt("online_threshold_seconds", 30)

                if !user.ShowOnlineStatus {
                        status = "hidden"
                        text = "a while ago"
                } else if diff < time.Duration(onlineSec)*time.Second {
                        // actively online
                        status = "online"
                        text = "online"
                } else if diff < 2*time.Minute {
                        // active recently window: 30s – 2min
                        status = "idle"
                        text = "active recently"
                } else if diff < time.Hour {
                        // offline since X minutes
                        mins := int(diff.Minutes())
                        if mins == 1 {
                                text = "offline since 1 minute"
                        } else {
                                text = "offline since " + strconv.Itoa(mins) + " minutes"
                        }
                        status = "offline"
                } else if diff < 2*time.Hour {
                        text = "offline since an hour"
                        status = "offline"
                } else if diff < 24*time.Hour {
                        hours := int(diff.Hours())
                        text = "offline since " + strconv.Itoa(hours) + " hours"
                        status = "offline"
                } else if diff < 48*time.Hour {
                        text = "last seen yesterday"
                        status = "offline"
                } else {
                        text = "last seen " + user.LastSeenAt.Local().Format("02/01/2006")
                        status = "offline"
                }
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(statusResp{Status: status, Text: text})
}

// ── MESSAGE REQUESTS ─────────────────────────────────────────────────────────

// APIChatFriendsListHandler → GET /api/chat/friends/list
// Returns actual friends from the friendships table (ignores hidden conversations).
// Used by the Friends modal so hidden chats don't remove people from the list.
func APIChatFriendsListHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        friends, err := db.GetFriends(sessData.UserID)
        if err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        type FriendItem struct {
                Username    string `json:"username"`
                DisplayName string `json:"display_name"`
                AvatarURL   string `json:"avatar_url"`
        }

        result := make([]FriendItem, 0, len(friends))
        for _, f := range friends {
                dn := f.OtherDisplayName
                if dn == "" {
                        dn = f.OtherUsername
                }
                result = append(result, FriendItem{
                        Username:    f.OtherUsername,
                        DisplayName: dn,
                        AvatarURL:   assetURL(f.OtherAvatarPath),
                })
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
}

// APIChatMsgRequestsCountHandler → GET /api/chat/msg-requests/count
// Returns the count of pending inbound message requests (lightweight poll endpoint).
func APIChatMsgRequestsCountHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        count, _ := db.GetMessageRequestCount(sessData.UserID)
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprintf(w, `{"count":%d}`, count)
}

// APIChatMsgRequestsHandler → GET /api/chat/msg-requests
// Returns pending inbound message requests for the current user.
func APIChatMsgRequestsHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        reqs, err := db.GetMessageRequests(sessData.UserID)
        if err != nil {
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        type reqJSON struct {
                FromUsername    string `json:"from_username"`
                FromDisplayName string `json:"from_display_name"`
                FromAvatarURL   string `json:"from_avatar_url"`
                InitialMessage  string `json:"initial_message"`
        }
        result := make([]reqJSON, 0, len(reqs))
        for _, rr := range reqs {
                dn := rr.FromDisplayName
                if dn == "" {
                        dn = rr.FromUsername
                }
                result = append(result, reqJSON{
                        FromUsername:    rr.FromUsername,
                        FromDisplayName: dn,
                        FromAvatarURL:   assetURL(rr.FromAvatarPath),
                        InitialMessage:  rr.InitialMessage,
                })
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
}

// APIChatAcceptMsgRequestHandler → POST /api/chat/msg-request/accept
func APIChatAcceptMsgRequestHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        fromUsername := strings.TrimSpace(r.FormValue("from"))
        if fromUsername == "" {
                http.Error(w, "Missing 'from'", http.StatusBadRequest)
                return
        }

        fromUser, err := db.GetUserByUsername(fromUsername)
        if err != nil || fromUser == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        if err := db.AcceptMessageRequest(fromUser.ID, sessData.UserID); err != nil {
                log.Printf("accept msg request: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

// APIChatDeclineMsgRequestHandler → POST /api/chat/msg-request/decline
func APIChatDeclineMsgRequestHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        fromUsername := strings.TrimSpace(r.FormValue("from"))
        if fromUsername == "" {
                http.Error(w, "Missing 'from'", http.StatusBadRequest)
                return
        }

        fromUser, err := db.GetUserByUsername(fromUsername)
        if err != nil || fromUser == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        if err := db.DeclineMessageRequest(fromUser.ID, sessData.UserID); err != nil {
                log.Printf("decline msg request: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

// APIChatHideConvHandler → POST /api/chat/conversation/hide
// Hides a conversation from the user's sidebar (messages stay in DB for other user).
func APIChatHideConvHandler(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }

        r.ParseForm()
        withUsername := strings.TrimSpace(r.FormValue("with"))
        if withUsername == "" {
                http.Error(w, "Missing 'with'", http.StatusBadRequest)
                return
        }

        otherUser, err := db.GetUserByUsername(withUsername)
        if err != nil || otherUser == nil {
                http.Error(w, "User not found", http.StatusNotFound)
                return
        }

        if err := db.HideConversation(sessData.UserID, otherUser.ID); err != nil {
                log.Printf("hide conversation: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"ok":true}`))
}

func formatMsgTime(t time.Time) string {
        now := time.Now()
        diff := now.Sub(t)
        if diff < 24*time.Hour && now.Day() == t.Day() {
                return t.Format("15:04")
        }
        if diff < 7*24*time.Hour {
                return t.Format("Mon")
        }
        return t.Format("02/01")
}

func formatMsgTimeFull(t time.Time) string {
        return t.Format("2006-01-02 15:04:05")
}

func APIChatNotificationsPollHandler(w http.ResponseWriter, r *http.Request) {
        _, sessData, _ := session.FromRequest(r)
        if sessData == nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
        }
        afterID, _ := strconv.Atoi(r.URL.Query().Get("after_id"))
        items, err := db.GetRecentUnreadNotifications(sessData.UserID, afterID)
        if err != nil {
                log.Printf("notifications poll: %v", err)
                http.Error(w, "Server error", http.StatusInternalServerError)
                return
        }
        if items == nil {
                items = []db.NotificationItem{}
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{"notifications": items})
}
