// Package messages holds internal messages: categories, the message state
// machine, fan-out to recipients' inboxes and the scheduler that publishes
// delayed messages exactly once (research R5, R6).
package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Statuses and types.
const (
	Draft      = "draft"
	Scheduled  = "scheduled"
	Publishing = "publishing"
	Published  = "published"
	Revoked    = "revoked"
	Archived   = "archived"

	TypeNotification = "notification"
	TypePrivate      = "private"
	TypeGroup        = "group"
)

// Limits.
const (
	MaxRecipients = 10000
	FanoutBatch   = 1000
)

// Errors.
var (
	ErrState   = errors.New("messages: not allowed in this status")
	ErrInput   = errors.New("messages: invalid input")
	ErrInUse   = errors.New("messages: category in use")
	ErrNotMine = errors.New("messages: not the sender")
)

// ValidationError carries a client-safe detail.
type ValidationError struct {
	Msg    string
	Detail map[string]any
}

func (e *ValidationError) Error() string { return "messages: " + e.Msg }

func invalid(msg, field string) error {
	return &ValidationError{Msg: msg, Detail: map[string]any{"field": field}}
}

// Directory resolves recipients through the auth service.
type Directory interface {
	// Lookup returns the ids among ids that are active members of the tenant.
	Lookup(ctx context.Context, tenantID string, ids []string) ([]string, error)
	// Members pages every active member id of the tenant through fn.
	Members(ctx context.Context, tenantID string, fn func(ids []string) error) error
}

// Publisher pushes live events (the stream hub; a no-op until wired).
type Publisher interface {
	Publish(ctx context.Context, tenantID string, to []string, all bool, typ string, data any) error
}

// NopPublisher drops events.
type NopPublisher struct{}

// Publish implements Publisher.
func (NopPublisher) Publish(context.Context, string, []string, bool, string, any) error { return nil }

// Recipients is the recipient request of a message.
type Recipients struct {
	All   bool     `json:"all,omitempty"`
	Users []string `json:"users,omitempty"`
}

// Service manages categories and messages.
type Service struct {
	st    repo.Store
	audit *audit.Writer
	dir   Directory
	pub   Publisher
	now   func() time.Time
}

// New wires the service.
func New(st repo.Store, aw *audit.Writer, dir Directory, pub Publisher) *Service {
	if pub == nil {
		pub = NopPublisher{}
	}
	return &Service{st: st, audit: aw, dir: dir, pub: pub, now: time.Now}
}

// SetClock injects the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetPublisher wires the live publisher after construction.
func (s *Service) SetPublisher(p Publisher) { s.pub = p }

