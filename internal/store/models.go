package store

import "time"

// Channel row: provider settings are sealed (research R1); SettingsPublic
// holds the non-secret fields for listings.
type Channel struct {
	ID, TenantID         string
	Name, Type           string
	SettingsSealed       []byte
	SettingsPublic       []byte // JSON object
	Enabled, IsDefault   bool
	Managed              bool // the platform channel created from configuration (read-only in the API)
	CreatedBy, UpdatedBy *string
	CreatedAt, UpdatedAt time.Time
	TemplateCount        int // joined for listings
}

// Template row.
type Template struct {
	ID, TenantID         string
	Name                 string
	ChannelID            *string
	ChannelType          string
	Subject, Body        string
	Variables            []string
	IsDefault            bool
	CreatedBy, UpdatedBy *string
	CreatedAt, UpdatedAt time.Time
	ChannelName          string // joined for display

	// System templates (feature 017): set by seeding only.
	SystemKey                   *string  // "<service>.<name>"; nil for ordinary templates
	BuiltinSubject, BuiltinBody string   // the built-in wording restore returns to
	RequiredVariables           []string // must stay referenced on save
	SecretVariables             []string // redacted in the stored log
}

// LogRow is one notification log entry (immutable once final).
type LogRow struct {
	ID, TenantID    string
	CreatedAt       time.Time
	ChannelID       string
	ChannelType     string
	TemplateID      *string
	TemplateKey     *string // system template sends
	Recipient       string
	RenderedSubject string
	RenderedBody    string
	Status          string // pending | sent | failed
	Error           string
	SenderKind      string // user | service
	SenderID        string
	Test            bool
	SentAt          *time.Time
}

// LogFilter selects log entries.
type LogFilter struct {
	ChannelID, TemplateID string
	Recipient, Status     string
	SenderID              string // forced for callers without stats:read
	From, To              time.Time
	CursorTS              time.Time
	CursorID              string
	Limit                 int
}

// Grant row.
type Grant struct {
	ID, TenantID             string
	ResourceType, ResourceID string // channel | template
	SubjectType, SubjectID   string // user | role | tenant ('' for tenant)
	Relation                 string // owner | editor | viewer | sharer
	GrantedBy                *string
	GrantedAt                time.Time
	ExpiresAt                *time.Time
}

// Category row.
type Category struct {
	ID, TenantID         string
	Name, Description    string
	Sort                 int
	CreatedBy, UpdatedBy *string
	CreatedAt, UpdatedAt time.Time
	MessageCount         int // joined for listings
}

// Message row.
type Message struct {
	ID, TenantID         string
	Title, Content       string
	Type, Status         string
	CategoryID           *string
	SenderID             *string
	SenderService        string
	Recipients           []byte // JSON: {"all":true} | {"users":[...]}
	ScheduledAt          *time.Time
	LeaseUntil           *time.Time
	PublishedAt          *time.Time
	CreatedBy, UpdatedBy *string
	CreatedAt, UpdatedAt time.Time
	CategoryName         string // joined
	RecipientCount       int    // joined
	ReadCount            int    // joined
}

// MessageFilter selects messages.
type MessageFilter struct {
	Status, CategoryID, Q string
	SenderID              string // forced for callers without messages:manage
	CursorTS              time.Time
	CursorID              string
	Limit                 int
}

// InboxRow is one message delivered to one recipient.
type InboxRow struct {
	ID, TenantID         string
	MessageID            string
	RecipientID          string
	Status               string // sent | received | read | revoked | deleted
	ReadAt               *time.Time
	CreatedAt, UpdatedAt time.Time
	Message              *Message // joined for listings
}

// AuditRow is one persisted audit event.
type AuditRow struct {
	TS                     time.Time
	TenantID, EventType    string
	ActorKind, ActorID     string
	SubjectKind, SubjectID string
	Outcome, Reason        string
	CorrelationID          string
	Details                []byte
	// SubjectName is filled on read for channels/templates (name) and
	// messages (title) that still exist; the writer leaves it empty.
	SubjectName string
}

// Stats are per-tenant counts.
type Stats struct {
	Channels, Templates int64
	Notifications       map[string]int64 // by status, last 24 h
	Messages            map[string]int64 // by status
	Operations24h       int64
}
