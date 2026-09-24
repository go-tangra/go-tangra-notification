# Implementation Plan: Notification Service

**Branch**: `006-notification-service` | **Date**: 2026-09-17 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/006-notification-service/spec.md`.

## Summary

`services/notification` is a new Freya platform module with the same shape as
`services/warden`: a Go service on the Freya mTLS channel that registers with
the application gateway, verifies the platform token forwarded by the gateway
with `pkg/authclient`, and stores channels, templates, the notification log,
grants, message categories, internal messages, inbox entries and audit rows in
TimescaleDB under per-tenant RLS. Email delivery uses `net/smtp` with
STARTTLS/TLS; channel provider settings are sealed at rest with an envelope
(AES-GCM data key wrapped by a KEK loaded from a file or the environment, the
scheme already used by auth). Templates are `text/template` / `html/template`
documents rendered with a fixed safe function set and a bounded output.
Zanzibar-style access (Owner/Editor/Viewer/Sharer with the extra `use` action)
is one indexed SQL query, copied from warden without the folder inheritance.
Internal messages fan out to one inbox row per recipient; scheduled messages
are published by an in-service worker with a leased claim in SQL so exactly one
instance publishes. Live push is a module-served SSE route relayed by the
gateway (5-minute streams, `Last-Event-ID` reconnect) fed by one Valkey stream
per tenant that gives fan-out across instances and a bounded replay window.
Other modules send and publish over `notification.v1` gRPC directly on the
Freya channel, authorized by the callee's `policy.yaml`. The browser API is an
OpenAPI document validated by kin-openapi, and the UI is a Vue 3 + Vuetify
Module Federation remote composed by the shell, with a small shell extension
for the header inbox badge.

## Technical Context

**Language/Version**: Go 1.26 (service), TypeScript 5 / Vue 3 / Vuetify 4 (remote and shell extension)

**Primary Dependencies**: Freya framework (transport, identity, audit, config), `services/auth/pkg/authclient` (token verification), `services/gateway/pkg/gatewayclient` (registration, manifest), pgx + goose (TimescaleDB), kin-openapi (request validation), `github.com/valkey-io/valkey-go` (streams for live events, rate limits), stdlib `net/smtp`, `crypto/aes` + `crypto/cipher` (envelope), `text/template` + `html/template` (rendering), hand-built MIME (text + HTML alternative), Module Federation runtime (UI). No ORM, no new third-party module (research R2, R3, R4).

**Storage**: TimescaleDB (`notification` database, `notification_app` role, RLS per tenant): `channels`, `templates`, `notification_log` (hypertable, 400-day retention), `grants`, `message_categories`, `messages`, `inbox` (message × recipient), `notification_audit_events` (hypertable). Valkey: one stream per tenant for live events (`XADD` … `MAXLEN ~ 10000`, trimmed to the replay window), rate-limit counters. No file storage.

**Testing**: Go unit (memstore double, fake SMTP server in-process, fake clock for the scheduler), contract (OpenAPI ↔ routes, proto, manifest), fuzz (template validator, backup parser, SMTP header validation, recipient parser, SSE event parser), integration (testcontainers: TimescaleDB, Valkey, Mailpit; gateway + auth as subprocesses like feature 005's harness), Vitest, Playwright + axe through the gateway (including the SSE stream and the header badge).

**Target Platform**: Linux server; evergreen browsers through the platform shell.

**Project Type**: web service + federated remote (platform module) + a shell extension point (header slot for the inbox badge).

**Performance Goals**: send p95 < 2 s to relay hand-off (SC-002); permission check one SQL query (< 5 ms) per request; live event delivered to open streams < 2 s (SC-005); "everyone" fan-out of 10,000 inbox rows < 60 s in batches of 1,000 (SC-005); scheduled publish within 60 s (SC-006); backup of 100 templates < 10 s (SC-008).

**Constraints**: credentials never in listings/logs/audit/errors/exports-without-credentials (SR-001, scanned with marker values); rendering bounded (1 s, 1 MiB output) with a fixed function set (SR-002); header-injection checks (SR-003); per-tenant/per-sender send limits (SR-005); SSE streams capped per person (5) and per tenant, 5-minute max age (gateway route timeout ceiling `MaxDurationRoute`), replay window 5 minutes (SR-006); bodies ≤ 256 KiB, variables ≤ 64 KiB, event payload ≤ 16 KiB, backup upload ≤ 16 MiB (route-level override as in warden).

**Scale/Scope**: tenants up to 50k members and 1M log rows; ~45 HTTP endpoints, 2 gRPC services (3 methods), 7 UI views + 1 header component, 8 tables, 1 Valkey stream per tenant.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **I. Secure by Default**: KEK required (no plaintext fallback); SMTP requires TLS or STARTTLS unless `smtp.allow_plaintext` (dev only, warns at start); channels are created disabled until a test send or an explicit enable; exports without credentials by default, with credentials only with `backup:manage` and an explicit flag; no public route at all (the stream is authenticated); modules may call gRPC only when listed in `policy.yaml`. PASS
- [x] **II. Zero Trust**: gateway → notification over mTLS; every route declared in the manifest with a permission; platform token verified by `authclient` middleware before handlers; gRPC callers policed by `policy.yaml` (SPIFFE allow-list per method) and the tenant taken from the request only for allowed service identities; the stream is bound to the verified user id; Valkey and TimescaleDB reached with dedicated credentials. PASS
- [x] **III. Boundary Validation**: OpenAPI schemas with lengths/patterns and `additionalProperties: false`; kin-openapi validation; template documents parsed with size/time bounds and a whitelist of functions; provider settings validated per type at save; recipients validated per channel type (RFC 5322 address for email, no CR/LF anywhere in headers); backup files parsed with size/depth bounds; proto messages validated in handlers. PASS
- [x] **IV. Test-First**: tests listed before implementation per story; negative tests for cross-tenant, escalation, expired grants, sends without `use`, inbox of another person, cross-tenant publish, header injection, template escape/function abuse; fuzz for every parser; credential-leak scan; `internal/authz`, `internal/render`, `internal/channel/email` (header/credential paths), `internal/sealed`, `internal/stream` at 100 %. PASS
- [x] **V. Observability**: audit hypertable with closed vocabulary; every send/test send/publish/revoke/grant audited with actor and subject; refusals audited; framework redacting logger plus a detail guard (keys `password`, `secret`, `token`, `body`, `content`, `settings` never in details); correlation ids from the gateway; health/metrics on the admin listener only; stream connection counts as metrics. PASS
- [x] **VI. Supply Chain**: no new third-party module (SMTP, MIME, templates and AES-GCM are stdlib; Valkey client, pgx, goose, kin-openapi, testcontainers already in the tree); `govulncheck` in CI; no custom cryptography (AES-256-GCM envelope copied from auth's audited `internal/crypto/envelope.go`). PASS
- [x] **VII. Simplicity**: grants evaluated in SQL like warden (no policy store); scheduler is a SQL lease loop (no queue, no cron library); live fan-out is one Valkey stream per tenant (no message broker); SSE served by the module and relayed by the gateway (no gateway change for the stream); the "sse" channel type is accepted but has no provider. Complexity Tracking records the 16 MiB body override, the shell header slot and the auth member-listing RPC. PASS
- [x] **Threat Model**: STRIDE table in research.md §Threat model. PASS

Post-design re-check (after Phase 1): unchanged, all PASS.

## Project Structure

### Documentation (this feature)

```text
specs/006-notification-service/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── notification-api.openapi.yaml   # browser API (through the gateway)
│   ├── notification.v1.proto           # service-to-service gRPC (Notifier, Events)
│   ├── manifest.md                     # gateway manifest: prefixes, permissions, abilities, nav
│   ├── stream.md                       # SSE wire format, ids, replay, reconnect
│   ├── backup.schema.json              # accepted backup document
│   ├── auth-changes.md                 # Profiles.ListMembers RPC in the auth service
│   └── shell-changes.md                # header slot for the inbox badge
└── tasks.md
```

### Source Code (repository root)

```text
services/notification/
├── go.mod                          # replace github.com/go-freya/freya => ../..; requires services/auth, services/gateway (pkg only)
├── cmd/notificationsvc/{main.go,bootstrap.go}   # run; bootstrap creates DB schema and checks the KEK
├── api/
│   ├── openapi/{notification.yaml,openapi.go}   # embedded contract
│   ├── proto/notification/v1/{notification.proto,*.pb.go}
│   └── schema/backup.schema.json
├── internal/
│   ├── config/                     # Config{Freya inline, DB, Valkey, KEK{Source,Path,Env}, SMTP{AllowPlaintext, DialTimeout}, Gateway, Limits{SendPerTenantPerMinute, SendPerSenderPerMinute, StreamsPerUser, ReplayWindow}, Scheduler{Interval}}
│   ├── store/                      # pgx repos, migrations/000{1..4}_*.sql, RLS, hypertables, lease claim for scheduled messages
│   ├── memstore/                   # in-memory Store double for unit tests
│   ├── sealed/                     # envelope encryption of provider settings (copied from auth's internal/crypto), redaction of credential fields
│   ├── audit/                      # writer + closed vocabulary + detail guard
│   ├── authz/                      # Zanzibar check with the `use` action: subjects from identity, relation lattice, grant/revoke/list/effective
│   ├── channel/                    # Provider interface, registry by type; email/ (net/smtp, MIME, header validation, dial timeout); nop for sms/slack/sse
│   ├── render/                     # template parse/validate (declared variables, function whitelist), preview, bounded render
│   ├── notify/                     # Send: resolve template/channel, authz `use`, render, deliver, log; test send; log listing
│   ├── messages/                   # categories, messages, fan-out to inbox, revoke, scheduler worker (lease loop)
│   ├── inbox/                      # per-user listing, read/status/delete
│   ├── stream/                     # Valkey stream publisher/subscriber, per-user hub, SSE writer, replay from Last-Event-ID
│   ├── transfer/                   # backup export/import (skip|overwrite)
│   ├── stats/                      # counts per tenant, open streams
│   ├── httpapi/                    # OpenAPI-validated handlers, authclient middleware, SSE route
│   ├── grpcapi/                    # notification.v1 Notifier + Events services (service callers)
│   └── app/                        # wiring, gateway registration, builtin grants, health
├── pkg/notificationmanifest/       # gateway manifest (routes from the OpenAPI doc, permissions, abilities, nav)
├── pkg/notifyclient/               # thin Go client for other modules: Send, Publish (wraps Freya.Client("notification"))
├── ui/                             # Vue 3 + Vuetify remote (exposes ./routes, ./nav, ./header)
│   ├── src/{api,stores,views/{channels,templates,log,messages,categories,inbox,permissions},components,remote}
│   └── tests/{unit,e2e}
├── deploy/{compose.yaml (TimescaleDB, Valkey, Mailpit), init-db.sql, dev.yaml, policy.yaml, kek.dev}
├── docs/{security-model.md,operations.md,dependencies.md}
├── scripts/{coverage-gate.sh,redaction-scan.sh}
├── Makefile
└── tests/{contract,fuzz,integration}

