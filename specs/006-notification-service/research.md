# Research: Notification Service

**Feature**: 006-notification-service · **Date**: 2026-09-17

Every unknown of the Technical Context is resolved below. The reference
implementation (`go-tangra-notification`: Kratos + ent + a scheduler service +
an SSE transport) was read for behaviour, not for structure; feature 005
(warden) is the structural template.

## R1. Provider settings at rest: envelope encryption with a KEK

**Decision**: `internal/sealed` seals the JSON provider settings of a channel
with a per-row AES-256-GCM data key wrapped by a 32-byte KEK (layout
`nonce | wrapped DEK | nonce | ciphertext`, associated data `channel:<id>`),
the exact scheme of `services/auth/internal/crypto/envelope.go`, copied into
the module. The KEK comes from `kek: {source: file|env, path|env}`; production
refuses to start without it. Reads return the settings with the fields listed
in the provider's `Secret()` set (`password`, `api_key`, `token`) replaced by
`"__set__"`; updates that send `"__set__"` (or omit the field) keep the stored
value; sending an empty string clears it.

**Rationale**: the platform already uses this construction (auth's TOTP seeds
and signing keys); stdlib only; the KEK never reaches the database; rotation
is re-sealing rows with a new KEK (an operator command in `bootstrap`).

**Alternatives considered**: storing credentials in warden's Vault mount
(couples two modules and adds a second store for a few kilobytes); Postgres
`pgcrypto` (key would be in SQL); one static key without wrapping (no rotation
path).

## R2. Email delivery: stdlib `net/smtp` with STARTTLS/TLS

**Decision**: `internal/channel/email` builds the MIME message itself
(`multipart/alternative` with a `text/plain` part derived from the HTML by
tag stripping, `Content-Transfer-Encoding: quoted-printable` via
`mime/quotedprintable`, `Message-ID`, `Date`, `MIME-Version`) and delivers
with `net/smtp`: dial with a timeout (default 30 s), implicit TLS on port
465, otherwise `STARTTLS` when advertised; plaintext only when
`smtp.allow_plaintext` is set (dev, warns at start). Auth is `PLAIN` over TLS
only (as in auth's mailer and warden's share mail). Every header value is
checked for CR/LF and non-printable characters; the recipient must parse with
`net/mail.ParseAddress` and be a single address; `From` comes from the channel
settings, `Reply-To` optionally.

**Rationale**: the reference uses `net/smtp` too; a third-party mailer adds a
dependency for no gain; the platform's two existing senders use the same
approach.

**Alternatives considered**: `github.com/wneessen/go-mail` (richer, but a new
dependency for SR-VI); sending through auth's email queue (auth's queue is for
its own transactional mail and has no per-tenant relays).

## R3. Template language: `text/template` + `html/template`, safe function set

**Decision**: subjects are `text/template`; bodies are `html/template` for the
email type (contextual escaping of variables) and `text/template` for the
other types. `internal/render` parses with `Option("missingkey=error")` and a
fixed `FuncMap` (`upper`, `lower`, `title`, `trim`, `default`, `date`, `join`,
`printf`); the parse tree is walked to collect every `.Name` field reference
and compared with the declared variables (undeclared → refused by name;
declared but unused → allowed). Rendering runs with a 1 s context deadline
and a 1 MiB limited writer; variables are `map[string]string` only (no method
calls, no nested structures), which removes template code execution through
data. Preview uses the same path with the caller's sample variables and never
touches the log.

**Rationale**: the reference renders Go templates; `html/template` gives the
escaping SR-002 asks for; a walk of `parse.Tree` gives the declared-variable
check without executing anything.

**Alternatives considered**: Mustache/handlebars (new dependency, no
contextual escaping); allowing arbitrary variable structures (widens the
injection surface).

## R4. Live stream: module-served SSE relayed by the gateway, Valkey streams behind it

