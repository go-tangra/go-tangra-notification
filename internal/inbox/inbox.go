// Package inbox is a person's own view of the messages delivered to them:
// listing with the unread count, reading (which marks read), bulk status
// changes and removal. Every operation is scoped to the caller.
package inbox

import (
	"context"
	"errors"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/messages"
	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// ErrInput is returned for bad statuses or empty id lists.
var ErrInput = errors.New("inbox: invalid input")

// Service is the inbox.
type Service struct {
	st    repo.Store
	audit *audit.Writer
}

// New wires the service.
func New(st repo.Store, aw *audit.Writer) *Service { return &Service{st: st, audit: aw} }

// MessageView is the message part of an entry.
type MessageView struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	Type         string     `json:"type"`
	CategoryName string     `json:"category_name"`
	SenderID     string     `json:"sender_id"`
	PublishedAt  *time.Time `json:"published_at"`
}

// EntryView is one inbox entry.
type EntryView struct {
	ID        string      `json:"id"`
	Message   MessageView `json:"message"`
	Status    string      `json:"status"`
	ReadAt    *time.Time  `json:"read_at"`
	CreatedAt time.Time   `json:"created_at"`
}

func view(r store.InboxRow) EntryView {
	v := EntryView{ID: r.ID, Status: r.Status, ReadAt: r.ReadAt, CreatedAt: r.CreatedAt}
	if r.Message != nil {
		sender := ""
		if r.Message.SenderID != nil {
			sender = *r.Message.SenderID
		} else {
			sender = r.Message.SenderService
		}
		v.Message = MessageView{ID: r.Message.ID, Title: r.Message.Title, Content: r.Message.Content, Type: r.Message.Type, CategoryName: r.Message.CategoryName, SenderID: sender, PublishedAt: r.Message.PublishedAt}
	}
	return v
}

// Page is a listing with the unread count.
type Page struct {
	Items      []EntryView `json:"items"`
	Unread     int64       `json:"unread"`
	NextCursor string      `json:"next_cursor"`
}

// List pages the caller's inbox (status: all | unread | read).
func (s *Service) List(ctx context.Context, subj authz.Subjects, status, cursor string, limit int) (Page, error) {
	switch status {
	case "", "all":
		status = ""
	case "unread", "read":
	default:
		return Page{}, ErrInput
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ts, id, err := messages.DecodeCursor(cursor)
	if err != nil {
		return Page{}, ErrInput
	}
	rows, err := s.st.InboxPage(ctx, subj.TenantID, subj.UserID, status, ts, id, limit)
	if err != nil {
		return Page{}, err
	}
	unread, err := s.st.InboxUnread(ctx, subj.TenantID, subj.UserID)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: make([]EntryView, 0, len(rows)), Unread: unread}
	for _, r := range rows {
		p.Items = append(p.Items, view(r))
	}
	if len(rows) == limit {
		p.NextCursor = messages.EncodeCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	return p, nil
}

// Unread counts the caller's unread entries.
func (s *Service) Unread(ctx context.Context, subj authz.Subjects) (int64, error) {
	return s.st.InboxUnread(ctx, subj.TenantID, subj.UserID)
}

// Read returns one entry with its content and marks it read.
func (s *Service) Read(ctx context.Context, subj authz.Subjects, id string) (EntryView, error) {
	row, err := s.st.GetInboxEntry(ctx, subj.TenantID, subj.UserID, id)
	if err != nil {
		return EntryView{}, err
	}
	if row.Status == "sent" || row.Status == "received" {
		if _, err := s.st.SetInboxStatus(ctx, subj.TenantID, subj.UserID, []string{id}, "read"); err != nil {
			return EntryView{}, err
		}
		if row, err = s.st.GetInboxEntry(ctx, subj.TenantID, subj.UserID, id); err != nil {
			return EntryView{}, err
		}
		s.emit(audit.Event{Type: audit.InboxRead, TenantID: subj.TenantID, ActorKind: "user", ActorID: subj.UserID, SubjectKind: "inbox", SubjectID: id, Outcome: "ok", Details: map[string]any{"message_id": row.MessageID}})
	}
	return view(row), nil
}

// SetStatus marks the caller's entries read, unread or received; returns
// the number changed and the new unread count.
func (s *Service) SetStatus(ctx context.Context, subj authz.Subjects, ids []string, status string) (updated, unread int64, err error) {
	if len(ids) == 0 || len(ids) > 500 || (status != "read" && status != "unread" && status != "received") {
		return 0, 0, ErrInput
	}
	if updated, err = s.st.SetInboxStatus(ctx, subj.TenantID, subj.UserID, ids, status); err != nil {
		return 0, 0, err
	}
	if status == "read" && updated > 0 {
		s.emit(audit.Event{Type: audit.InboxRead, TenantID: subj.TenantID, ActorKind: "user", ActorID: subj.UserID, SubjectKind: "inbox", SubjectID: ids[0], Outcome: "ok", Details: map[string]any{"count": updated}})
	}
	unread, err = s.st.InboxUnread(ctx, subj.TenantID, subj.UserID)
	return updated, unread, err
}

// Remove hides the caller's entries.
func (s *Service) Remove(ctx context.Context, subj authz.Subjects, ids []string) (updated, unread int64, err error) {
	if len(ids) == 0 || len(ids) > 500 {
		return 0, 0, ErrInput
	}
	if updated, err = s.st.SetInboxStatus(ctx, subj.TenantID, subj.UserID, ids, "deleted"); err != nil {
		return 0, 0, err
	}
	if updated > 0 {
		s.emit(audit.Event{Type: audit.InboxDeleted, TenantID: subj.TenantID, ActorKind: "user", ActorID: subj.UserID, SubjectKind: "inbox", SubjectID: ids[0], Outcome: "ok", Details: map[string]any{"count": updated}})
	}
	unread, err = s.st.InboxUnread(ctx, subj.TenantID, subj.UserID)
	return updated, unread, err
}

func (s *Service) emit(e audit.Event) {
	if s.audit != nil {
		_ = s.audit.Emit(e)
	}
}