func strp(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func userPtr(s authz.Subjects) *string {
	if s.UserID == "" {
		return nil
	}
	u := s.UserID
	return &u
}

// ---- categories

// CategoryInput is a create/update request.
type CategoryInput struct {
	Name        string
	Description string
	Sort        int
}

// CategoryView is a category as returned to clients.
type CategoryView struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Sort         int       `json:"sort"`
	MessageCount int       `json:"message_count"`
	CreatedBy    string    `json:"created_by"`
	UpdatedBy    string    `json:"updated_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func categoryView(c store.Category) CategoryView {
	return CategoryView{ID: c.ID, Name: c.Name, Description: c.Description, Sort: c.Sort, MessageCount: c.MessageCount, CreatedBy: strp(c.CreatedBy), UpdatedBy: strp(c.UpdatedBy), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func (in CategoryInput) validate() error {
	if len(in.Name) == 0 || len(in.Name) > 100 {
		return invalid("name must be 1-100 characters", "name")
	}
	if len(in.Description) > 500 {
		return invalid("description too long", "description")
	}
	if in.Sort < 0 || in.Sort > 100000 {
		return invalid("sort out of range", "sort")
	}
	return nil
}

// CreateCategory stores a category.
func (s *Service) CreateCategory(ctx context.Context, subj authz.Subjects, in CategoryInput) (CategoryView, error) {
	if err := in.validate(); err != nil {
		return CategoryView{}, err
	}
	row := store.Category{ID: store.NewID(), TenantID: subj.TenantID, Name: in.Name, Description: in.Description, Sort: in.Sort, CreatedBy: userPtr(subj), UpdatedBy: userPtr(subj)}
	if err := s.st.InsertCategory(ctx, row); err != nil {
		return CategoryView{}, err
	}
	s.emit(audit.Event{Type: audit.CategoryCreated, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "category", SubjectID: row.ID, Outcome: "ok", Details: map[string]any{"name": in.Name}})
	got, err := s.st.GetCategory(ctx, subj.TenantID, row.ID)
	if err != nil {
		return CategoryView{}, err
	}
	return categoryView(got), nil
}

// ListCategories orders by sort then name.
func (s *Service) ListCategories(ctx context.Context, subj authz.Subjects) ([]CategoryView, error) {
	rows, err := s.st.ListCategories(ctx, subj.TenantID)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryView, 0, len(rows))
	for _, r := range rows {
		out = append(out, categoryView(r))
	}
	return out, nil
}

// UpdateCategory rewrites a category.
func (s *Service) UpdateCategory(ctx context.Context, subj authz.Subjects, id string, in CategoryInput) (CategoryView, error) {
	if err := in.validate(); err != nil {
		return CategoryView{}, err
	}
	row, err := s.st.GetCategory(ctx, subj.TenantID, id)
	if err != nil {
		return CategoryView{}, err
	}
	row.Name, row.Description, row.Sort, row.UpdatedBy = in.Name, in.Description, in.Sort, userPtr(subj)
	if err := s.st.UpdateCategory(ctx, row); err != nil {
		return CategoryView{}, err
	}
	s.emit(audit.Event{Type: audit.CategoryUpdated, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "category", SubjectID: id, Outcome: "ok", Details: map[string]any{"name": in.Name}})
	got, err := s.st.GetCategory(ctx, subj.TenantID, id)
	if err != nil {
		return CategoryView{}, err
	}
	return categoryView(got), nil
}

// DeleteCategory removes a category; one in use refuses with ErrInUse.
func (s *Service) DeleteCategory(ctx context.Context, subj authz.Subjects, id string) error {
	row, err := s.st.GetCategory(ctx, subj.TenantID, id)
	if err != nil {
		return err
	}
	if row.MessageCount > 0 {
		return fmt.Errorf("%w: %d messages", ErrInUse, row.MessageCount)
	}
	if err := s.st.DeleteCategory(ctx, subj.TenantID, id); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrInUse
		}
		return err
	}
	s.emit(audit.Event{Type: audit.CategoryDeleted, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "category", SubjectID: id, Outcome: "ok", Details: map[string]any{"name": row.Name}})
	return nil
}

// ---- messages

// MessageInput is a create/update request.
type MessageInput struct {
	Title       string
	Content     string
	Type        string
	CategoryID  string
	Recipients  Recipients
	ScheduledAt *time.Time
}

// MessageView is a message as returned to clients.
type MessageView struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	CategoryID     *string    `json:"category_id"`
	CategoryName   string     `json:"category_name"`
	SenderID       string     `json:"sender_id"`
	Recipients     Recipients `json:"recipients"`
	RecipientCount int        `json:"recipient_count"`
	ReadCount      int        `json:"read_count"`
	ScheduledAt    *time.Time `json:"scheduled_at"`
	PublishedAt    *time.Time `json:"published_at"`
	CreatedBy      string     `json:"created_by"`
	UpdatedBy      string     `json:"updated_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func messageView(m store.Message) MessageView {
	var rc Recipients
	_ = json.Unmarshal(m.Recipients, &rc)
	if rc.Users == nil {
		rc.Users = []string{}
	}
	sender := strp(m.SenderID)
	if sender == "" {
		sender = m.SenderService
	}
	return MessageView{ID: m.ID, Title: m.Title, Content: m.Content, Type: m.Type, Status: m.Status, CategoryID: m.CategoryID, CategoryName: m.CategoryName, SenderID: sender,
		Recipients: rc, RecipientCount: m.RecipientCount, ReadCount: m.ReadCount, ScheduledAt: m.ScheduledAt, PublishedAt: m.PublishedAt,
		CreatedBy: strp(m.CreatedBy), UpdatedBy: strp(m.UpdatedBy), CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func (in *MessageInput) validate() error {
	if len(in.Title) == 0 || len(in.Title) > 200 {
		return invalid("title must be 1-200 characters", "title")
	}
	if len(in.Content) > 64<<10 {
		return invalid("content exceeds 64 KiB", "content")
	}
	if in.Type == "" {
		in.Type = TypeNotification
	}
	if in.Type != TypeNotification && in.Type != TypePrivate && in.Type != TypeGroup {
		return invalid("unknown type", "type")
	}
	if in.Recipients.All == (len(in.Recipients.Users) > 0) {
		return invalid("recipients must be all=true or a list of users", "recipients")
	}
	if len(in.Recipients.Users) > MaxRecipients {
		return invalid("too many recipients", "recipients")
	}
	seen := map[string]bool{}
	dedup := in.Recipients.Users[:0:0]
	for _, u := range in.Recipients.Users {
		if u == "" {
			return invalid("empty recipient id", "recipients")
		}
		if !seen[u] {
			seen[u] = true
			dedup = append(dedup, u)
		}
	}
	in.Recipients.Users = dedup
	return nil
}

func (s *Service) row(ctx context.Context, tenantID, id string) (store.Message, error) {
	return s.st.GetMessage(ctx, tenantID, id)
}

// canManage reports whether the caller may act on the message: its sender,
// or anyone holding messages:manage (the caller passes manage=true).
func canManage(m store.Message, subj authz.Subjects, manage bool) bool {
	return manage || (subj.UserID != "" && strp(m.SenderID) == subj.UserID) || (subj.Service != "" && m.SenderService == subj.Service)
}

// Create stores a draft.
func (s *Service) Create(ctx context.Context, subj authz.Subjects, in MessageInput) (MessageView, error) {
	if err := in.validate(); err != nil {
		return MessageView{}, err
	}
	rc, _ := json.Marshal(in.Recipients)
	row := store.Message{ID: store.NewID(), TenantID: subj.TenantID, Title: in.Title, Content: in.Content, Type: in.Type, Status: Draft, SenderID: userPtr(subj), SenderService: subj.Service,
		Recipients: rc, ScheduledAt: in.ScheduledAt, CreatedBy: userPtr(subj), UpdatedBy: userPtr(subj)}
	if in.CategoryID != "" {
		c := in.CategoryID
		row.CategoryID = &c
	}
	if err := s.st.InsertMessage(ctx, row); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return MessageView{}, invalid("unknown category", "category_id")
		}
		return MessageView{}, err
	}
	s.emit(audit.Event{Type: audit.MessageCreated, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "message", SubjectID: row.ID, Outcome: "ok", Details: map[string]any{"type": in.Type, "all": in.Recipients.All, "recipients": len(in.Recipients.Users)}})
	return s.Get(ctx, subj, row.ID, true)
}