**Decision**: `GET /api/notification/v1/stream` is an ordinary manifest route
with `x-freya-timeout-seconds: 300` (the gateway's `MaxDurationRoute`) and
`inbox:read`; the gateway already relays streaming responses
(`httputil.ReverseProxy` with `FlushInterval: -1`, a flushing status
recorder). The handler writes `text/event-stream` with `retry: 3000`, a `:
ping` every 15 s, and closes the response at 4 min 50 s so the client
reconnects with `Last-Event-ID` before the gateway deadline. Events live in
one Valkey stream per tenant (`notif:events:<tenant_id>`, `XADD … MAXLEN ~
10000`, plus `XTRIM MINID` to the replay window every minute); each event
carries `to` (user ids, or `*` for the tenant), `type`, `data` (≤ 16 KiB) and
the Valkey stream id becomes the SSE `id`. Each service instance runs one
`XREAD BLOCK` loop per tenant with open streams and fans events out to the
per-user hubs it holds; a reconnecting client's `Last-Event-ID` is served with
`XRANGE (id +`, filtered for the user; an id older than the window yields
`event: reset` and the client refreshes its inbox. Limits: 5 streams per
person, 2,000 per tenant, 1 KiB write buffer per stream — a stalled client is
dropped.

**Rationale**: no gateway change; the shell already consumes an SSE stream
from the gateway with the same reconnect pattern (`EventsMaxAge`); Valkey
streams give fan-out and replay in one primitive and Valkey is already a
dependency.

**Alternatives considered**: extending the gateway's own `/gateway/v1/events`
to carry module events (couples the gateway to the notification module and
needs a new gRPC subscription); WebSocket (the gateway does not relay
upgrades); Postgres `LISTEN/NOTIFY` (no replay, one connection per instance
per tenant); an in-process hub only (breaks with two instances).

## R5. Scheduled publishing: SQL lease loop

**Decision**: `messages.Scheduler` runs every `scheduler.interval` (default
15 s): `UPDATE messages SET status='publishing', lease_until=now()+60s WHERE
status='scheduled' AND scheduled_at <= now() AND (lease_until IS NULL OR
lease_until < now()) RETURNING id` (bounded to 50 rows), then publishes each
claimed message (fan-out to inbox in batches of 1,000 inside one transaction
per batch, live events after commit) and sets `status='published',
published_at`. A crash mid-publish leaves the lease to expire; the next claim
re-runs the fan-out, which is idempotent (`INSERT … ON CONFLICT (message_id,
recipient_id) DO NOTHING`). Health reports the scheduler's last tick.

**Rationale**: exactly-once publishing across instances with one table and no
queue; the reference delegated this to an external scheduler the platform does
not have.

**Alternatives considered**: `pg_cron` (extension not guaranteed);
Valkey-based locks (a second source of truth for state); a cron library
(single-instance).

## R6. "Everyone in the tenant" and recipient validation

**Decision**: the auth service gains `auth.v1.Profiles/ListMembers(tenant_id,
cursor, limit ≤ 1000)` returning active member ids (service-to-service,
policy-restricted to the notification identity; see contracts/auth-changes.md).
Fan-out pages through it and inserts inbox rows per page. Explicit recipients
are validated with `Profiles/Lookup` (unknown or foreign ids are dropped from
the recipient list and reported in the response). Sender and recipient
display names are resolved by the UI through the existing browser lookup
(`POST /api/v1/users/lookup`) as warden does.

**Rationale**: membership is auth's data; a paged service RPC is the only
interface that scales to 50k members.

**Alternatives considered**: caching the member list in the notification
database (stale, duplicates auth's data); the browser search endpoint (≤ 20
results).

## R7. Access check in SQL with the `use` action

**Decision**: warden's `internal/authz` is copied with three changes: no
ancestor chain (channels and templates are flat), the relation → actions
lattice adds `use` (Owner, Editor, Sharer), and resource types are `channel`
and `template`. Tenant owner/admin roles (from the platform token's effective
roles) hold Owner implicitly (FR-014). Sends check `use` on the template and
on the channel (a template's channel is checked as well when overridden);
a channel with no grants but `enabled` and marked default is usable by every
member (the default channel's implicit tenant `use` grant is materialised as a
tenant grant on creation and removed when the default flag moves).

**Rationale**: identical semantics to warden keep the two modules'
permission UIs interchangeable; the `use` action is the reference's addition.

**Alternatives considered**: role-based only (no delegation of "use" without
"write"); a dedicated authorization service (out of scope, see constitution
VII).

## R8. Send pipeline, log and rate limits

**Decision**: `notify.Send` = validate → load template (read via authz) →
resolve channel (override or template's; must be enabled; type must match the
template's channel type) → authz `use` on both → rate limit (Valkey counters:
600/min/tenant, 60/min/sender, configurable) → insert log row `pending` →
render → deliver → update log `sent`/`failed` with a scrubbed reason (the
provider error is passed through `sealed.Redact` which replaces any stored
credential value and drops everything after the first line) → audit. The log
row is written before delivery so a crash leaves a `pending` row that the
listing shows as such (a maintenance query marks `pending` rows older than 10
minutes `failed: interrupted`). Test sends use a built-in template and mark
the row `test = true`. gRPC sends from modules follow the same path with the
service identity as sender (`actor_kind = service`).

**Rationale**: matches the reference's log semantics; no retry queue (spec
assumption); the pending-first order gives an audit trail even for crashes.

**Alternatives considered**: async queue with retries (spec says no automatic
retry; adds infrastructure); logging only after delivery (loses crashed sends).

## R9. Backup format

**Decision**: one JSON document (`contracts/backup.schema.json`): `version:
1`, `exported_at`, `channels[]` (settings with credential fields omitted unless
`include_credentials`, in which case they are included in clear and the
export is audited `backup_exported_with_credentials`), `templates[]`
(referencing channels by name), `categories[]`. Import validates the whole
document (≤ 16 MiB, ≤ 10,000 items) before any write, then applies per entity
with `mode: skip|overwrite` matched on (name, type) for channels and name for
templates and categories, in one transaction per entity type; templates whose
channel name is unknown are imported with `channel_id = NULL` and reported.
Messages, inbox and log entries are never part of a backup.

**Rationale**: same shape as warden's backup; credentials excluded by default
(SR-001).

**Alternatives considered**: including the log (bulk personal data);
per-entity endpoints (three round trips, no atomicity).

## R10. Browser API shape, gRPC surface and manifest

**Decision**: REST under `/api/notification/v1` (contracts/notification-api
.openapi.yaml, ~45 operations, every operation carrying `x-freya-permission`);
`notification.v1.Notifier/Send`, `Notifier/SendTest` and
`notification.v1.Events/Publish` (one or many users) for services, listed in
`policy.yaml` with the allowed SPIFFE ids (gateway, warden, future modules)
and not proxied by the gateway; `pkg/notifyclient` wraps them for callers.
The manifest (contracts/manifest.md) declares 13 permissions, 8 abilities and
7 navigation entries under a "Notifications" menu (the shell groups them per
module).

**Rationale**: mirrors warden; services call each other directly on the Freya
channel (as warden calls auth), so gRPC methods need no gateway permission.

## R11. Shell header slot for the inbox badge

**Decision**: the shell's `Default.vue` renders, next to the theme toggle,
the `./header` export of every module whose manifest lists it in
`remote.exposes` (contracts/shell-changes.md): a component receiving
`session` and `api` like `./boot`. The notification remote's header component
shows a bell with the unread count, opens a menu with the five newest inbox
entries and a link to the inbox view, and subscribes to the stream (one
`EventSource` shared across the remote via a Pinia store; the badge updates on
`inbox` events). It renders nothing for people without `inbox:read`.

**Rationale**: the spec requires the badge outside the module's own pages;
the generic slot keeps the shell module-agnostic.

**Alternatives considered**: shell-native component calling the notification
API (couples shell and module); polling from the layout (no live updates).

## R12. API permissions and built-in role grants

**Decision**: permissions `channels:read`, `channels:manage`,
`templates:read`, `templates:manage`, `notifications:send`,
`notifications:read`, `messages:read`, `messages:manage`, `inbox:read`,
`events:publish`, `permissions:manage`, `backup:manage`, `stats:read`.
Built-in grants registered with `RegisterPermissions.builtin_grants` (as
warden does): owner/admin all; member `channels:read`, `templates:read`,
`notifications:send`, `notifications:read`, `messages:read`, `inbox:read`;
auditor `stats:read`, `notifications:read`; operator `stats:read`.
`notifications:read` for members lists only their own sends (the log filter is
forced to `sender = caller` unless the caller holds `stats:read`).

## R13. Changes to other services

- Auth: `auth.v1.Profiles/ListMembers` (contracts/auth-changes.md).
- Gateway shell: header slot (contracts/shell-changes.md); allow-list entry
  `spiffe://example.org/svc/notification=/api/notification,/ui;notification`;
  no gateway service change (streams and body limits use existing manifest
  fields).
- Warden (later, out of scope here): switch share mail to
  `pkg/notifyclient` once the notification module exists.

## Threat model (STRIDE)

| Threat | Category | Mitigation | Proof |
|--------|----------|------------|-------|
| T1 relay password read back through the API, backups, logs, audit or error messages | Information disclosure | envelope encryption; `Secret()` fields redacted on read; detail guard; provider errors scrubbed; credentials in exports only with the explicit flag and audit | unit `sealed`, `channel/email` error scrubbing; integration credential-marker scan (`NOTIF-MARKER-PW-`) across listings, log, audit, exports |
| T2 template injection: executing code from variables, calling unsafe functions, reading files/env | Tampering / Elevation | variables are strings only; fixed FuncMap; `html/template` escaping; parse-time validation; 1 s / 1 MiB bounds | unit + fuzz `render` (function names, `{{` in variables, huge loops) |
| T3 mail header injection through recipient, subject, from, reply-to | Tampering | CR/LF and control-character rejection on every header; `net/mail` parsing of addresses | unit + fuzz `channel/email` headers |
| T4 sending through a channel or template without `use`; escalation by granting above own relation | Elevation | authz `use` on template and channel; relation lattice bound by granter | unit `authz` lattice; integration access matrix |
| T5 reading another person's inbox or stream; cross-tenant events | Information disclosure | inbox rows keyed by `(tenant, recipient)` from the verified token; stream hub keyed by user id; events filtered by `to` and tenant stream; gRPC `tenant_id` accepted only from allow-listed services | integration two-user tests; unit `stream` filtering |
| T6 open-relay abuse / spam through the API | Denial of service | per-tenant and per-sender rate limits; recipient must be a single address; log of every send | unit rate limit; integration 61st send refused |
| T7 oversized bodies, variables, backup, event payloads | Denial of service | OpenAPI limits; 16 MiB route override for backup only; bounded rendering | contract tests; fuzz backup parser |
| T8 stream exhaustion (many connections, slow readers) | Denial of service | 5 streams per person, 2,000 per tenant, 1 KiB write buffer, 5-minute max age | unit `stream` limits; integration slow client |
| T9 scheduled message published twice or never after a crash | Tampering / Repudiation | SQL lease claim; idempotent fan-out; health exposes last tick | integration kill-during-publish test |
| T10 spoofed service caller on gRPC | Spoofing | mTLS + `policy.yaml` allow-list per method | contract test refusing an unlisted identity |
| T11 audit gaps | Repudiation | closed vocabulary, refusals audited, contract test that every mutating route has an audit event | `TestEveryMutationAudited` |
