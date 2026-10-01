# Changelog — services/notification

## Unreleased

- **Server-side pagination and sorting for every table (feature 032)**:
  `GET /channels`, `/templates`, `/messages`, `/notifications`, `/categories`
  and `/audit` take `page`, `page_size` (1–200), `sort` and `order` and answer
  `{items, total, page, page_size, sort, order}`; a page beyond the end
  answers the last page. Sort fields: channels `name|type|created_at`,
  templates `name|channel|updated_at`, messages `created_at|subject|status`,
  log `created_at|status|channel`, categories `name|sort_order`, audit `ts`.
  Invalid values answer 422 `validation_failed` with `detail.param` (the
  value is never echoed); malformed query parameters rejected by the OpenAPI
  validator now answer the same 422 (was 400 `malformed_body`).
- Channel and template visibility moved into SQL: the caller's readable ids
  (all for tenant administrators) restrict both the count and the page, so a
  member with partial grants sees exact totals and never counts or receives a
  hidden record. Messages keep the sender scope and the log the own-sends
  scope (without `stats:read`) in the count as well.
- The log and audit lists cover the **last 7 days** when `from`/`to` are
  absent (exact counts within a bounded window on the hypertables); `from`
  and `to` widen or move it. This applies to the legacy cursor path too.
- Legacy `cursor`/`limit` paging keeps its previous shape plus `total` for one
  release; mixing it with the new parameters is 422 (`param: cursor`). gRPC,
  backup walks and the inbox are unchanged. The `limit` parameter lost its
  OpenAPI default (the validator would have added it to every request).
- `GET /categories` is paged (default order `sort_order`, 25 per page).
- Migration `0006_list_indexes`: `messages (tenant_id, lower(title), id)` and
  `messages (tenant_id, sender_id, created_at DESC, id DESC)`.
- UI (`@go-tangra/ui` ^4.3.0): channels, templates, messages, log, audit,
  categories and permissions tables are server-paged and sortable with the
  page, size and sort in the URL (`?channels.page=…`); selects load up to 200
  channels / categories.
- Dependency: `github.com/go-tangra/go-tangra/v4` v4.3.0 (`listquery`).

- **Scheduled tasks for the scheduler module (feature 026)**: the module
  serves `scheduler.v1.TaskExecutor/ExecuteTask` (scheduler-v4 SDK) and
  executes `notification:send-test-email` (payload `recipient`, optional
  `subject` ≤ 200, `body` ≤ 10,000 plain text, `channelId`): a test email
  through the tenant's default or chosen email channel — never the platform
  channel — logged and audited with the scheduler as the actor. Invalid
  payloads, a missing/disabled/non-email channel and permanent delivery
  refusals fail permanently; transient delivery failures and rate limits are
  retried by the scheduler. Only `svc/scheduler` of the own trust domain may
  call the executor (policy rule `scheduler-execute` plus an in-process
  check).
- New config section `task_scheduler: {enabled: false, service: scheduler}`
  (separate from the message `scheduler` section): `enabled: true` registers
  the task types with the scheduler (`discovery.static.<service>`), retried
  until accepted and refreshed every 5 minutes.
- New system template `lcm.certificates_expiring` (variables `days`, `count`,
  `certificates`, optional `tenant`) for lcm's expiring-certificates digest;
  `deploy/policy.yaml` admits `svc/lcm` in `modules-send` (lcm may send only
  `lcm.*` keys).
- Dependency: `github.com/go-tangra/go-tangra-scheduler/sdk/v4`
  (`golang.org/x/net` 0.59.0 follows from it).

## 4.3.0 — 2026-09-26

- **Visual editor for email template bodies**: in *Templates*, the body of an
  email template (system templates included, with *Restore built-in*) is
  edited in a rich-text editor (bold, italic, underline, strike, headings,
  lists, quote, links, undo/redo) with an *HTML source* toggle. sms, slack
  and sse templates keep the plain text area.
- Go template actions (`{{.link}}`, `{{ .tenant }}`, `{{if .x}}…{{end}}`,
  pipes, trim markers, comments) are stored exactly as written, in text and
  in attribute values such as `href="{{.link}}"`; they appear as chips in the
  editor. *Insert variable* adds `{{.name}}` for the template's declared
  variables (system templates: including the required ones).
- A body the editor cannot keep exactly (inline styles, `div`/`img`/tables,
  an action between blocks such as the built-in `warden.share` conditional,
  an unterminated action) opens in the source view with a warning and is not
  rewritten; switching it to the visual editor asks first. Opening and saving
  without edits never changes a body.
- New UI dependencies `@tiptap/core`, `@tiptap/starter-kit`, `@tiptap/pm`
  (MIT, justified in `docs/dependencies.md`), loaded as a separate chunk only
  when an email template is opened. No server change: bodies are still
  validated and rendered with `html/template`.

## 4.2.1 — manifest version

- **Fix**: the gateway manifest version is raised to `1.1.0`. 4.2.0 added the
  template restore route but kept manifest version `1.0.0`, so the gateway
  refused its registration (`manifest_drift`) while a 4.1 instance was still
  registered (visible for one lease period on upgrade; a rolling update with
  two instances would keep failing).
