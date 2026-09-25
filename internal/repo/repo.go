// Package repo declares the persistence the services depend on. The database
// binding (repodb) runs every call in a tenant-scoped transaction under RLS;
// the in-memory double (memstore) applies the same tenant argument checks so
// unit tests observe identical semantics.
package repo

import (
	"context"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Channels is the channel persistence.
type Channels interface {
	InsertChannel(ctx context.Context, c store.Channel) error
	GetChannel(ctx context.Context, tenantID, id string) (store.Channel, error)
	ListChannels(ctx context.Context, tenantID, typ, afterName string, limit int) ([]store.Channel, error)
	UpdateChannel(ctx context.Context, c store.Channel) error
	ClearDefaultChannel(ctx context.Context, tenantID, typ, exceptID string) error
	DeleteChannel(ctx context.Context, tenantID, id string) error
	ManagedChannel(ctx context.Context, tenantID string) (store.Channel, error)
	DefaultEmailChannel(ctx context.Context, tenantID string) (store.Channel, error)
}

// Templates is the template persistence.
type Templates interface {
	InsertTemplate(ctx context.Context, t store.Template) error
	GetTemplate(ctx context.Context, tenantID, id string) (store.Template, error)
	ListTemplates(ctx context.Context, tenantID string, channelID *string, q, afterName string, limit int) ([]store.Template, error)
	TemplatesByIDs(ctx context.Context, tenantID string, ids []string) ([]store.Template, error)
	AllTemplates(ctx context.Context, tenantID string, limit int) ([]store.Template, error)
	UpdateTemplate(ctx context.Context, t store.Template) error
	ClearDefaultTemplate(ctx context.Context, tenantID, channelID, exceptID string) error
	DeleteTemplate(ctx context.Context, tenantID, id string) error
	TemplateByKey(ctx context.Context, tenantID, key string) (store.Template, error)
	SetTemplateBuiltin(ctx context.Context, t store.Template) error
}

// Log is the notification log persistence.
type Log interface {
	InsertLog(ctx context.Context, l store.LogRow) error
	SetLogOutcome(ctx context.Context, tenantID, id, status, errText, subject, body string, sentAt *time.Time) error
	GetLog(ctx context.Context, tenantID, id string) (store.LogRow, error)
	LogPage(ctx context.Context, tenantID string, f store.LogFilter) ([]store.LogRow, error)
	ExpirePendingLogs(ctx context.Context, olderThan time.Time) (int64, error) // system scope
}

// Grants is the relation-tuple persistence.
type Grants interface {
	UpsertGrant(ctx context.Context, g store.Grant) (store.Grant, error)
	GetGrant(ctx context.Context, tenantID, id string) (store.Grant, error)
	DeleteGrant(ctx context.Context, tenantID, id string) error
	GrantsOnResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]store.Grant, error)
	GrantsForSubjects(ctx context.Context, tenantID, userID string, roles []string, now time.Time) ([]store.Grant, error)
	DeleteGrantsOfResource(ctx context.Context, tenantID, resourceType, resourceID string) error
	DeleteGrantsOfSubject(ctx context.Context, tenantID, resourceType, resourceID, subjectType, subjectID string) error
}

// Categories is the message-category persistence.
type Categories interface {
	InsertCategory(ctx context.Context, c store.Category) error
	GetCategory(ctx context.Context, tenantID, id string) (store.Category, error)
	ListCategories(ctx context.Context, tenantID string) ([]store.Category, error)
	UpdateCategory(ctx context.Context, c store.Category) error
	DeleteCategory(ctx context.Context, tenantID, id string) error
}

// Messages is the internal-message persistence.
type Messages interface {
	InsertMessage(ctx context.Context, m store.Message) error
	GetMessage(ctx context.Context, tenantID, id string) (store.Message, error)
	ListMessages(ctx context.Context, tenantID string, f store.MessageFilter) ([]store.Message, error)
	UpdateMessage(ctx context.Context, m store.Message) error
	SetMessageStatus(ctx context.Context, tenantID, id, status string, publishedAt *time.Time) error
	DeleteMessage(ctx context.Context, tenantID, id string) error
	ClaimDueMessages(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]store.Message, error) // system scope
}

// Inbox is the per-recipient delivery persistence.
type Inbox interface {
	InsertInboxBatch(ctx context.Context, rows []store.InboxRow) (int64, error)
	InboxPage(ctx context.Context, tenantID, recipientID, status string, cursorTS time.Time, cursorID string, limit int) ([]store.InboxRow, error)
	GetInboxEntry(ctx context.Context, tenantID, recipientID, id string) (store.InboxRow, error)
	InboxUnread(ctx context.Context, tenantID, recipientID string) (int64, error)
	SetInboxStatus(ctx context.Context, tenantID, recipientID string, ids []string, status string) (int64, error)
	RevokeUnread(ctx context.Context, tenantID, messageID string) ([]string, error)
	MessageRecipients(ctx context.Context, tenantID, messageID, status, afterID string, limit int) ([]store.InboxRow, error)
}

// Audit is the audit persistence.
type Audit interface {
	InsertAuditRows(ctx context.Context, rows []store.AuditRow) error
	QueryAudit(ctx context.Context, tenantID, eventType, actorID string, from, to, cursor time.Time, limit int) ([]store.AuditRow, error)
}

// Stats reads the per-tenant counts.
type Stats interface {
	TenantStats(ctx context.Context, tenantID string, now time.Time) (store.Stats, error)
}

// Store is everything, plus Atomic: fn runs against a Store whose writes are
// committed together or not at all.
type Store interface {
	Channels
	Templates
	Log
	Grants
	Categories
	Messages
	Inbox
	Audit
	Stats
	Atomic(ctx context.Context, tenantID string, fn func(Store) error) error
}