// Get returns a message; without manage only the sender sees it.
func (s *Service) Get(ctx context.Context, subj authz.Subjects, id string, manage bool) (MessageView, error) {
	m, err := s.row(ctx, subj.TenantID, id)
	if err != nil {
		return MessageView{}, err
	}
	if !canManage(m, subj, manage) {
		return MessageView{}, store.ErrNotFound
	}
	return messageView(m), nil
}

// ListFilter selects messages.
type ListFilter struct {
	Status, CategoryID, Q string
	Cursor                string
	Limit                 int
}

// List pages messages newest first; without manage only the caller's own.
func (s *Service) List(ctx context.Context, subj authz.Subjects, f ListFilter, manage bool) ([]MessageView, string, error) {
	sf := store.MessageFilter{Status: f.Status, CategoryID: f.CategoryID, Q: f.Q, Limit: f.Limit}
	if !manage {
		sf.SenderID = subj.UserID
		if sf.SenderID == "" {
			return []MessageView{}, "", nil
		}
	}
	if sf.Limit <= 0 || sf.Limit > 100 {
		sf.Limit = 50
	}
	ts, id, err := DecodeCursor(f.Cursor)
	if err != nil {
		return nil, "", err
	}
	sf.CursorTS, sf.CursorID = ts, id
	rows, err := s.st.ListMessages(ctx, subj.TenantID, sf)
	if err != nil {
		return nil, "", err
	}
	out := make([]MessageView, 0, len(rows))
	for _, r := range rows {
		out = append(out, messageView(r))
	}
	next := ""
	if len(rows) == sf.Limit {
		next = EncodeCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	return out, next, nil
}

// Update rewrites a draft or scheduled message.
func (s *Service) Update(ctx context.Context, subj authz.Subjects, id string, in MessageInput, manage bool) (MessageView, error) {
	if err := in.validate(); err != nil {
		return MessageView{}, err
	}
	m, err := s.row(ctx, subj.TenantID, id)
	if err != nil {
		return MessageView{}, err
	}
	if !canManage(m, subj, manage) {
		return MessageView{}, store.ErrNotFound
	}
	if m.Status != Draft && m.Status != Scheduled {
		return MessageView{}, ErrState
	}
	rc, _ := json.Marshal(in.Recipients)
	m.Title, m.Content, m.Type, m.Recipients, m.ScheduledAt, m.UpdatedBy, m.CategoryID = in.Title, in.Content, in.Type, rc, in.ScheduledAt, userPtr(subj), nil
	if in.CategoryID != "" {
		c := in.CategoryID
		m.CategoryID = &c
	}
	if m.Status == Scheduled && (in.ScheduledAt == nil || !in.ScheduledAt.After(s.now())) {
		m.Status = Draft
	}
	if err := s.st.UpdateMessage(ctx, m); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return MessageView{}, invalid("unknown category", "category_id")
		}
		return MessageView{}, err
	}
	s.emit(audit.Event{Type: audit.MessageUpdated, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "message", SubjectID: id, Outcome: "ok"})
	return s.Get(ctx, subj, id, manage)
}

