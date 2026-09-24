# Changelog — services/notification

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
