-- +goose Up
-- Feature 032 (server-side tables): indexes only where a sortable field of a
-- potentially large list lacks one. Existing indexes already serve the
-- channel/template name order (unique lower(name)), the message default
-- order (tenant_id, created_at DESC, id DESC), the log (tenant_id,
-- created_at DESC; status) and the audit trail (tenant_id, ts DESC).
-- Channels, templates and categories are small configuration tables.
CREATE INDEX IF NOT EXISTS messages_tenant_lower_title ON messages (tenant_id, lower(title), id);
-- Members without messages:manage list their own messages, newest first.
CREATE INDEX IF NOT EXISTS messages_tenant_sender_created ON messages (tenant_id, sender_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS messages_tenant_sender_created;
DROP INDEX IF EXISTS messages_tenant_lower_title;
