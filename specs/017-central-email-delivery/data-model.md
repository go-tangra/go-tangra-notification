# Data Model: Central Email Delivery (017)

## notification

### Migration `0005_system_email.sql`

```sql
-- +goose Up
ALTER TABLE channels  ADD COLUMN managed boolean NOT NULL DEFAULT false;
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
-- +goose Down is not provided (forward-only, platform convention)
```

RLS and grants: unchanged (new columns inherit the table policies).

### Channel (extended)

| Field | Rule |
|---|---|
| managed | true only for the platform channel created from `platform_email`; at most one per tenant |
| settings | same email schema as today; `password` sealed |

- Managed channel: API/UI update and delete → `FailedPrecondition managed_channel`;
  read and test send allowed; `is_default=true`, `enabled=true` for type email
  in the platform tenant, with the tenant-wide `use` grant (existing
  `SetTenantUse`).
- Start-up upsert (system scope): find `managed` channel in the platform
  tenant; insert or update settings/name (`Platform email`); if the config
  block is absent, an existing managed channel is **disabled** (not deleted:
  templates/log rows may reference it) and a warning logged.

### Template (extended)

| Field | Rule |
|---|---|
| system_key | `<service>.<name>`; set only by seeding; immutable |
| builtin_subject / builtin_body | built-in wording; refreshed on upgrade seeding (only these two columns, never subject/body) |
| required_variables | subset of `variables`; must stay referenced in subject or body on save |
| secret_variables | subset of `variables`; redacted in the stored log |

- System templates: tenant = platform, `channel_id NULL`, `channel_type email`,
  `is_default false`; delete → `FailedPrecondition system_template`;
  `name` = key; update allowed for subject/body only (name, variables,
  channel fixed).
- `restore` copies builtin_subject/body into subject/body.

Seed set: see research D3.

### Notification log entry (extended)

| Field | Rule |
|---|---|
| template_key | set for key sends |
| rendered_subject / rendered_body | secret variables replaced by `[redacted]` (double rendering) |
| error | scrubbed of channel secrets and of secret variable values |

## auth

### Migration `0009_outbox_retire.sql`

```sql
-- +goose Up
ALTER TABLE outbox ADD COLUMN failed_at  timestamptz;
ALTER TABLE outbox ADD COLUMN last_error text CHECK (char_length(last_error) <= 200);
DROP INDEX outbox_pending_idx;
CREATE INDEX outbox_pending_idx ON outbox (next_attempt_at)
  WHERE sent_at IS NULL AND failed_at IS NULL;
```

### Queued message (extended)

States: `pending` (sent_at NULL, failed_at NULL) → `sent` (sent_at) |
`failed` (failed_at). Claim sets
`next_attempt_at = now() + least(interval '30 seconds' * 2^attempts, interval '1 hour')`.

Payload (sealed, AAD `email:<to>` unchanged):
- v2: `{"v":2,"template":"auth.invite","vars":{"link":"…","valid_for":"72 hours","tenant":"…"}}`
- legacy (no `v`): `{"subject":"…","text":"…"}` → sent as `auth.message`.

## warden

No schema change. `share.Message` becomes `{To, Template, Vars}`.

## go-tangra-docker

`configs/notification.yaml`:

```yaml
platform_email:
  host: mailpit
  port: 1025
  tls: none
  allow_plaintext: true        # development only
  from: tangra@example.org
  # username: …               # only with tls implicit|starttls
  # password_file: /run/secrets/smtp.password
```