services/gateway/shell/                # header slot: renders a remote's ./header export (contracts/shell-changes.md)
services/auth/                          # auth.v1.Profiles/ListMembers (contracts/auth-changes.md)
```

**Structure Decision**: mirror `services/warden` package for package (domain packages under `internal/`, `memstore` double, embedded OpenAPI, `pkg/<module>manifest`, `ui/` embedded with `-tags ui`), reusing its authz, audit, store and httpapi patterns by copy (the services will move to their own repositories, so no shared internal package). New pieces are `sealed`, `channel`, `render`, `stream`, the scheduler worker in `messages`, and `pkg/notifyclient` for other modules.

## Complexity Tracking

| Item | Why Needed | Simpler Alternative Rejected Because |
|------|------------|-------------------------------------|
| Request body limit 16 MiB on `/api/notification/v1/backup/import` (route `max_body_bytes`, service limit raised to match) | tenant backups with hundreds of templates exceed 1 MiB | chunked upload adds an upload state machine; the limit is per route and JSON handlers keep the 64 KiB cap (256 KiB for template bodies) |
| Shell header slot (`./header` export rendered by `Default.vue`) | the inbox badge must live in the shell header, outside the module's routes | a polling badge inside the module's own pages would not be visible on other modules' pages; a hard-coded notification component in the shell would couple the shell to one module |
| `auth.v1.Profiles/ListMembers` RPC | "everyone in the tenant" needs the active member ids at publish time | the browser search endpoint is capped at 20 results and rate-limited; the console admin listing is admin-only and browser-facing |
| Valkey stream per tenant for live events | fan-out across service instances plus a replay window in one primitive | in-process hub only works for one instance; pub/sub has no replay; a table of events adds writes on the hot path |
