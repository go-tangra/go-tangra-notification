# Data Model: Notification Service

**Feature**: 006-notification-service · **Storage**: TimescaleDB database
`notification` (role `notification_app`, RLS on every table via
`app_tenant_matches(tenant_id)` as in warden), Valkey for live events and
counters.

All ids are UUIDv7 (`store.NewID()`); `tenant_id` and user ids are the auth
service's UUIDs; times are `timestamptz` (ISO-8601 in JSON). `created_by` /
`updated_by` are user ids (NULL for service actors, whose SPIFFE id goes to
the audit event).

## channels

| Column | Type | Notes |
|--------|------|-------|
| id | uuid PK | |
| tenant_id | uuid | RLS |
| name | text | 1–100, unique per tenant (`lower(name)`) |
| type | text | `email` \| `sms` \| `slack` \| `sse` |
| settings_sealed | bytea | envelope-encrypted JSON (research R1); ≤ 8 KiB clear |
| settings_public | jsonb | non-secret settings copied for listings (host, port, from, reply_to, tls) |
| enabled | bool | default false |
| is_default | bool | partial unique index `(tenant_id, type) WHERE is_default` |
| created_by, updated_by | uuid NULL | |
| created_at, updated_at | timestamptz | |

Validation per type at save (`channel.Provider.Validate(settings)`): email
requires `host`, `port` (1–65535), `from` (single RFC 5322 address); `tls`
∈ `implicit|starttls|none` (`none` only with `smtp.allow_plaintext`);
`username`/`password` optional together; `reply_to` optional. Credential
fields per provider: email `password`; sms/slack `api_key`, `token`.

Deleting is refused while `templates.channel_id` references the row
(`ON DELETE RESTRICT`, reported as `conflict` with the count).

## templates

| Column | Type | Notes |
|--------|------|-------|
| id | uuid PK | |
| tenant_id | uuid | RLS |
| name | text | 1–100, unique per tenant |
| channel_id | uuid NULL → channels | NULL after an import with an unknown channel ("needs a channel") |
| channel_type | text | copied from the channel at save; sends require channel type equality |
| subject | text | ≤ 998, `text/template` |
| body | text | ≤ 256 KiB, `html/template` (email) or `text/template` |
| variables | text[] | declared names, `^[A-Za-z][A-Za-z0-9_]{0,63}$`, ≤ 50 |
| is_default | bool | partial unique `(tenant_id, channel_id) WHERE is_default` |
| created_by, updated_by, created_at, updated_at | | |

Saving validates subject and body (research R3): syntax error → `validation_failed`
with `{position}`; undeclared reference → `validation_failed` with `{variable}`.

## notification_log (hypertable on `created_at`, 7-day chunks, 400-day retention)

| Column | Type | Notes |
|--------|------|-------|
| id | uuid | |
| tenant_id | uuid | RLS |
| created_at | timestamptz | partition key |
| channel_id | uuid | not a FK (channels may be deleted later) |
| channel_type | text | |
| template_id | uuid NULL | NULL for test sends |
| recipient | text | ≤ 512 |
| rendered_subject | text | |
| rendered_body | text | ≤ 1 MiB |
| status | text | `pending` \| `sent` \| `failed` |
| error | text | scrubbed provider reason, ≤ 1 KiB |
| sender_kind, sender_id | text | `user`/`service` + id |
| test | bool | |
| sent_at | timestamptz NULL | |

Indexes: `(tenant_id, created_at DESC)`, `(tenant_id, channel_id, created_at
DESC)`, `(tenant_id, template_id, created_at DESC)`, `(tenant_id, sender_id,
created_at DESC)`, `(tenant_id, status, created_at DESC)`. Rows are never
updated after reaching `sent`/`failed`. A maintenance query (every minute)
sets `pending` rows older than 10 minutes to `failed`, `error='interrupted'`.

## grants

