-- +goose Up
CREATE TABLE channels (
  id              uuid PRIMARY KEY,
  tenant_id       uuid NOT NULL,
  name            text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  type            text NOT NULL CHECK (type IN ('email','sms','slack','sse')),
  settings_sealed bytea NOT NULL,
  settings_public jsonb NOT NULL DEFAULT '{}'::jsonb,
  enabled         boolean NOT NULL DEFAULT false,
  is_default      boolean NOT NULL DEFAULT false,
  created_by      uuid,
  updated_by      uuid,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX channels_tenant_name ON channels (tenant_id, lower(name));
CREATE UNIQUE INDEX channels_tenant_default ON channels (tenant_id, type) WHERE is_default;

CREATE TABLE templates (
  id           uuid PRIMARY KEY,
  tenant_id    uuid NOT NULL,
  name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  channel_id   uuid REFERENCES channels(id) ON DELETE RESTRICT,
  channel_type text NOT NULL,
  subject      text NOT NULL DEFAULT '' CHECK (length(subject) <= 998),
  body         text NOT NULL DEFAULT '' CHECK (length(body) <= 262144),
  variables    text[] NOT NULL DEFAULT '{}',
  is_default   boolean NOT NULL DEFAULT false,
  created_by   uuid,
  updated_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX templates_tenant_name ON templates (tenant_id, lower(name));
CREATE UNIQUE INDEX templates_channel_default ON templates (tenant_id, channel_id) WHERE is_default AND channel_id IS NOT NULL;
CREATE INDEX templates_tenant_channel ON templates (tenant_id, channel_id);

CREATE TABLE grants (
  id            uuid PRIMARY KEY,
  tenant_id     uuid NOT NULL,
  resource_type text NOT NULL CHECK (resource_type IN ('channel','template')),
  resource_id   uuid NOT NULL,
  subject_type  text NOT NULL CHECK (subject_type IN ('user','role','tenant')),
  subject_id    text NOT NULL DEFAULT '',
  relation      text NOT NULL CHECK (relation IN ('owner','editor','viewer','sharer')),
  granted_by    uuid,
  granted_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz,
  UNIQUE (tenant_id, resource_type, resource_id, subject_type, subject_id)
);
CREATE INDEX grants_resource ON grants (tenant_id, resource_type, resource_id);
CREATE INDEX grants_subject ON grants (tenant_id, subject_type, subject_id);

CREATE TABLE message_categories (
  id          uuid PRIMARY KEY,
  tenant_id   uuid NOT NULL,
  name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  description text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  sort        integer NOT NULL DEFAULT 0,
  created_by  uuid,
  updated_by  uuid,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX categories_tenant_name ON message_categories (tenant_id, lower(name));

CREATE TABLE messages (
  id             uuid PRIMARY KEY,
  tenant_id      uuid NOT NULL,
  title          text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  content        text NOT NULL DEFAULT '' CHECK (length(content) <= 65536),
  type           text NOT NULL CHECK (type IN ('notification','private','group')),
  status         text NOT NULL CHECK (status IN ('draft','scheduled','publishing','published','revoked','archived')),
  category_id    uuid REFERENCES message_categories(id) ON DELETE RESTRICT,
  sender_id      uuid,
  sender_service text NOT NULL DEFAULT '',
  recipients     jsonb NOT NULL DEFAULT '{}'::jsonb,
  scheduled_at   timestamptz,
  lease_until    timestamptz,
  published_at   timestamptz,
  created_by     uuid,
  updated_by     uuid,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_tenant_created ON messages (tenant_id, created_at DESC, id DESC);
CREATE INDEX messages_due ON messages (status, scheduled_at) WHERE status = 'scheduled';

CREATE TABLE inbox (
  id           uuid PRIMARY KEY,
  tenant_id    uuid NOT NULL,
  message_id   uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  recipient_id uuid NOT NULL,
  status       text NOT NULL CHECK (status IN ('sent','received','read','revoked','deleted')),
  read_at      timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (message_id, recipient_id)
);
CREATE INDEX inbox_recipient ON inbox (tenant_id, recipient_id, status, created_at DESC);
CREATE INDEX inbox_message ON inbox (tenant_id, message_id, status);

-- +goose Down
DROP TABLE IF EXISTS inbox;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS message_categories;
DROP TABLE IF EXISTS grants;
DROP TABLE IF EXISTS templates;
DROP TABLE IF EXISTS channels;