- A contract test pins the manifest content to its version: changing routes,
  permissions or navigation without raising the version now fails CI.

## 4.2.0 — 2026-09-26 (feature 017, central email delivery)

notification becomes the single outbound-email path of the platform: auth
(invitations, recovery) and warden (share links) send through it instead of
their own relay settings.

- **Platform relay**: new `platform_email` block (host, port, `tls`
  implicit|starttls|none, username, `password_file`, from, reply_to,
  `allow_plaintext`). At start the module creates or updates the managed
  "Platform email" channel of the platform tenant (`platform_tenant_id`,
  default `00000000-0000-0000-0000-000000000001`): enabled, default email
  channel, usable tenant-wide, password sealed. Without the block an existing
  managed channel is disabled. The channel is read-only in the API and UI
  (`409 managed_channel`), test sends work, backups never export it.
- **Refusals at start**: a literal `platform_email.password` (use
  `password_file`), `tls: none` without `platform_email.allow_plaintext`, a
  username with `tls: none`, a missing/empty/unreadable password file or one
  more open than 0640. `platform_email.allow_plaintext` is warned at every
  start; tenant channels keep `smtp.allow_plaintext` and its production
  refusal.
- **System templates**: `auth.invite`, `auth.account_reset`, `auth.recovery`,
  `auth.message` (messages queued by auth 4.1) and `warden.share`, seeded in
  the platform tenant with built-in English wording; edits survive restarts
  and upgrades. Only subject and body change (`422 system_template_field`);
  dropping a required variable is refused (`422 missing_required_variable`);
  deletion is refused (`409 system_template`); `POST
  /templates/{id}/restore` returns to the built-in wording.
- **Send by key**: `notification.v1.SendRequest.template_key` (exactly one of
  `template_id` / `template_key`). The key must belong to the caller's
  namespace (`svc/<name>` may send `<name>.*`; refusals audited as
  `access_refused`); no per-tenant grants; the tenant's enabled default email
  channel, else the platform channel, else `FailedPrecondition
  email_not_configured`; `limits_notification.system_send_per_minute`
  (default 300) per calling service, throttled as `ResourceExhausted`.
- **Redaction**: secret variables (the links) are stored as `[redacted]` in
  the log entry (subject and body, every escaping context) and scrubbed from
  failure reasons; audit rows carry no variables. Log entries carry
  `template_key`.
- **Retryable outcome**: `SendResponse.retryable` tells callers whether a
  failed delivery may succeed later (network errors and SMTP 4xx) or never
  will (no STARTTLS, certificate not valid for the host, invalid recipient,
  SMTP 5xx).
- **SDK module**: `github.com/go-tangra/go-tangra-notification/sdk/v4` holds
  the `notification.v1` proto and `pkg/notifyclient` (`SendKey` with
  `Result{Sent, Retryable, Reason}`), so callers no longer depend on the
  service module.
- Migration `0005_system_email.sql` (forward-only).

## Unreleased — feature 006 (notification service)

Added the notification module: a tenant-scoped, multi-channel notification and
in-app messaging service composed into the platform shell.

- **Channels & templates**: encrypted-at-rest channel settings (email over SMTP
  implemented; sms/slack/sse declared), Go-template subjects/bodies with a
  declared variable list, preview, one default per type/channel, a "send a test
  message" action. Credentials are never returned in full (`"__set__"`).
- **Send pipeline & log**: render -> deliver -> a notification log entry
  (pending -> sent/failed with a scrubbed error), listable with filters, single
  entry with the rendered body. Valkey rate limits (600/min tenant, 60/min
  sender) fail closed.
- **Zanzibar access**: owner/editor/viewer/sharer on channels and templates, to
  users/roles/the tenant with expiry; `use` gates sending; grant/revoke/check/
  effective; unreadable ids answer `not_found`.
- **Internal messages & inbox**: categories, messages (draft -> scheduled ->
  published -> revoked -> archived), publish to users or everyone (auth
  `ListMembers`), a per-user inbox with unread counts, revoke, and delayed
  publishing by an SQL-lease scheduler (exactly-once).
- **Live stream**: a per-user SSE stream relayed by the gateway delivering inbox
  events and module `Publish` events, with `Last-Event-ID` replay, heartbeat and
  per-person/per-tenant limits. A bell with an unread badge in the shell header
  (`./header` expose).
- **Backup & operations**: tenant export/import of channels/templates/categories
  (skip|overwrite; credentials only on request, audited), statistics, audit trail
  with resolved names, health.
- **Interfaces**: browser API under `/api/notification/v1`; `notification.v1`
  gRPC (`Notifier`, `Events`) for services with `pkg/notifyclient`; a Module
  Federation remote; gateway manifest from the OpenAPI document.

Platform changes for this feature:

- `auth.v1.Profiles/ListMembers` (paged active members) and a policy rule
  admitting the notification service on the directory RPCs.
- Shell `./header` expose: manifest schema enum, `boot.ts` header slots,
  `Default.vue` app-bar rendering inside an isolated boundary.
- Gateway decision cache keys namespaced (`gwdec:`) so they no longer collide
  with the auth service's own decision cache on a shared Valkey.