Same shape as warden's `grants`: `id, tenant_id, resource_type
(channel|template), resource_id, subject_type (user|role|tenant), subject_id
(user id, role slug, ''), relation (owner|editor|viewer|sharer), granted_by,
expires_at NULL, created_at`; `UNIQUE (tenant_id, resource_type, resource_id,
subject_type, subject_id)`; index `(tenant_id, resource_type, resource_id)`
and `(tenant_id, subject_type, subject_id)`. Relation → actions: owner
`read,write,delete,share,use`; editor `read,write,use`; viewer `read`; sharer
`read,share,use`. The creator gets an `owner` grant; a default channel gets a
`tenant` grant with relation `sharer` (read/use) that is removed when the
default flag moves (research R7). Grants of a deleted resource are deleted
with it.

## message_categories

`id, tenant_id, name (1–100, unique per tenant), description (≤ 500), sort
(int), created_by, updated_by, created_at, updated_at`. Deleting is refused
while messages reference it.

## messages

| Column | Type | Notes |
|--------|------|-------|
| id | uuid PK | |
| tenant_id | uuid | RLS |
| title | text | 1–200 |
| content | text | ≤ 64 KiB (plain text as given; rendered as text in the UI) |
| type | text | `notification` \| `private` \| `group` |
| status | text | `draft` \| `scheduled` \| `publishing` \| `published` \| `revoked` \| `archived` |
| category_id | uuid NULL → message_categories | RESTRICT |
| sender_id | uuid NULL | user id; service senders keep `sender_service` (SPIFFE id) instead |
| recipients | jsonb | `{"all": true}` or `{"users": [ids]}` (≤ 10,000 ids) — the request, kept for re-publish and display |
| scheduled_at | timestamptz NULL | |
| lease_until | timestamptz NULL | scheduler claim (research R5) |
| published_at | timestamptz NULL | |
| created_by, updated_by, created_at, updated_at | | |

State transitions:

```text
draft ──send(now)──▶ publishing ──▶ published ──revoke──▶ revoked
draft ──send(at)───▶ scheduled ──(scheduler)──▶ publishing ──▶ published
scheduled ──cancel──▶ draft            published ──archive──▶ archived
draft ──delete──▶ (row removed)        revoked/archived: terminal (delete allowed for admins)
```

Editing is allowed in `draft` and `scheduled` only. Index `(tenant_id,
status, scheduled_at)` for the scheduler claim.

## inbox

| Column | Type | Notes |
|--------|------|-------|
| id | uuid PK | |
| tenant_id | uuid | RLS |
| message_id | uuid → messages | CASCADE |
| recipient_id | uuid | user id |
| status | text | `sent` \| `received` \| `read` \| `revoked` \| `deleted` |
| read_at | timestamptz NULL | |
| created_at, updated_at | | |

`UNIQUE (message_id, recipient_id)` (idempotent fan-out); index
`(tenant_id, recipient_id, status, created_at DESC)`. Listing for a person:
`status IN ('sent','received','read')` or `revoked` with `read_at IS NOT NULL`;
`deleted` rows are hidden. Unread count: `status IN ('sent','received')`.

## notification_audit_events (hypertable, 7-day chunks, 400-day retention)

Same columns as warden's audit table (`ts, tenant_id, event_type, actor_kind
(user|service|system), actor_id, subject_kind (channel|template|notification|
grant|category|message|inbox|backup|system), subject_id, outcome (ok|refused|
failed), reason, correlation_id, details jsonb`). Reads add a derived
`subject_name` (channel/template name, message title) while the subject
exists.

Event types (closed vocabulary): `channel_created`, `channel_updated`,
`channel_deleted`, `channel_tested`, `template_created`, `template_updated`,
`template_deleted`, `template_previewed`, `notification_sent`,
`notification_failed`, `grant_created`, `grant_revoked`, `access_refused`,
`category_created`, `category_updated`, `category_deleted`,
`message_created`, `message_updated`, `message_deleted`,
`message_published`, `message_revoked`, `message_archived`,
`inbox_read`, `inbox_deleted`, `event_published`, `backup_exported`,
`backup_exported_with_credentials`, `backup_imported`, `stream_opened`,
`stream_refused`. Detail guard: keys containing `password`, `secret`,
`token`, `api_key`, `body`, `content`, `settings` are dropped; string values
longer than 256 characters are truncated.

## Live events (Valkey, not persisted)

Stream `notif:events:<tenant_id>`; entry fields `to` (`*` or
comma-separated user ids ≤ 1,000), `type` (≤ 64, `^[a-z][a-z0-9._-]*$`),
`data` (JSON ≤ 16 KiB), `at` (unix ms). `MAXLEN ~ 10000`; trimmed to the
replay window (5 min) by `XTRIM MINID` from the subscriber loop. Built-in
types: `inbox` (`{message_id, title, category, unread}`), `inbox.revoked`
(`{message_id, unread}`), `reset` (client refreshes). Rate-limit counters:
`notif:rl:tenant:<id>:<minute>`, `notif:rl:sender:<id>:<minute>` (TTL 2 min).

## Views returned to the browser (never carry sealed settings)

- `Channel`: id, name, type, settings (public fields + `"__set__"` markers),
  enabled, is_default, template_count, created_by, updated_by, times,
  permissions `{read, write, delete, share, use}`.
- `Template`: id, name, channel_id, channel_name, channel_type, subject, body,
  variables, is_default, created_by, updated_by, times, permissions.
- `LogEntry`: as the table, `rendered_body` only in the single-entry read.
- `Message`: as the table plus `category_name`, `recipient_count`,
  `read_count`.
- `InboxEntry`: id, message `{id, title, content, type, category_name,
  sender_id, published_at}`, status, read_at, created_at.
- `Grant`, `Effective`: as warden's.
