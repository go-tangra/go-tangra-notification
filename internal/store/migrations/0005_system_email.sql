-- +goose Up
-- Central email delivery (feature 017): the configuration-managed platform
-- channel, system templates addressed by key, and the key on log entries.
-- Row-level security and grants apply to the new columns through their tables.
ALTER TABLE channels ADD COLUMN managed boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX channels_one_managed ON channels (tenant_id) WHERE managed;

ALTER TABLE templates ADD COLUMN system_key         text;
ALTER TABLE templates ADD COLUMN builtin_subject    text;
ALTER TABLE templates ADD COLUMN builtin_body       text;
ALTER TABLE templates ADD COLUMN required_variables text[] NOT NULL DEFAULT '{}';
ALTER TABLE templates ADD COLUMN secret_variables   text[] NOT NULL DEFAULT '{}';
ALTER TABLE templates ADD CONSTRAINT templates_system_key_chk
  CHECK (system_key IS NULL OR system_key ~ '^[a-z][a-z0-9]*\.[a-z][a-z0-9_]{0,62}$');
CREATE UNIQUE INDEX templates_system_key ON templates (tenant_id, system_key)
  WHERE system_key IS NOT NULL;

ALTER TABLE notification_log ADD COLUMN template_key text;

-- Forward-only (platform convention): no Down migration.
