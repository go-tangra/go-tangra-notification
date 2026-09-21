# services/notification — tenant notifications & messaging

A tenant-scoped, multi-channel notification service built on the Freya
framework and composed into the platform shell as a federated remote. It
delivers **outbound notifications** through configurable channels (email over
SMTP is fully implemented; sms/slack/sse are declared for later providers)
using Go-template subjects and bodies, and **internal messages** (an in-app
inbox with categories, scheduling, revoke and a live server-sent-events
stream). Channel credentials are encrypted at rest (AES-256-GCM envelope) and
never returned in full; access to channels and templates is Zanzibar-style
(owner / editor / viewer / sharer, to users, roles or the tenant, with
expiry). Every operation is audited; credentials and message bodies never
reach logs, the audit trail or a credential-free backup.

Design: `specs/006-notification-service/` (spec, plan, research, data model,
contracts, quickstart). Security model:
[`docs/security-model.md`](docs/security-model.md). Operations:
[`docs/operations.md`](docs/operations.md). Dependencies:
[`docs/dependencies.md`](docs/dependencies.md).

## Layout

| Path | What |
|------|------|
| `api/openapi/notification.yaml` | browser API contract (served under `/api/notification/v1`) |
| `api/proto/notification/v1/` | `Notifier` (Send, SendTest) and `Events` (Publish) gRPC for services |
| `api/schema/backup.schema.json` | tenant backup document schema |
| `internal/config` | configuration + validation (secure defaults, named opt-outs) |
| `internal/store`, `internal/repo` | TimescaleDB schema (RLS, hypertables), repositories + in-memory double |
| `internal/sealed` | envelope encryption of channel settings (KEK -> DEK, `"__set__"` redaction) |
| `internal/audit` | closed audit vocabulary, batched writer, credential/content guard |
| `internal/authz` | Zanzibar grants (owner/editor/viewer/sharer, `use`) |
| `internal/channel`, `internal/channel/email` | delivery providers (stdlib `net/smtp`, hand-built MIME) |
| `internal/render` | safe Go-template rendering (fixed FuncMap, bounded time/size) |
| `internal/notify` | channels, templates and the send pipeline + log |
| `internal/messages`, `internal/inbox` | internal messages, scheduler, per-user inbox |
| `internal/stream` | Valkey-backed live event fan-out + SSE relay |
| `internal/transfer`, `internal/stats` | backup export/import, operator statistics |
| `internal/httpapi`, `internal/grpcapi` | browser and service APIs |
| `internal/app`, `cmd/notificationsvc` | wiring and the service binary |
| `pkg/notificationmanifest` | gateway manifest built from the OpenAPI document |
| `pkg/notifyclient` | Go client other services use to Send / Publish |
| `ui/` | Vue 3 + Vuetify federated remote (channels, templates, log, messages, inbox, permissions, ops) |

## Run

```bash
make -C services/notification compose-up      # TimescaleDB :5434, Valkey :6381, Mailpit :8027/:1027
make -C services/notification cover fuzz       # unit gate + fuzz targets
make -C services/notification test-integration # tagged end-to-end suite (Docker)
cd services/notification/ui && npm run build   # federated remote (embedded with -tags ui)
```

See `specs/006-notification-service/quickstart.md` for the full end-to-end run
against the gateway and auth service.

## API permissions

`channels:read/manage`, `templates:read/manage`, `notifications:send/read`,
`messages:read/manage`, `inbox:read`, `events:publish`, `permissions:manage`,
`backup:manage`, `stats:read`. The gateway enforces the per-route permission
from the manifest; the module then enforces the Zanzibar grant (`use` is what
sending requires). Built-in role grants are seeded by the module
(`pkg/notificationmanifest.Grants`).

## Limits (defaults)

600 sends/min per tenant, 60/min per sender; 5 live streams per person, 2000
per tenant; a 5-minute replay window; a 16 MiB backup upload; the scheduler
runs every 15 s with a 60 s lease.