// SendResult reports a send.
type SendResult struct {
	Status            string   `json:"status"`
	RecipientCount    int      `json:"recipient_count"`
	DroppedRecipients []string `json:"dropped_recipients"`
}

// Send publishes a draft now, or schedules it when scheduled_at is in the future.
func (s *Service) Send(ctx context.Context, subj authz.Subjects, id string, manage bool) (SendResult, error) {
	m, err := s.row(ctx, subj.TenantID, id)
	if err != nil {
		return SendResult{}, err
	}
	if !canManage(m, subj, manage) {
		return SendResult{}, store.ErrNotFound
	}
	if m.Status != Draft {
		return SendResult{}, ErrState
	}
	if m.ScheduledAt != nil && m.ScheduledAt.After(s.now()) {
		if err := s.st.SetMessageStatus(ctx, subj.TenantID, id, Scheduled, nil); err != nil {
			return SendResult{}, err
		}
		s.emit(audit.Event{Type: audit.MessageUpdated, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "message", SubjectID: id, Outcome: "ok", Details: map[string]any{"status": Scheduled}})
		return SendResult{Status: Scheduled, DroppedRecipients: []string{}}, nil
	}
	if err := s.st.SetMessageStatus(ctx, subj.TenantID, id, Publishing, nil); err != nil {
		return SendResult{}, err
	}
	n, dropped, err := s.Publish(ctx, subj.TenantID, id, subj)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Status: Published, RecipientCount: n, DroppedRecipients: dropped}, nil
}

