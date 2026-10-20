package db

import (
	"context"
	"database/sql"
	"time"
)

type SupportTicket struct {
	ID            int
	UserID        *int
	GuestUsername string
	DisplayName   string
	Title         string
	Body          string
	Category      string
	Status        string
	ReplyCount    int
	HasAdminReply bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SupportTicketReply struct {
	ID           int
	TicketID     int
	AuthorUserID *int
	AuthorName   string
	IsAdmin      bool
	Body         string
	CreatedAt    time.Time
}

func MigrateSupport() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS support_tickets (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			guest_username TEXT DEFAULT '',
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			category TEXT DEFAULT 'other',
			status TEXT DEFAULT 'open',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_support_tickets_user ON support_tickets(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_support_tickets_status ON support_tickets(status)`,
		`CREATE TABLE IF NOT EXISTS support_ticket_replies (
			id SERIAL PRIMARY KEY,
			ticket_id INTEGER REFERENCES support_tickets(id) ON DELETE CASCADE,
			author_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			is_admin BOOLEAN DEFAULT FALSE,
			body TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ticket_replies_ticket ON support_ticket_replies(ticket_id)`,
	}
	for _, q := range queries {
		if _, err := DB.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func CreateSupportTicket(ctx context.Context, userID *int, guestUsername, title, body, category string) (int, error) {
	var id int
	err := DB.QueryRowContext(ctx,
		`INSERT INTO support_tickets (user_id, guest_username, title, body, category)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		userID, guestUsername, title, body, category,
	).Scan(&id)
	return id, err
}

func GetSupportTicket(ctx context.Context, id int) (*SupportTicket, error) {
	t := &SupportTicket{}
	var userID sql.NullInt64
	err := DB.QueryRowContext(ctx,
		`SELECT t.id, t.user_id, t.guest_username, COALESCE(u.username, t.guest_username),
			t.title, t.body, t.category, t.status, t.created_at, t.updated_at,
			(SELECT COUNT(*) FROM support_ticket_replies WHERE ticket_id = t.id),
			(SELECT COUNT(*) > 0 FROM support_ticket_replies WHERE ticket_id = t.id AND is_admin = TRUE)
		 FROM support_tickets t
		 LEFT JOIN users u ON u.id = t.user_id
		 WHERE t.id = $1`, id,
	).Scan(&t.ID, &userID, &t.GuestUsername, &t.DisplayName,
		&t.Title, &t.Body, &t.Category, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.ReplyCount, &t.HasAdminReply)
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		v := int(userID.Int64)
		t.UserID = &v
	}
	return t, nil
}

func GetMyTickets(ctx context.Context, userID int) ([]SupportTicket, error) {
	rows, err := DB.QueryContext(ctx,
		`SELECT t.id, t.user_id, t.guest_username, COALESCE(u.username, t.guest_username),
			t.title, t.body, t.category, t.status, t.created_at, t.updated_at,
			(SELECT COUNT(*) FROM support_ticket_replies WHERE ticket_id = t.id),
			(SELECT COUNT(*) > 0 FROM support_ticket_replies WHERE ticket_id = t.id AND is_admin = TRUE)
		 FROM support_tickets t
		 LEFT JOIN users u ON u.id = t.user_id
		 WHERE t.user_id = $1
		 ORDER BY t.updated_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTickets(rows)
}

func AdminGetAllTickets(ctx context.Context, status string) ([]SupportTicket, error) {
	q := `SELECT t.id, t.user_id, t.guest_username, COALESCE(u.username, t.guest_username),
		     t.title, t.body, t.category, t.status, t.created_at, t.updated_at,
		     (SELECT COUNT(*) FROM support_ticket_replies WHERE ticket_id = t.id),
		     (SELECT COUNT(*) > 0 FROM support_ticket_replies WHERE ticket_id = t.id AND is_admin = TRUE)
		  FROM support_tickets t
		  LEFT JOIN users u ON u.id = t.user_id`
	var rows *sql.Rows
	var err error
	if status != "" && status != "all" {
		q += ` WHERE t.status = $1 ORDER BY t.updated_at DESC`
		rows, err = DB.QueryContext(ctx, q, status)
	} else {
		q += ` ORDER BY t.updated_at DESC`
		rows, err = DB.QueryContext(ctx, q)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTickets(rows)
}

func AdminCountOpenTickets(ctx context.Context) int {
	var n int
	DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_tickets WHERE status='open'`).Scan(&n)
	return n
}

func scanTickets(rows *sql.Rows) ([]SupportTicket, error) {
	var out []SupportTicket
	for rows.Next() {
		var t SupportTicket
		var userID sql.NullInt64
		if err := rows.Scan(&t.ID, &userID, &t.GuestUsername, &t.DisplayName,
			&t.Title, &t.Body, &t.Category, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.ReplyCount, &t.HasAdminReply); err != nil {
			return nil, err
		}
		if userID.Valid {
			v := int(userID.Int64)
			t.UserID = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func AddTicketReply(ctx context.Context, ticketID int, authorUserID *int, isAdmin bool, body string) error {
	_, err := DB.ExecContext(ctx,
		`INSERT INTO support_ticket_replies (ticket_id, author_user_id, is_admin, body) VALUES ($1,$2,$3,$4)`,
		ticketID, authorUserID, isAdmin, body,
	)
	if err != nil {
		return err
	}
	_, err = DB.ExecContext(ctx,
		`UPDATE support_tickets SET updated_at=NOW() WHERE id=$1`, ticketID)
	return err
}

func GetTicketReplies(ctx context.Context, ticketID int) ([]SupportTicketReply, error) {
	rows, err := DB.QueryContext(ctx,
		`SELECT r.id, r.ticket_id, r.author_user_id, COALESCE(u.username,'Admin'), r.is_admin, r.body, r.created_at
		 FROM support_ticket_replies r
		 LEFT JOIN users u ON u.id = r.author_user_id
		 WHERE r.ticket_id = $1
		 ORDER BY r.created_at ASC`, ticketID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SupportTicketReply
	for rows.Next() {
		var rep SupportTicketReply
		var authorID sql.NullInt64
		if err := rows.Scan(&rep.ID, &rep.TicketID, &authorID, &rep.AuthorName, &rep.IsAdmin, &rep.Body, &rep.CreatedAt); err != nil {
			return nil, err
		}
		if authorID.Valid {
			v := int(authorID.Int64)
			rep.AuthorUserID = &v
		}
		out = append(out, rep)
	}
	return out, rows.Err()
}

func SetTicketStatus(ctx context.Context, ticketID int, status string) error {
	_, err := DB.ExecContext(ctx,
		`UPDATE support_tickets SET status=$1, updated_at=NOW() WHERE id=$2`, status, ticketID)
	return err
}
