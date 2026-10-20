package handlers

import (
        "context"
        "net/http"
        "shadow/internal/db"
        "shadow/internal/session"
        "strconv"
        "strings"
        "time"
)

func SupportHandler(w http.ResponseWriter, r *http.Request) {
        _, sess, _ := session.FromRequest(r)
        data := pageData(w, r)

        type SupportPageData struct {
                PageData
                MyTickets []db.SupportTicket
                View      string
                Error     string
                Success   string
        }

        var myTickets []db.SupportTicket
        if sess != nil {
                ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
                defer cancel()
                myTickets, _ = db.GetMyTickets(ctx, sess.UserID)
        }

        view := r.URL.Query().Get("view")
        if view == "" {
                view = "main"
        }

        if r.Method == http.MethodPost {
                if sess == nil {
                        http.Redirect(w, r, "/login", http.StatusSeeOther)
                        return
                }
                if err := r.ParseForm(); err != nil {
                        http.Error(w, "Bad Request", http.StatusBadRequest)
                        return
                }
                title := strings.TrimSpace(r.FormValue("title"))
                body := strings.TrimSpace(r.FormValue("body"))
                category := strings.TrimSpace(r.FormValue("category"))

                if title == "" || body == "" {
                        tmpl, _ := parseTemplates("support.html")
                        d := SupportPageData{PageData: data, MyTickets: myTickets, View: "create", Error: "Title and description are required."}
                        tmpl.ExecuteTemplate(w, "base", d)
                        return
                }
                if category == "" {
                        category = "other"
                }

                ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
                defer cancel()

                userIDPtr := &sess.UserID
                ticketID, err := db.CreateSupportTicket(ctx, userIDPtr, "", title, body, category)
                if err != nil {
                        tmpl, _ := parseTemplates("support.html")
                        d := SupportPageData{PageData: data, MyTickets: myTickets, View: "create", Error: "Failed to submit ticket. Please try again."}
                        tmpl.ExecuteTemplate(w, "base", d)
                        return
                }
                http.Redirect(w, r, "/support/ticket/"+strconv.Itoa(ticketID)+"?submitted=1", http.StatusSeeOther)
                return
        }

        tmpl, err := parseTemplates("support.html")
        if err != nil {
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        tmpl.ExecuteTemplate(w, "base", SupportPageData{PageData: data, MyTickets: myTickets, View: view})
}

func SupportTicketDetailHandler(w http.ResponseWriter, r *http.Request) {
        _, sess, _ := session.FromRequest(r)
        data := pageData(w, r)

        idStr := strings.TrimPrefix(r.URL.Path, "/support/ticket/")
        idStr = strings.Split(idStr, "/")[0]
        ticketID, err := strconv.Atoi(idStr)
        if err != nil {
                http.Redirect(w, r, "/support", http.StatusFound)
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        ticket, err := db.GetSupportTicket(ctx, ticketID)
        if err != nil || ticket == nil {
                http.Redirect(w, r, "/support", http.StatusFound)
                return
        }

        // Only ticket owner or admin can view — for guest tickets, no auth check (they have the link)
        if ticket.UserID != nil && (sess == nil || sess.UserID != *ticket.UserID) {
                http.Redirect(w, r, "/support", http.StatusFound)
                return
        }

        type TicketPageData struct {
                PageData
                Ticket    *db.SupportTicket
                Replies   []db.SupportTicketReply
                Submitted bool
        }

        replies, _ := db.GetTicketReplies(ctx, ticketID)

        if r.Method == http.MethodPost {
                if err := r.ParseForm(); err != nil {
                        http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
                        return
                }
                action := r.FormValue("action")

                if action == "close" && ticket.Status == "open" {
                        _ = db.SetTicketStatus(ctx, ticketID, "closed")
                        http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
                        return
                }

                if ticket.Status == "closed" {
                        http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
                        return
                }

                body := strings.TrimSpace(r.FormValue("body"))
                if body == "" {
                        http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
                        return
                }

                var authorID *int
                if sess != nil {
                        authorID = &sess.UserID
                }

                ctx2, cancel2 := context.WithTimeout(r.Context(), 5*time.Second)
                defer cancel2()
                _ = db.AddTicketReply(ctx2, ticketID, authorID, false, body)
                http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
                return
        }

        tmpl, err := parseTemplates("support-ticket.html")
        if err != nil {
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        tmpl.ExecuteTemplate(w, "base", TicketPageData{
                PageData:  data,
                Ticket:    ticket,
                Replies:   replies,
                Submitted: r.URL.Query().Get("submitted") == "1",
        })
}
