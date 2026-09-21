-- +goose Up
CREATE TABLE notification_log (
  id               uuid NOT NULL,
  tenant_id        uuid NOT NULL,
  created_at       timestamptz NOT NULL,
  channel_id       uuid NOT NULL,
  channel_type     text NOT NULL,
  template_id      uuid,
  recipient        text NOT NULL CHECK (length(recipient) <= 512),
  rendered_subject text NOT NULL DEFAULT '',
  rendered_body    text NOT NULL DEFAULT '',
  status           text NOT NULL CHECK (status IN ('pending','sent','failed')),
  error            text NOT NULL DEFAULT '',
  sender_kind      text NOT NULL CHECK (sender_kind IN ('user','service')),
  sender_id        text NOT NULL DEFAULT '',
  test             boolean NOT NULL DEFAULT false,
  sent_at          timestamptz
);
SELECT create_hypertable('notification_log', 'created_at', chunk_time_interval => INTERVAL '7 days');
CREATE UNIQUE INDEX notification_log_id ON notification_log (tenant_id, id, created_at);
CREATE INDEX notification_log_tenant ON notification_log (tenant_id, created_at DESC);
CREATE INDEX notification_log_channel ON notification_log (tenant_id, channel_id, created_at DESC);
CREATE INDEX notification_log_template ON notification_log (tenant_id, template_id, created_at DESC);
CREATE INDEX notification_log_sender ON notification_log (tenant_id, sender_id, created_at DESC);
CREATE INDEX notification_log_status ON notification_log (tenant_id, status, created_at DESC);
SELECT add_retention_policy('notification_log', INTERVAL '400 days');

CREATE TABLE notification_audit_events (
  ts             timestamptz NOT NULL,
  tenant_id      uuid NOT NULL,
  event_type     text NOT NULL,
  actor_kind     text NOT NULL CHECK (actor_kind IN ('user','service','system')),
  actor_id       text NOT NULL DEFAULT '',
  subject_kind   text NOT NULL DEFAULT '',
  subject_id     text NOT NULL DEFAULT '',
  outcome        text NOT NULL CHECK (outcome IN ('ok','refused','failed')),
  reason         text NOT NULL DEFAULT '',
  correlation_id text NOT NULL DEFAULT '',
  details        jsonb NOT NULL DEFAULT '{}'::jsonb
);
SELECT create_hypertable('notification_audit_events', 'ts', chunk_time_interval => INTERVAL '7 days');
CREATE INDEX notification_audit_tenant_ts ON notification_audit_events (tenant_id, ts DESC);
CREATE INDEX notification_audit_type_ts ON notification_audit_events (tenant_id, event_type, ts DESC);
SELECT add_retention_policy('notification_audit_events', INTERVAL '400 days');

-- +goose Down
DROP TABLE IF EXISTS notification_audit_events;
DROP TABLE IF EXISTS notification_log;
