package db

import (
        "database/sql"
        "time"
)

type Friendship struct {
        ID               int
        RequesterID      int
        AddresseeID      int
        Status           string
        CreatedAt        time.Time
        OtherUserID      int
        OtherUsername    string
        OtherDisplayName string
        OtherAvatarPath  string
}

type Message struct {
        ID              int
        SenderID        int
        ReceiverID      int
        Content         string
        CreatedAt       time.Time
        ReadAt          *time.Time
        IsDeleted       bool
        EditedAt        *time.Time
        ReplyToID       *int
        ReplyToContent  string
        ReplyToSenderID int
}

func FriendshipStatus(userID, otherID int) (string, error) {
        var requesterID int
        var status string
        err := DB.QueryRow(`
                SELECT requester_id, status FROM friendships
                WHERE (requester_id=$1 AND addressee_id=$2) OR (requester_id=$2 AND addressee_id=$1)
        `, userID, otherID).Scan(&requesterID, &status)
        if err == sql.ErrNoRows {
                return "none", nil
        }
        if err != nil {
                return "none", err
        }
        if status == "accepted" {
                return "accepted", nil
        }
        if status == "pending" {
                if requesterID == userID {
                        return "pending_sent", nil
                }
                return "pending_received", nil
        }
        return "none", nil
}

func SendFriendRequest(userID, targetID int) error {
        _, err := DB.Exec(`
                INSERT INTO friendships (requester_id, addressee_id, status)
                VALUES ($1, $2, 'pending')
                ON CONFLICT (requester_id, addressee_id) DO NOTHING
        `, userID, targetID)
        return err
}

func AcceptFriendRequest(userID, requesterID int) error {
        _, err := DB.Exec(`
                UPDATE friendships SET status='accepted'
                WHERE requester_id=$1 AND addressee_id=$2 AND status='pending'
        `, requesterID, userID)
        return err
}

func RejectFriendRequest(userID, otherID int) error {
        _, err := DB.Exec(`
                DELETE FROM friendships
                WHERE (requester_id=$1 AND addressee_id=$2) OR (requester_id=$2 AND addressee_id=$1)
        `, userID, otherID)
        return err
}