// Publish fans a publishing message out to its recipients (idempotent: rows
// already present are skipped) and marks it published. actor is the
// subject for the audit event (the scheduler passes the system actor).
func (s *Service) Publish(ctx context.Context, tenantID, id string, actor authz.Subjects) (int, []string, error) {
	m, err := s.row(ctx, tenantID, id)
	if err != nil {
		return 0, nil, err
	}
	var rc Recipients
	_ = json.Unmarshal(m.Recipients, &rc)
	total, dropped := 0, []string{}
	deliver := func(ids []string) error {
		rows := make([]store.InboxRow, 0, len(ids))
		for _, u := range ids {
			rows = append(rows, store.InboxRow{ID: store.NewID(), TenantID: tenantID, MessageID: id, RecipientID: u, Status: "sent"})
		}
		n, err := s.st.InsertInboxBatch(ctx, rows)
		if err != nil {
			return err
		}
		total += int(n)
		if n > 0 {
			_ = s.pub.Publish(ctx, tenantID, ids, false, "inbox", map[string]any{"message_id": id, "title": m.Title, "category": m.CategoryName})
		}
		return nil
	}
	if rc.All {
		err = s.dir.Members(ctx, tenantID, func(ids []string) error {
			for i := 0; i < len(ids); i += FanoutBatch {
				end := min(i+FanoutBatch, len(ids))
				if err := deliver(ids[i:end]); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return 0, nil, err
		}
	} else {
		active, err := s.dir.Lookup(ctx, tenantID, rc.Users)
		if err != nil {
			return 0, nil, err
		}
		ok := map[string]bool{}
		for _, a := range active {
			ok[a] = true
		}
		for _, u := range rc.Users {
			if !ok[u] {
				dropped = append(dropped, u)
			}
		}
		sort.Strings(dropped)
		for i := 0; i < len(active); i += FanoutBatch {
			end := min(i+FanoutBatch, len(active))
			if err := deliver(active[i:end]); err != nil {
				return 0, nil, err
			}
		}
	}
	now := s.now()
	if err := s.st.SetMessageStatus(ctx, tenantID, id, Published, &now); err != nil {
		return 0, nil, err
	}
	s.emit(audit.Event{Type: audit.MessagePublished, TenantID: tenantID, ActorKind: actor.ActorKind(), ActorID: actor.ActorID(), SubjectKind: "message", SubjectID: id, Outcome: "ok",
		Details: map[string]any{"recipients": total, "dropped": len(dropped), "all": rc.All}})
	return total, dropped, nil
}

// Cancel returns a scheduled message to draft.
func (s *Service) Cancel(ctx context.Context, subj authz.Subjects, id string, manage bool) (MessageView, error) {
	return s.transition(ctx, subj, id, manage, Scheduled, Draft, audit.MessageUpdated)
}

// Archive moves a published or revoked message to archived.
func (s *Service) Archive(ctx context.Context, subj authz.Subjects, id string, manage bool) (MessageView, error) {
	return s.transition(ctx, subj, id, manage, Published, Archived, audit.MessageArchived, Revoked)
}

func (s *Service) transition(ctx context.Context, subj authz.Subjects, id string, manage bool, from, to string, ev audit.EventType, alsoFrom ...string) (MessageView, error) {
	m, err := s.row(ctx, subj.TenantID, id)
	if err != nil {
		return MessageView{}, err
	}
	if !canManage(m, subj, manage) {
		return MessageView{}, store.ErrNotFound
	}
	allowed := m.Status == from
	for _, f := range alsoFrom {
		allowed = allowed || m.Status == f
	}
	if !allowed {
		return MessageView{}, ErrState
	}
	if err := s.st.SetMessageStatus(ctx, subj.TenantID, id, to, nil); err != nil {
		return MessageView{}, err
	}
	s.emit(audit.Event{Type: ev, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "message", SubjectID: id, Outcome: "ok", Details: map[string]any{"status": to}})
	return s.Get(ctx, subj, id, manage)
}

// Revoke withdraws a published message: unread inbox entries are hidden,
// read ones stay marked revoked; recipients get a live event.
func (s *Service) Revoke(ctx context.Context, subj authz.Subjects, id string, manage bool) (MessageView, error) {
	m, err := s.row(ctx, subj.TenantID, id)
	if err != nil {
		return MessageView{}, err
	}
	if !canManage(m, subj, manage) {
		return MessageView{}, store.ErrNotFound
	}
	if m.Status != Published {
		return MessageView{}, ErrState
	}
	var affected []string
	err = s.st.Atomic(ctx, subj.TenantID, func(tx repo.Store) error {
		var err error
		if affected, err = tx.RevokeUnread(ctx, subj.TenantID, id); err != nil {
			return err
		}
		return tx.SetMessageStatus(ctx, subj.TenantID, id, Revoked, nil)
	})
	if err != nil {
		return MessageView{}, err
	}
	for i := 0; i < len(affected); i += FanoutBatch {
		end := min(i+FanoutBatch, len(affected))
		_ = s.pub.Publish(ctx, subj.TenantID, affected[i:end], false, "inbox.revoked", map[string]any{"message_id": id})
	}
	s.emit(audit.Event{Type: audit.MessageRevoked, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "message", SubjectID: id, Outcome: "ok", Details: map[string]any{"recipients": len(affected)}})
	return s.Get(ctx, subj, id, manage)
}

// Delete removes a draft, or an archived/revoked message with its inbox rows.
func (s *Service) Delete(ctx context.Context, subj authz.Subjects, id string, manage bool) error {
	m, err := s.row(ctx, subj.TenantID, id)
	if err != nil {
		return err
	}
	if !canManage(m, subj, manage) {
		return store.ErrNotFound
	}
	if m.Status != Draft && m.Status != Archived && m.Status != Revoked {
		return ErrState
	}
	if err := s.st.DeleteMessage(ctx, subj.TenantID, id); err != nil {
		return err
	}
	s.emit(audit.Event{Type: audit.MessageDeleted, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "message", SubjectID: id, Outcome: "ok", Details: map[string]any{"status": m.Status}})
	return nil
}

// RecipientView is one delivery of a message (sender view).
type RecipientView struct {
	ID          string     `json:"id"`
	RecipientID string     `json:"recipient_id"`
	Status      string     `json:"status"`
	ReadAt      *time.Time `json:"read_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Recipients lists the deliveries of a message, paged by id.
func (s *Service) Recipients(ctx context.Context, subj authz.Subjects, id, status, cursor string, limit int, manage bool) ([]RecipientView, string, error) {
	m, err := s.row(ctx, subj.TenantID, id)
	if err != nil {
		return nil, "", err
	}
	if !canManage(m, subj, manage) {
		return nil, "", store.ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.st.MessageRecipients(ctx, subj.TenantID, id, status, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]RecipientView, 0, len(rows))
	for _, r := range rows {
		out = append(out, RecipientView{ID: r.ID, RecipientID: r.RecipientID, Status: r.Status, ReadAt: r.ReadAt, CreatedAt: r.CreatedAt})
	}
	next := ""
	if len(rows) == limit {
		next = rows[len(rows)-1].ID
	}
	return out, next, nil
}

// EncodeCursor / DecodeCursor format a (time, id) page cursor.
func EncodeCursor(ts time.Time, id string) string {
	return ts.UTC().Format(time.RFC3339Nano) + "|" + id
}

// DecodeCursor parses a cursor ("" → zero values).
func DecodeCursor(c string) (time.Time, string, error) {
	if c == "" {
		return time.Time{}, "", nil
	}
	for i := 0; i < len(c); i++ {
		if c[i] == '|' && i > 0 {
			ts, err := time.Parse(time.RFC3339Nano, c[:i])
			if err != nil {
				break
			}
			return ts, c[i+1:], nil
		}
	}
	return time.Time{}, "", invalid("bad cursor", "cursor")
}

func (s *Service) emit(e audit.Event) {
	if s.audit != nil {
		_ = s.audit.Emit(e)
	}
}
