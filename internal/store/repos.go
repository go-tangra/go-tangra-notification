package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const uuidRE = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func jsonOrEmpty(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// restricted maps a foreign-key restriction (23503) to ErrConflict.
func restricted(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		return ErrConflict
	}
	return conflict(err)
}

// ---------------------------------------------------------------- channels

const channelCols = "c.id, c.tenant_id, c.name, c.type, c.settings_sealed, c.settings_public, c.enabled, c.is_default, c.managed, c.created_by, c.updated_by, c.created_at, c.updated_at, (SELECT count(*) FROM templates t WHERE t.channel_id = c.id)"

func scanChannel(r pgx.Row) (Channel, error) {
	var c Channel
	err := r.Scan(&c.ID, &c.TenantID, &c.Name, &c.Type, &c.SettingsSealed, &c.SettingsPublic, &c.Enabled, &c.IsDefault, &c.Managed, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt, &c.TemplateCount)
	return c, notFound(err)
}

func scanChannels(rows pgx.Rows) ([]Channel, error) {
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertChannel creates a channel; a name clash, a second default or a
// second managed channel is ErrConflict.
func InsertChannel(ctx context.Context, tx pgx.Tx, c Channel) error {
	_, err := tx.Exec(ctx, `INSERT INTO channels (id, tenant_id, name, type, settings_sealed, settings_public, enabled, is_default, managed, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, c.ID, c.TenantID, c.Name, c.Type, c.SettingsSealed, jsonOrEmpty(c.SettingsPublic), c.Enabled, c.IsDefault, c.Managed, c.CreatedBy)
	return conflict(err)
}

// ManagedChannel returns the tenant's configuration-managed channel.
func ManagedChannel(ctx context.Context, tx pgx.Tx, tenantID string) (Channel, error) {
	return scanChannel(tx.QueryRow(ctx, "SELECT "+channelCols+" FROM channels c WHERE c.tenant_id = $1 AND c.managed", tenantID))
}

// DefaultEmailChannel returns the tenant's default email channel (enabled
// or not; the caller decides).
func DefaultEmailChannel(ctx context.Context, tx pgx.Tx, tenantID string) (Channel, error) {
	return scanChannel(tx.QueryRow(ctx, "SELECT "+channelCols+" FROM channels c WHERE c.tenant_id = $1 AND c.type = 'email' AND c.is_default", tenantID))
}

// GetChannel by tenant + id.
func GetChannel(ctx context.Context, tx pgx.Tx, tenantID, id string) (Channel, error) {
	return scanChannel(tx.QueryRow(ctx, "SELECT "+channelCols+" FROM channels c WHERE c.tenant_id = $1 AND c.id = $2", tenantID, id))
}

// ListChannels pages by name (cursor = name of the last row seen) with an optional type.
func ListChannels(ctx context.Context, tx pgx.Tx, tenantID, typ, afterName string, limit int) ([]Channel, error) {
	rows, err := tx.Query(ctx, "SELECT "+channelCols+` FROM channels c WHERE c.tenant_id = $1 AND ($2 = '' OR c.type = $2) AND lower(c.name) > lower($3)
		ORDER BY lower(c.name), c.id LIMIT $4`, tenantID, typ, afterName, limit)
	if err != nil {
		return nil, err
	}
	return scanChannels(rows)
}

// UpdateChannel rewrites the mutable columns.
func UpdateChannel(ctx context.Context, tx pgx.Tx, c Channel) error {
	ct, err := tx.Exec(ctx, `UPDATE channels SET name = $3, settings_sealed = $4, settings_public = $5, enabled = $6, is_default = $7, updated_by = $8, updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, c.TenantID, c.ID, c.Name, c.SettingsSealed, jsonOrEmpty(c.SettingsPublic), c.Enabled, c.IsDefault, c.UpdatedBy)
	if err != nil {
		return conflict(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearDefaultChannel drops the default flag of every channel of the type except id.
func ClearDefaultChannel(ctx context.Context, tx pgx.Tx, tenantID, typ, exceptID string) error {
	_, err := tx.Exec(ctx, "UPDATE channels SET is_default = false, updated_at = now() WHERE tenant_id = $1 AND type = $2 AND is_default AND id <> $3", tenantID, typ, exceptID)
	return err
}

// DeleteChannel removes a channel; templates referencing it make it ErrConflict.
func DeleteChannel(ctx context.Context, tx pgx.Tx, tenantID, id string) error {
	ct, err := tx.Exec(ctx, "DELETE FROM channels WHERE tenant_id = $1 AND id = $2", tenantID, id)
	if err != nil {
		return restricted(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- templates

const templateCols = "t.id, t.tenant_id, t.name, t.channel_id, t.channel_type, t.subject, t.body, t.variables, t.is_default, t.created_by, t.updated_by, t.created_at, t.updated_at, COALESCE(c.name, ''), " +
	"t.system_key, COALESCE(t.builtin_subject, ''), COALESCE(t.builtin_body, ''), t.required_variables, t.secret_variables"
const templateFrom = " FROM templates t LEFT JOIN channels c ON c.id = t.channel_id "

func scanTemplate(r pgx.Row) (Template, error) {
	var t Template
	err := r.Scan(&t.ID, &t.TenantID, &t.Name, &t.ChannelID, &t.ChannelType, &t.Subject, &t.Body, &t.Variables, &t.IsDefault, &t.CreatedBy, &t.UpdatedBy, &t.CreatedAt, &t.UpdatedAt, &t.ChannelName,
		&t.SystemKey, &t.BuiltinSubject, &t.BuiltinBody, &t.RequiredVariables, &t.SecretVariables)
	t.Variables, t.RequiredVariables, t.SecretVariables = nonNil(t.Variables), nonNil(t.RequiredVariables), nonNil(t.SecretVariables)
	return t, notFound(err)
}

func scanTemplates(rows pgx.Rows) ([]Template, error) {
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// InsertTemplate creates a template (system columns only for system templates).
func InsertTemplate(ctx context.Context, tx pgx.Tx, t Template) error {
	var builtinSubject, builtinBody *string
	if t.SystemKey != nil {
		builtinSubject, builtinBody = &t.BuiltinSubject, &t.BuiltinBody
	}
	_, err := tx.Exec(ctx, `INSERT INTO templates (id, tenant_id, name, channel_id, channel_type, subject, body, variables, is_default, created_by, updated_by,
		system_key, builtin_subject, builtin_body, required_variables, secret_variables)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$11,$12,$13,$14,$15)`, t.ID, t.TenantID, t.Name, t.ChannelID, t.ChannelType, t.Subject, t.Body, nonNil(t.Variables), t.IsDefault, t.CreatedBy,
		t.SystemKey, builtinSubject, builtinBody, nonNil(t.RequiredVariables), nonNil(t.SecretVariables))
	return conflict(err)
}

// TemplateByKey returns the system template of the tenant with the key.
func TemplateByKey(ctx context.Context, tx pgx.Tx, tenantID, key string) (Template, error) {
	return scanTemplate(tx.QueryRow(ctx, "SELECT "+templateCols+templateFrom+"WHERE t.tenant_id = $1 AND t.system_key = $2", tenantID, key))
}

// SetTemplateBuiltin refreshes the built-in wording and the variable sets
// of a system template; subject and body (operator edits) are untouched.
func SetTemplateBuiltin(ctx context.Context, tx pgx.Tx, t Template) error {
	ct, err := tx.Exec(ctx, `UPDATE templates SET builtin_subject = $3, builtin_body = $4, variables = $5, required_variables = $6, secret_variables = $7, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND system_key IS NOT NULL`, t.TenantID, t.ID, t.BuiltinSubject, t.BuiltinBody, nonNil(t.Variables), nonNil(t.RequiredVariables), nonNil(t.SecretVariables))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetTemplate by tenant + id.
func GetTemplate(ctx context.Context, tx pgx.Tx, tenantID, id string) (Template, error) {
	return scanTemplate(tx.QueryRow(ctx, "SELECT "+templateCols+templateFrom+"WHERE t.tenant_id = $1 AND t.id = $2", tenantID, id))
}

// ListTemplates pages by name with optional channel and name filter.
func ListTemplates(ctx context.Context, tx pgx.Tx, tenantID string, channelID *string, q, afterName string, limit int) ([]Template, error) {
	rows, err := tx.Query(ctx, "SELECT "+templateCols+templateFrom+`WHERE t.tenant_id = $1 AND ($2::uuid IS NULL OR t.channel_id = $2)
		AND ($3 = '' OR t.name ILIKE '%' || $3 || '%') AND lower(t.name) > lower($4) ORDER BY lower(t.name), t.id LIMIT $5`, tenantID, channelID, escapeLike(q), afterName, limit)
	if err != nil {
		return nil, err
	}
	return scanTemplates(rows)
}

// TemplatesByIDs loads templates by id (order by name).
func TemplatesByIDs(ctx context.Context, tx pgx.Tx, tenantID string, ids []string) ([]Template, error) {
	rows, err := tx.Query(ctx, "SELECT "+templateCols+templateFrom+"WHERE t.tenant_id = $1 AND t.id = ANY($2::uuid[]) ORDER BY lower(t.name), t.id", tenantID, nonNil(ids))
	if err != nil {
		return nil, err
	}
	return scanTemplates(rows)
}

// AllTemplates lists every template of the tenant (backup; bounded).
func AllTemplates(ctx context.Context, tx pgx.Tx, tenantID string, limit int) ([]Template, error) {
	rows, err := tx.Query(ctx, "SELECT "+templateCols+templateFrom+"WHERE t.tenant_id = $1 ORDER BY lower(t.name), t.id LIMIT $2", tenantID, limit)
	if err != nil {
		return nil, err
	}
	return scanTemplates(rows)
}

// UpdateTemplate rewrites the mutable columns.
func UpdateTemplate(ctx context.Context, tx pgx.Tx, t Template) error {
	ct, err := tx.Exec(ctx, `UPDATE templates SET name = $3, channel_id = $4, channel_type = $5, subject = $6, body = $7, variables = $8, is_default = $9, updated_by = $10, updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, t.TenantID, t.ID, t.Name, t.ChannelID, t.ChannelType, t.Subject, t.Body, nonNil(t.Variables), t.IsDefault, t.UpdatedBy)
	if err != nil {
		return conflict(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearDefaultTemplate drops the default flag of every template of the channel except id.
func ClearDefaultTemplate(ctx context.Context, tx pgx.Tx, tenantID, channelID, exceptID string) error {
	_, err := tx.Exec(ctx, "UPDATE templates SET is_default = false, updated_at = now() WHERE tenant_id = $1 AND channel_id = $2 AND is_default AND id <> $3", tenantID, channelID, exceptID)
	return err
}

// DeleteTemplate removes a template.
func DeleteTemplate(ctx context.Context, tx pgx.Tx, tenantID, id string) error {
	ct, err := tx.Exec(ctx, "DELETE FROM templates WHERE tenant_id = $1 AND id = $2", tenantID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func escapeLike(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

// ---------------------------------------------------------------- notification log

const logCols = "id, tenant_id, created_at, channel_id, channel_type, template_id, recipient, rendered_subject, rendered_body, status, error, sender_kind, sender_id, test, sent_at, template_key"

func scanLog(r pgx.Row) (LogRow, error) {
	var l LogRow
	err := r.Scan(&l.ID, &l.TenantID, &l.CreatedAt, &l.ChannelID, &l.ChannelType, &l.TemplateID, &l.Recipient, &l.RenderedSubject, &l.RenderedBody, &l.Status, &l.Error, &l.SenderKind, &l.SenderID, &l.Test, &l.SentAt, &l.TemplateKey)
	return l, notFound(err)
}

// InsertLog writes a pending entry.
func InsertLog(ctx context.Context, tx pgx.Tx, l LogRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO notification_log (id, tenant_id, created_at, channel_id, channel_type, template_id, recipient, rendered_subject, rendered_body, status, error, sender_kind, sender_id, test, sent_at, template_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, l.ID, l.TenantID, l.CreatedAt, l.ChannelID, l.ChannelType, l.TemplateID, l.Recipient, l.RenderedSubject, l.RenderedBody, l.Status, l.Error, l.SenderKind, l.SenderID, l.Test, l.SentAt, l.TemplateKey)
	return err
}

// SetLogOutcome finalises a pending entry (rendered content may be set too).
func SetLogOutcome(ctx context.Context, tx pgx.Tx, tenantID, id, status, errText, subject, body string, sentAt *time.Time) error {
	ct, err := tx.Exec(ctx, `UPDATE notification_log SET status = $3, error = $4, rendered_subject = $5, rendered_body = $6, sent_at = $7
		WHERE tenant_id = $1 AND id = $2 AND status = 'pending'`, tenantID, id, status, errText, subject, body, sentAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetLog by tenant + id.
func GetLog(ctx context.Context, tx pgx.Tx, tenantID, id string) (LogRow, error) {
	return scanLog(tx.QueryRow(ctx, "SELECT "+logCols+" FROM notification_log WHERE tenant_id = $1 AND id = $2", tenantID, id))
}

// LogPage lists entries newest first under the filter; the cursor is the
// (created_at, id) of the last row seen. Bodies are not loaded.
func LogPage(ctx context.Context, tx pgx.Tx, tenantID string, f LogFilter) ([]LogRow, error) {
	to := f.To
	if to.IsZero() {
		to = time.Now().Add(time.Minute)
	}
	var cursorTS *time.Time
	if !f.CursorTS.IsZero() {
		cursorTS = &f.CursorTS
	}
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, created_at, channel_id, channel_type, template_id, recipient, rendered_subject, '', status, error, sender_kind, sender_id, test, sent_at, template_key
		FROM notification_log WHERE tenant_id = $1
		AND ($2 = '' OR channel_id::text = $2) AND ($3 = '' OR template_id::text = $3) AND ($4 = '' OR recipient ILIKE '%' || $4 || '%')
		AND ($5 = '' OR status = $5) AND ($6 = '' OR sender_id = $6) AND created_at >= $7 AND created_at <= $8
		AND ($9::timestamptz IS NULL OR (created_at, id) < ($9, $10::uuid))
		ORDER BY created_at DESC, id DESC LIMIT $11`, tenantID, f.ChannelID, f.TemplateID, escapeLike(f.Recipient), f.Status, f.SenderID, f.From, to, cursorTS, nullIfEmpty(f.CursorID), f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogRow
	for rows.Next() {
		l, err := scanLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ExpirePendingLogs marks entries stuck in pending as failed (system scope).
func ExpirePendingLogs(ctx context.Context, tx pgx.Tx, olderThan time.Time) (int64, error) {
	ct, err := tx.Exec(ctx, "UPDATE notification_log SET status = 'failed', error = 'interrupted' WHERE status = 'pending' AND created_at < $1", olderThan)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// ---------------------------------------------------------------- grants

const grantCols = "id, tenant_id, resource_type, resource_id, subject_type, subject_id, relation, granted_by, granted_at, expires_at"

func scanGrant(r pgx.Row) (Grant, error) {
	var g Grant
	err := r.Scan(&g.ID, &g.TenantID, &g.ResourceType, &g.ResourceID, &g.SubjectType, &g.SubjectID, &g.Relation, &g.GrantedBy, &g.GrantedAt, &g.ExpiresAt)
	return g, notFound(err)
}

func scanGrants(rows pgx.Rows) ([]Grant, error) {
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpsertGrant creates or replaces the grant for (resource, subject).
func UpsertGrant(ctx context.Context, tx pgx.Tx, g Grant) (Grant, error) {
	return scanGrant(tx.QueryRow(ctx, `INSERT INTO grants (id, tenant_id, resource_type, resource_id, subject_type, subject_id, relation, granted_by, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tenant_id, resource_type, resource_id, subject_type, subject_id) DO UPDATE SET relation = EXCLUDED.relation, granted_by = EXCLUDED.granted_by, granted_at = now(), expires_at = EXCLUDED.expires_at
		RETURNING `+grantCols, g.ID, g.TenantID, g.ResourceType, g.ResourceID, g.SubjectType, g.SubjectID, g.Relation, g.GrantedBy, g.ExpiresAt))
}

// GetGrant by id.
func GetGrant(ctx context.Context, tx pgx.Tx, tenantID, id string) (Grant, error) {
	return scanGrant(tx.QueryRow(ctx, "SELECT "+grantCols+" FROM grants WHERE tenant_id = $1 AND id = $2", tenantID, id))
}

// DeleteGrant revokes.
func DeleteGrant(ctx context.Context, tx pgx.Tx, tenantID, id string) error {
	ct, err := tx.Exec(ctx, "DELETE FROM grants WHERE tenant_id = $1 AND id = $2", tenantID, id)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// GrantsOnResource lists the grants on one resource.
func GrantsOnResource(ctx context.Context, tx pgx.Tx, tenantID, resourceType, resourceID string) ([]Grant, error) {
	rows, err := tx.Query(ctx, "SELECT "+grantCols+" FROM grants WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3 ORDER BY granted_at", tenantID, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

// GrantsForSubjects lists unexpired grants held by any of the subjects.
func GrantsForSubjects(ctx context.Context, tx pgx.Tx, tenantID, userID string, roles []string, now time.Time) ([]Grant, error) {
	rows, err := tx.Query(ctx, "SELECT "+grantCols+` FROM grants WHERE tenant_id = $1
		AND ((subject_type = 'user' AND subject_id = $2) OR (subject_type = 'role' AND subject_id = ANY($3::text[])) OR subject_type = 'tenant')
		AND (expires_at IS NULL OR expires_at > $4) ORDER BY granted_at`, tenantID, userID, nonNil(roles), now)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

// DeleteGrantsOfResource removes every grant on a resource.
func DeleteGrantsOfResource(ctx context.Context, tx pgx.Tx, tenantID, resourceType, resourceID string) error {
	_, err := tx.Exec(ctx, "DELETE FROM grants WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3", tenantID, resourceType, resourceID)
	return err
}

// DeleteGrantsOfSubject removes a specific (resource, subject) tuple.
func DeleteGrantsOfSubject(ctx context.Context, tx pgx.Tx, tenantID, resourceType, resourceID, subjectType, subjectID string) error {
	_, err := tx.Exec(ctx, "DELETE FROM grants WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3 AND subject_type = $4 AND subject_id = $5", tenantID, resourceType, resourceID, subjectType, subjectID)
	return err
}

// ---------------------------------------------------------------- categories

const categoryCols = "k.id, k.tenant_id, k.name, k.description, k.sort, k.created_by, k.updated_by, k.created_at, k.updated_at, (SELECT count(*) FROM messages m WHERE m.category_id = k.id)"

func scanCategory(r pgx.Row) (Category, error) {
	var c Category
	err := r.Scan(&c.ID, &c.TenantID, &c.Name, &c.Description, &c.Sort, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt, &c.MessageCount)
	return c, notFound(err)
}

// InsertCategory creates a category.
func InsertCategory(ctx context.Context, tx pgx.Tx, c Category) error {
	_, err := tx.Exec(ctx, `INSERT INTO message_categories (id, tenant_id, name, description, sort, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$6)`,
		c.ID, c.TenantID, c.Name, c.Description, c.Sort, c.CreatedBy)
	return conflict(err)
}

// GetCategory by tenant + id.
func GetCategory(ctx context.Context, tx pgx.Tx, tenantID, id string) (Category, error) {
	return scanCategory(tx.QueryRow(ctx, "SELECT "+categoryCols+" FROM message_categories k WHERE k.tenant_id = $1 AND k.id = $2", tenantID, id))
}

// ListCategories orders by sort then name.
func ListCategories(ctx context.Context, tx pgx.Tx, tenantID string) ([]Category, error) {
	rows, err := tx.Query(ctx, "SELECT "+categoryCols+" FROM message_categories k WHERE k.tenant_id = $1 ORDER BY k.sort, lower(k.name), k.id LIMIT 1000", tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCategory rewrites the mutable columns.
func UpdateCategory(ctx context.Context, tx pgx.Tx, c Category) error {
	ct, err := tx.Exec(ctx, "UPDATE message_categories SET name = $3, description = $4, sort = $5, updated_by = $6, updated_at = now() WHERE tenant_id = $1 AND id = $2",
		c.TenantID, c.ID, c.Name, c.Description, c.Sort, c.UpdatedBy)
	if err != nil {
		return conflict(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCategory removes a category; messages referencing it make it ErrConflict.
func DeleteCategory(ctx context.Context, tx pgx.Tx, tenantID, id string) error {
	ct, err := tx.Exec(ctx, "DELETE FROM message_categories WHERE tenant_id = $1 AND id = $2", tenantID, id)
	if err != nil {
		return restricted(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- messages

const messageCols = `m.id, m.tenant_id, m.title, m.content, m.type, m.status, m.category_id, m.sender_id, m.sender_service, m.recipients, m.scheduled_at, m.lease_until, m.published_at,
	m.created_by, m.updated_by, m.created_at, m.updated_at, COALESCE(k.name, ''),
	(SELECT count(*) FROM inbox i WHERE i.message_id = m.id), (SELECT count(*) FROM inbox i WHERE i.message_id = m.id AND i.read_at IS NOT NULL)`
const messageFrom = " FROM messages m LEFT JOIN message_categories k ON k.id = m.category_id "

func scanMessage(r pgx.Row) (Message, error) {
	var m Message
	err := r.Scan(&m.ID, &m.TenantID, &m.Title, &m.Content, &m.Type, &m.Status, &m.CategoryID, &m.SenderID, &m.SenderService, &m.Recipients, &m.ScheduledAt, &m.LeaseUntil, &m.PublishedAt,
		&m.CreatedBy, &m.UpdatedBy, &m.CreatedAt, &m.UpdatedAt, &m.CategoryName, &m.RecipientCount, &m.ReadCount)
	return m, notFound(err)
}

func scanMessages(rows pgx.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertMessage creates a message.
func InsertMessage(ctx context.Context, tx pgx.Tx, m Message) error {
	_, err := tx.Exec(ctx, `INSERT INTO messages (id, tenant_id, title, content, type, status, category_id, sender_id, sender_service, recipients, scheduled_at, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`, m.ID, m.TenantID, m.Title, m.Content, m.Type, m.Status, m.CategoryID, m.SenderID, m.SenderService, jsonOrEmpty(m.Recipients), m.ScheduledAt, m.CreatedBy)
	return restricted(err)
}

// GetMessage by tenant + id.
func GetMessage(ctx context.Context, tx pgx.Tx, tenantID, id string) (Message, error) {
	return scanMessage(tx.QueryRow(ctx, "SELECT "+messageCols+messageFrom+"WHERE m.tenant_id = $1 AND m.id = $2", tenantID, id))
}

// ListMessages pages newest first under the filter.
func ListMessages(ctx context.Context, tx pgx.Tx, tenantID string, f MessageFilter) ([]Message, error) {
	var cursorTS *time.Time
	if !f.CursorTS.IsZero() {
		cursorTS = &f.CursorTS
	}
	rows, err := tx.Query(ctx, "SELECT "+messageCols+messageFrom+`WHERE m.tenant_id = $1 AND ($2 = '' OR m.status = $2) AND ($3 = '' OR m.category_id::text = $3)
		AND ($4 = '' OR m.title ILIKE '%' || $4 || '%') AND ($5 = '' OR m.sender_id::text = $5)
		AND ($6::timestamptz IS NULL OR (m.created_at, m.id) < ($6, $7::uuid))
		ORDER BY m.created_at DESC, m.id DESC LIMIT $8`, tenantID, f.Status, f.CategoryID, escapeLike(f.Q), f.SenderID, cursorTS, nullIfEmpty(f.CursorID), f.Limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// UpdateMessage rewrites the editable columns (draft/scheduled only, enforced by the caller).
func UpdateMessage(ctx context.Context, tx pgx.Tx, m Message) error {
	ct, err := tx.Exec(ctx, `UPDATE messages SET title = $3, content = $4, type = $5, category_id = $6, recipients = $7, scheduled_at = $8, status = $9, updated_by = $10, updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, m.TenantID, m.ID, m.Title, m.Content, m.Type, m.CategoryID, jsonOrEmpty(m.Recipients), m.ScheduledAt, m.Status, m.UpdatedBy)
	if err != nil {
		return restricted(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetMessageStatus moves a message between states, clearing the lease and
// setting published_at when given.
func SetMessageStatus(ctx context.Context, tx pgx.Tx, tenantID, id, status string, publishedAt *time.Time) error {
	ct, err := tx.Exec(ctx, "UPDATE messages SET status = $3, lease_until = NULL, published_at = COALESCE($4, published_at), updated_at = now() WHERE tenant_id = $1 AND id = $2", tenantID, id, status, publishedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMessage removes a message and (by cascade) its inbox rows.
func DeleteMessage(ctx context.Context, tx pgx.Tx, tenantID, id string) error {
	ct, err := tx.Exec(ctx, "DELETE FROM messages WHERE tenant_id = $1 AND id = $2", tenantID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimDueMessages leases due scheduled messages (system scope): each row is
// handed to exactly one claimer until the lease expires (research R5).
func ClaimDueMessages(ctx context.Context, tx pgx.Tx, now time.Time, lease time.Duration, limit int) ([]Message, error) {
	rows, err := tx.Query(ctx, `WITH due AS (
			SELECT id FROM messages WHERE status IN ('scheduled','publishing') AND scheduled_at <= $1 AND (lease_until IS NULL OR lease_until < $1)
			ORDER BY scheduled_at LIMIT $3 FOR UPDATE SKIP LOCKED)
		UPDATE messages m SET status = 'publishing', lease_until = $2, updated_at = now() FROM due WHERE m.id = due.id
		RETURNING m.id, m.tenant_id`, now, now.Add(lease), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.TenantID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- inbox

const inboxCols = "i.id, i.tenant_id, i.message_id, i.recipient_id, i.status, i.read_at, i.created_at, i.updated_at"

// InsertInboxBatch adds one row per recipient, skipping existing (message, recipient) pairs.
func InsertInboxBatch(ctx context.Context, tx pgx.Tx, rows []InboxRow) (int64, error) {
	var n int64
	for _, r := range rows {
		ct, err := tx.Exec(ctx, `INSERT INTO inbox (id, tenant_id, message_id, recipient_id, status) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (message_id, recipient_id) DO NOTHING`,
			r.ID, r.TenantID, r.MessageID, r.RecipientID, r.Status)
		if err != nil {
			return n, err
		}
		n += ct.RowsAffected()
	}
	return n, nil
}

// InboxPage lists a recipient's visible entries newest first with the joined
// message. status: "" (all visible), "unread", "read".
func InboxPage(ctx context.Context, tx pgx.Tx, tenantID, recipientID, status string, cursorTS time.Time, cursorID string, limit int) ([]InboxRow, error) {
	var cts *time.Time
	if !cursorTS.IsZero() {
		cts = &cursorTS
	}
	rows, err := tx.Query(ctx, "SELECT "+inboxCols+", "+messageCols+" FROM inbox i JOIN messages m ON m.id = i.message_id LEFT JOIN message_categories k ON k.id = m.category_id"+`
		WHERE i.tenant_id = $1 AND i.recipient_id = $2 AND i.status <> 'deleted' AND (i.status <> 'revoked' OR i.read_at IS NOT NULL)
		AND ($3 = '' OR ($3 = 'unread' AND i.status IN ('sent','received')) OR ($3 = 'read' AND i.status = 'read'))
		AND ($4::timestamptz IS NULL OR (i.created_at, i.id) < ($4, $5::uuid))
		ORDER BY i.created_at DESC, i.id DESC LIMIT $6`, tenantID, recipientID, status, cts, nullIfEmpty(cursorID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxRow
	for rows.Next() {
		var r InboxRow
		var m Message
		if err := rows.Scan(&r.ID, &r.TenantID, &r.MessageID, &r.RecipientID, &r.Status, &r.ReadAt, &r.CreatedAt, &r.UpdatedAt,
			&m.ID, &m.TenantID, &m.Title, &m.Content, &m.Type, &m.Status, &m.CategoryID, &m.SenderID, &m.SenderService, &m.Recipients, &m.ScheduledAt, &m.LeaseUntil, &m.PublishedAt,
			&m.CreatedBy, &m.UpdatedBy, &m.CreatedAt, &m.UpdatedAt, &m.CategoryName, &m.RecipientCount, &m.ReadCount); err != nil {
			return nil, err
		}
		r.Message = &m
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetInboxEntry loads one visible entry of the recipient with its message.
func GetInboxEntry(ctx context.Context, tx pgx.Tx, tenantID, recipientID, id string) (InboxRow, error) {
	row := tx.QueryRow(ctx, "SELECT "+inboxCols+", "+messageCols+" FROM inbox i JOIN messages m ON m.id = i.message_id LEFT JOIN message_categories k ON k.id = m.category_id"+
		" WHERE i.tenant_id = $1 AND i.recipient_id = $2 AND i.id = $3 AND i.status <> 'deleted' AND (i.status <> 'revoked' OR i.read_at IS NOT NULL)", tenantID, recipientID, id)
	var r InboxRow
	var m Message
	err := row.Scan(&r.ID, &r.TenantID, &r.MessageID, &r.RecipientID, &r.Status, &r.ReadAt, &r.CreatedAt, &r.UpdatedAt,
		&m.ID, &m.TenantID, &m.Title, &m.Content, &m.Type, &m.Status, &m.CategoryID, &m.SenderID, &m.SenderService, &m.Recipients, &m.ScheduledAt, &m.LeaseUntil, &m.PublishedAt,
		&m.CreatedBy, &m.UpdatedBy, &m.CreatedAt, &m.UpdatedAt, &m.CategoryName, &m.RecipientCount, &m.ReadCount)
	if err != nil {
		return r, notFound(err)
	}
	r.Message = &m
	return r, nil
}

// InboxUnread counts sent/received entries of the recipient.
func InboxUnread(ctx context.Context, tx pgx.Tx, tenantID, recipientID string) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, "SELECT count(*) FROM inbox WHERE tenant_id = $1 AND recipient_id = $2 AND status IN ('sent','received')", tenantID, recipientID).Scan(&n)
	return n, err
}

// SetInboxStatus updates the recipient's own entries among ids; read sets
// read_at, unread clears it; rows already deleted or revoked are untouched.
func SetInboxStatus(ctx context.Context, tx pgx.Tx, tenantID, recipientID string, ids []string, status string) (int64, error) {
	var q string
	switch status {
	case "read":
		q = "UPDATE inbox SET status = 'read', read_at = COALESCE(read_at, now()), updated_at = now() WHERE tenant_id = $1 AND recipient_id = $2 AND id = ANY($3::uuid[]) AND status IN ('sent','received','read')"
	case "unread":
		q = "UPDATE inbox SET status = 'sent', read_at = NULL, updated_at = now() WHERE tenant_id = $1 AND recipient_id = $2 AND id = ANY($3::uuid[]) AND status IN ('sent','received','read')"
	case "received":
		q = "UPDATE inbox SET status = 'received', updated_at = now() WHERE tenant_id = $1 AND recipient_id = $2 AND id = ANY($3::uuid[]) AND status = 'sent'"
	case "deleted":
		q = "UPDATE inbox SET status = 'deleted', updated_at = now() WHERE tenant_id = $1 AND recipient_id = $2 AND id = ANY($3::uuid[]) AND status <> 'deleted'"
	default:
		return 0, fmt.Errorf("store: inbox status %q", status)
	}
	ct, err := tx.Exec(ctx, q, tenantID, recipientID, nonNil(ids))
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// RevokeUnread marks every unread entry of a message revoked; read entries
// are marked revoked but stay visible (read_at set). Returns the recipients affected.
func RevokeUnread(ctx context.Context, tx pgx.Tx, tenantID, messageID string) ([]string, error) {
	rows, err := tx.Query(ctx, "UPDATE inbox SET status = 'revoked', updated_at = now() WHERE tenant_id = $1 AND message_id = $2 AND status IN ('sent','received','read') RETURNING recipient_id", tenantID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MessageRecipients lists the inbox rows of a message (sender view), paged by id.
func MessageRecipients(ctx context.Context, tx pgx.Tx, tenantID, messageID, status, afterID string, limit int) ([]InboxRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+inboxCols+" FROM inbox i WHERE i.tenant_id = $1 AND i.message_id = $2 AND ($3 = '' OR i.status = $3) AND i.id::text > $4 ORDER BY i.id LIMIT $5",
		tenantID, messageID, status, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxRow
	for rows.Next() {
		var r InboxRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.MessageID, &r.RecipientID, &r.Status, &r.ReadAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- audit & stats

// InsertAuditRows writes a batch.
func InsertAuditRows(ctx context.Context, tx pgx.Tx, rows []AuditRow) error {
	for _, r := range rows {
		if _, err := tx.Exec(ctx, `INSERT INTO notification_audit_events (ts, tenant_id, event_type, actor_kind, actor_id, subject_kind, subject_id, outcome, reason, correlation_id, details)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, r.TS, r.TenantID, r.EventType, r.ActorKind, r.ActorID, r.SubjectKind, r.SubjectID, r.Outcome, r.Reason, r.CorrelationID, jsonOrEmpty(r.Details)); err != nil {
			return err
		}
	}
	return nil
}

// QueryAudit pages events newest first; cursor = ts of the last row seen.
// Live subjects resolve to a name: channels and templates by name, messages
// by title. Ids are only cast when they look like UUIDs.
func QueryAudit(ctx context.Context, tx pgx.Tx, tenantID, eventType, actorID string, from, to, cursor time.Time, limit int) ([]AuditRow, error) {
	rows, err := tx.Query(ctx, `SELECT a.ts, a.tenant_id, a.event_type, a.actor_kind, a.actor_id, a.subject_kind, a.subject_id, a.outcome, a.reason, a.correlation_id, a.details,
		COALESCE(CASE WHEN a.subject_id !~ '`+uuidRE+`' THEN NULL
			WHEN a.subject_kind = 'channel' THEN (SELECT c.name FROM channels c WHERE c.tenant_id = a.tenant_id AND c.id = a.subject_id::uuid)
			WHEN a.subject_kind = 'template' THEN (SELECT t.name FROM templates t WHERE t.tenant_id = a.tenant_id AND t.id = a.subject_id::uuid)
			WHEN a.subject_kind = 'message' THEN (SELECT m.title FROM messages m WHERE m.tenant_id = a.tenant_id AND m.id = a.subject_id::uuid) END, '')
		FROM notification_audit_events a WHERE a.tenant_id = $1 AND ($2 = '' OR a.event_type = $2) AND ($3 = '' OR a.actor_id = $3)
		AND a.ts >= $4 AND a.ts <= $5 AND ($6::timestamptz IS NULL OR a.ts < $6) ORDER BY a.ts DESC LIMIT $7`,
		tenantID, eventType, actorID, from, to, nullTime(cursor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.TS, &r.TenantID, &r.EventType, &r.ActorKind, &r.ActorID, &r.SubjectKind, &r.SubjectID, &r.Outcome, &r.Reason, &r.CorrelationID, &r.Details, &r.SubjectName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// TenantStats computes the per-tenant counts.
func TenantStats(ctx context.Context, tx pgx.Tx, tenantID string, now time.Time) (Stats, error) {
	st := Stats{Notifications: map[string]int64{}, Messages: map[string]int64{}}
	since := now.Add(-24 * time.Hour)
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM channels WHERE tenant_id = $1),
		(SELECT count(*) FROM templates WHERE tenant_id = $1),
		(SELECT count(*) FROM notification_audit_events WHERE tenant_id = $1 AND ts >= $2)`, tenantID, since).
		Scan(&st.Channels, &st.Templates, &st.Operations24h); err != nil {
		return st, err
	}
	rows, err := tx.Query(ctx, "SELECT status, count(*) FROM notification_log WHERE tenant_id = $1 AND created_at >= $2 GROUP BY status", tenantID, since)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return st, err
		}
		st.Notifications[k] = n
	}
	rows.Close()
	rows, err = tx.Query(ctx, "SELECT status, count(*) FROM messages WHERE tenant_id = $1 GROUP BY status", tenantID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return st, err
		}
		st.Messages[k] = n
	}
	return st, rows.Err()
}