func GetFriends(userID int) ([]Friendship, error) {
        rows, err := DB.Query(`
                SELECT
                        f.id, f.requester_id, f.addressee_id, f.status, f.created_at,
                        u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.avatar_path,'')
                FROM friendships f
                JOIN users u ON (
                        CASE WHEN f.requester_id=$1 THEN f.addressee_id ELSE f.requester_id END = u.id
                )
                WHERE (f.requester_id=$1 OR f.addressee_id=$1) AND f.status='accepted'
                        AND u.deleted_at IS NULL
                ORDER BY f.created_at DESC
        `, userID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var friends []Friendship
        for rows.Next() {
                var f Friendship
                if err := rows.Scan(&f.ID, &f.RequesterID, &f.AddresseeID, &f.Status, &f.CreatedAt,
                        &f.OtherUserID, &f.OtherUsername, &f.OtherDisplayName, &f.OtherAvatarPath); err != nil {
                        continue
                }
                friends = append(friends, f)
        }
        return friends, nil
}

func GetPendingRequests(userID int) ([]Friendship, error) {
        rows, err := DB.Query(`
                SELECT
                        f.id, f.requester_id, f.addressee_id, f.status, f.created_at,
                        u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.avatar_path,'')
                FROM friendships f
                JOIN users u ON f.requester_id = u.id
                WHERE f.addressee_id=$1 AND f.status='pending'
                        AND u.deleted_at IS NULL
                ORDER BY f.created_at DESC
        `, userID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var reqs []Friendship
        for rows.Next() {
                var f Friendship
                if err := rows.Scan(&f.ID, &f.RequesterID, &f.AddresseeID, &f.Status, &f.CreatedAt,
                        &f.OtherUserID, &f.OtherUsername, &f.OtherDisplayName, &f.OtherAvatarPath); err != nil {
                        continue
                }
                reqs = append(reqs, f)
        }
        return reqs, nil
}

func SendMessage(senderID, receiverID int, content string, replyToID int) error {
        var err error
        if replyToID > 0 {
                _, err = DB.Exec(`
                        INSERT INTO messages (sender_id, receiver_id, content, reply_to_id)
                        VALUES ($1, $2, $3, $4)
                `, senderID, receiverID, content, replyToID)
        } else {
                _, err = DB.Exec(`
                        INSERT INTO messages (sender_id, receiver_id, content)
                        VALUES ($1, $2, $3)
                `, senderID, receiverID, content)
        }
        return err
}

// msgSelectFrom is the SELECT…FROM clause (with LEFT JOIN for reply context).
// Append a WHERE + ORDER BY + LIMIT clause to complete the query.
const msgSelectFrom = `
SELECT m.id, m.sender_id, m.receiver_id, m.content, m.created_at, m.read_at,
       COALESCE(m.is_deleted, false), m.edited_at,
       m.reply_to_id,
       COALESCE(CASE WHEN COALESCE(r.is_deleted,false) THEN '' ELSE r.content END, ''),
       COALESCE(r.sender_id, 0)
FROM messages m
LEFT JOIN messages r ON r.id = m.reply_to_id
`

func scanMessage(rows *sql.Rows) (Message, error) {
        var m Message
        err := rows.Scan(&m.ID, &m.SenderID, &m.ReceiverID, &m.Content, &m.CreatedAt, &m.ReadAt,
                &m.IsDeleted, &m.EditedAt, &m.ReplyToID, &m.ReplyToContent, &m.ReplyToSenderID)
        return m, err
}

// GetMessagesPaginated returns up to `limit` messages ending before beforeID (0 = latest).
// Messages are returned in ascending order (oldest first).
// hasMore=true means there are older messages beyond what was returned.
func GetMessagesPaginated(userID, otherID, beforeID, limit int) ([]Message, bool, error) {
        fetch := limit + 1
        var rows *sql.Rows
        var err error
        if beforeID > 0 {
                rows, err = DB.Query(msgSelectFrom+`
                        WHERE ((m.sender_id=$1 AND m.receiver_id=$2) OR (m.sender_id=$2 AND m.receiver_id=$1))
                          AND m.id < $3
                        ORDER BY m.created_at DESC, m.id DESC
                        LIMIT $4
                `, userID, otherID, beforeID, fetch)
        } else {
                rows, err = DB.Query(msgSelectFrom+`
                        WHERE (m.sender_id=$1 AND m.receiver_id=$2) OR (m.sender_id=$2 AND m.receiver_id=$1)
                        ORDER BY m.created_at DESC, m.id DESC
                        LIMIT $3
                `, userID, otherID, fetch)
        }
        if err != nil {
                return nil, false, err
        }
        defer rows.Close()
        var msgs []Message
        for rows.Next() {
                m, err := scanMessage(rows)
                if err != nil {
                        continue
                }
                msgs = append(msgs, m)
        }
        hasMore := len(msgs) > limit
        if hasMore {
                msgs = msgs[:limit]
        }
        // reverse to ascending order
        for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
                msgs[i], msgs[j] = msgs[j], msgs[i]
        }
        // mark received messages as read
        DB.Exec(`UPDATE messages SET read_at=NOW() WHERE sender_id=$1 AND receiver_id=$2 AND read_at IS NULL`, otherID, userID)
        return msgs, hasMore, nil
}

// GetMessagesAfter returns messages with id > afterID in ascending order (for polling).
// It also returns any messages that were recently edited/deleted (within last 90s)
// so the other person's UI updates instantly without a full reload.
func GetMessagesAfter(userID, otherID, afterID int) ([]Message, error) {
        rows, err := DB.Query(msgSelectFrom+`
                WHERE ((m.sender_id=$1 AND m.receiver_id=$2) OR (m.sender_id=$2 AND m.receiver_id=$1))
                  AND (
                    m.id > $3
                    OR (m.edited_at IS NOT NULL AND m.edited_at > NOW() - INTERVAL '90 seconds')
                  )
                ORDER BY m.created_at ASC, m.id ASC
                LIMIT 150
        `, userID, otherID, afterID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var msgs []Message
        for rows.Next() {
                m, err := scanMessage(rows)
                if err != nil {
                        continue
                }
                msgs = append(msgs, m)
        }
        // mark received messages as read
        DB.Exec(`UPDATE messages SET read_at=NOW() WHERE sender_id=$1 AND receiver_id=$2 AND read_at IS NULL`, otherID, userID)
        return msgs, nil
}

// AreFriends returns true if the two users have an accepted friendship.
func AreFriends(userID, otherID int) bool {
        var exists bool
        DB.QueryRow(`
                SELECT EXISTS(
                        SELECT 1 FROM friendships
                        WHERE ((requester_id=$1 AND addressee_id=$2) OR (requester_id=$2 AND addressee_id=$1))
                          AND status='accepted'
                )
        `, userID, otherID).Scan(&exists)
        return exists
}

// GetReadUpTo returns the max ID of messages sent by senderID to receiverID that have been read.
func GetReadUpTo(senderID, receiverID int) int {
        var id int
        DB.QueryRow(`
                SELECT COALESCE(MAX(id), 0) FROM messages
                WHERE sender_id=$1 AND receiver_id=$2 AND read_at IS NOT NULL
        `, senderID, receiverID).Scan(&id)
        return id
}

// EditMessage updates the content of a message (only sender can edit, not deleted).
func EditMessage(msgID, senderID int, newContent string) error {
        _, err := DB.Exec(`
                UPDATE messages SET content=$1, edited_at=NOW()
                WHERE id=$2 AND sender_id=$3 AND COALESCE(is_deleted, false)=false
        `, newContent, msgID, senderID)
        return err
}

// DeleteMessage soft-deletes a message (only sender can delete).
// Sets edited_at=NOW() so polls can pick up the change.
func DeleteMessage(msgID, senderID int) error {
        _, err := DB.Exec(`
                UPDATE messages SET is_deleted=true, content='', edited_at=NOW()
                WHERE id=$1 AND sender_id=$2
        `, msgID, senderID)
        return err
}

func GetLastMessage(userID, otherID int) (*Message, error) {
        row := DB.QueryRow(`
                SELECT id, sender_id, receiver_id, content, created_at, read_at,
                       COALESCE(is_deleted, false), edited_at
                FROM messages
                WHERE (sender_id=$1 AND receiver_id=$2) OR (sender_id=$2 AND receiver_id=$1)
                ORDER BY created_at DESC, id DESC LIMIT 1
        `, userID, otherID)
        var m Message
        err := row.Scan(&m.ID, &m.SenderID, &m.ReceiverID, &m.Content, &m.CreatedAt, &m.ReadAt, &m.IsDeleted, &m.EditedAt)
        if err == sql.ErrNoRows {
                return nil, nil
        }
        return &m, err
}

func GetUnreadCount(userID int) (int, error) {
        var count int
        err := DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE receiver_id=$1 AND read_at IS NULL`, userID).Scan(&count)
        return count, err
}

func GetUnreadCountFrom(userID, senderID int) (int, error) {
        var count int
        err := DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE receiver_id=$1 AND sender_id=$2 AND read_at IS NULL`, userID, senderID).Scan(&count)
        return count, err
}

// ── MESSAGE REQUESTS ──────────────────────────────────────────────────────────

type MessageRequest struct {
        ID              int
        FromID          int
        ToID            int
        FromUsername    string
        FromDisplayName string
        FromAvatarPath  string
        InitialMessage  string
        Status          string
        CreatedAt       time.Time
}

// CanChat returns true if the two users are allowed to exchange messages:
// either they are friends, have an accepted message-request, or already
// have messages between them (e.g. after unfriending).
func CanChat(userID, otherID int) bool {
        var exists bool
        DB.QueryRow(`
                SELECT EXISTS(
                        SELECT 1 FROM friendships
                        WHERE ((requester_id=$1 AND addressee_id=$2) OR (requester_id=$2 AND addressee_id=$1))
                          AND status='accepted'
                        UNION ALL
                        SELECT 1 FROM message_requests
                        WHERE ((from_id=$1 AND to_id=$2) OR (from_id=$2 AND to_id=$1))
                          AND status='accepted'
                        UNION ALL
                        SELECT 1 FROM messages
                        WHERE (sender_id=$1 AND receiver_id=$2) OR (sender_id=$2 AND receiver_id=$1)
                        LIMIT 1
                )
        `, userID, otherID).Scan(&exists)
        return exists
}

// CreateMessageRequest sends a DM request from a non-friend.
// If a prior request from the same person exists, updates it.
func CreateMessageRequest(fromID, toID int, message string) error {
        _, err := DB.Exec(`
                INSERT INTO message_requests (from_id, to_id, initial_message, status, created_at)
                VALUES ($1, $2, $3, 'pending', NOW())
                ON CONFLICT (from_id, to_id)
                DO UPDATE SET initial_message=EXCLUDED.initial_message,
                              status='pending',
                              created_at=NOW()
        `, fromID, toID, message)
        return err
}

// GetMessageRequests returns pending inbound requests for toID.
func GetMessageRequests(toID int) ([]MessageRequest, error) {
        rows, err := DB.Query(`
                SELECT mr.id, mr.from_id, mr.to_id,
                       u.username, COALESCE(u.display_name,''), COALESCE(u.avatar_path,''),
                       mr.initial_message, mr.status, mr.created_at
                FROM message_requests mr
                JOIN users u ON u.id = mr.from_id
                WHERE mr.to_id=$1 AND mr.status='pending'
                  AND u.deleted_at IS NULL
                ORDER BY mr.created_at DESC
        `, toID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var reqs []MessageRequest
        for rows.Next() {
                var r MessageRequest
                if err := rows.Scan(&r.ID, &r.FromID, &r.ToID,
                        &r.FromUsername, &r.FromDisplayName, &r.FromAvatarPath,
                        &r.InitialMessage, &r.Status, &r.CreatedAt); err != nil {
                        continue
                }
                reqs = append(reqs, r)
        }
        return reqs, nil
}

// GetMessageRequestCount returns pending inbound request count.
func GetMessageRequestCount(toID int) (int, error) {
        var n int
        err := DB.QueryRow(`SELECT COUNT(*) FROM message_requests WHERE to_id=$1 AND status='pending'`, toID).Scan(&n)
        return n, err
}

// AcceptMessageRequest marks a request accepted and stores the initial message.
func AcceptMessageRequest(fromID, toID int) error {
        var initMsg string
        err := DB.QueryRow(`
                UPDATE message_requests SET status='accepted'
                WHERE from_id=$1 AND to_id=$2 AND status='pending'
                RETURNING initial_message
        `, fromID, toID).Scan(&initMsg)
        if err != nil {
                return err
        }
        // deliver the initial message
        if initMsg != "" {
                _, err = DB.Exec(`INSERT INTO messages (sender_id, receiver_id, content) VALUES ($1,$2,$3)`,
                        fromID, toID, initMsg)
                _ = err
        }
        // unhide conversation for the acceptor in case they had hidden it
        UnhideConversation(toID, fromID)
        return err
}

// DeclineMessageRequest marks a request declined.
func DeclineMessageRequest(fromID, toID int) error {
        _, err := DB.Exec(`UPDATE message_requests SET status='declined' WHERE from_id=$1 AND to_id=$2`, fromID, toID)
        return err
}

// ── CONVERSATION VISIBILITY ───────────────────────────────────────────────────

// HideConversation hides a conversation from userID's sidebar.
func HideConversation(userID, otherID int) error {
        _, err := DB.Exec(`
                INSERT INTO hidden_conversations (user_id, other_user_id)
                VALUES ($1,$2) ON CONFLICT DO NOTHING
        `, userID, otherID)
        return err
}

// UnhideConversation re-shows a hidden conversation (called when user sends a new message).
func UnhideConversation(userID, otherID int) {
        DB.Exec(`DELETE FROM hidden_conversations WHERE user_id=$1 AND other_user_id=$2`, userID, otherID)
}

// HasHiddenConversation returns true if userID has hidden their chat with otherID.
func HasHiddenConversation(userID, otherID int) bool {
        var exists bool
        DB.QueryRow(`
                SELECT EXISTS(
                        SELECT 1 FROM hidden_conversations
                        WHERE user_id=$1 AND other_user_id=$2
                )
        `, userID, otherID).Scan(&exists)
        return exists
}

// ── GET ALL CONVERSATIONS ─────────────────────────────────────────────────────

// GetAllConversations returns every conversation partner for userID:
//   - current friends (even without messages)
//   - non-friends with whom messages have been exchanged
//   - non-friends whose accepted message-request exists
//
// Hidden conversations are excluded.
func GetAllConversations(userID int) ([]Friendship, error) {
        rows, err := DB.Query(`
                SELECT DISTINCT u.id,
                       u.username,
                       COALESCE(u.display_name,''),
                       COALESCE(u.avatar_path,'')
                FROM (
                        -- friends
                        SELECT CASE WHEN requester_id=$1 THEN addressee_id ELSE requester_id END AS uid
                        FROM friendships
                        WHERE (requester_id=$1 OR addressee_id=$1) AND status='accepted'
                        UNION
                        -- anyone we've exchanged messages with
                        SELECT CASE WHEN sender_id=$1 THEN receiver_id ELSE sender_id END AS uid
                        FROM messages WHERE sender_id=$1 OR receiver_id=$1
                        UNION
                        -- accepted message-request partners
                        SELECT CASE WHEN from_id=$1 THEN to_id ELSE from_id END AS uid
                        FROM message_requests
                        WHERE (from_id=$1 OR to_id=$1) AND status='accepted'
                ) p
                JOIN users u ON u.id = p.uid
                WHERE u.deleted_at IS NULL AND COALESCE(u.banned,false)=false
                  AND NOT EXISTS(
                      SELECT 1 FROM hidden_conversations hc
                      WHERE hc.user_id=$1 AND hc.other_user_id=u.id
                  )
        `, userID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var list []Friendship
        for rows.Next() {
                var f Friendship
                if err := rows.Scan(&f.OtherUserID, &f.OtherUsername, &f.OtherDisplayName, &f.OtherAvatarPath); err != nil {
                        continue
                }
                list = append(list, f)
        }
        return list, nil
}

func SearchUsersForChat(query string, limit int) ([]User, error) {
        rows, err := DB.Query(`
                SELECT id, username, COALESCE(display_name,''), COALESCE(email,''), COALESCE(bio,''), COALESCE(avatar_path,''), COALESCE(banner_path,''), created_at
                FROM users
                WHERE username ILIKE $1 AND deleted_at IS NULL AND COALESCE(banned,false)=false
                LIMIT $2
        `, "%"+query+"%", limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var users []User
        for rows.Next() {
                var u User
                if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Bio, &u.AvatarPath, &u.BannerPath, &u.CreatedAt); err != nil {
                        continue
                }
                users = append(users, u)
        }
        return users, nil
}

type NotificationItem struct {
        MsgID    int    `json:"msg_id"`
        FromUser string `json:"from"`
        Content  string `json:"content"`
}

func GetRecentUnreadNotifications(userID, afterID int) ([]NotificationItem, error) {
        rows, err := DB.Query(`
                SELECT m.id, u.username, m.content
                FROM messages m
                JOIN users u ON u.id = m.sender_id
                WHERE m.receiver_id = $1 AND m.id > $2 AND NOT m.is_deleted
                ORDER BY m.id ASC
                LIMIT 30
        `, userID, afterID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var items []NotificationItem
        for rows.Next() {
                var item NotificationItem
                if err := rows.Scan(&item.MsgID, &item.FromUser, &item.Content); err != nil {
                        continue
                }
                items = append(items, item)
        }
        return items, nil
}

func GetOutgoingFriendRequests(userID int) ([]Friendship, error) {
	rows, err := DB.Query(`
		SELECT
			f.id, f.requester_id, f.addressee_id, f.status, f.created_at,
			u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.avatar_path,'')
		FROM friendships f
		JOIN users u ON f.addressee_id = u.id
		WHERE f.requester_id=$1 AND f.status='pending'
			AND u.deleted_at IS NULL
		ORDER BY f.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reqs []Friendship
	for rows.Next() {
		var f Friendship
		if err := rows.Scan(&f.ID, &f.RequesterID, &f.AddresseeID, &f.Status, &f.CreatedAt,
			&f.OtherUserID, &f.OtherUsername, &f.OtherDisplayName, &f.OtherAvatarPath); err != nil {
			continue
		}
		reqs = append(reqs, f)
	}
	return reqs, nil
}
