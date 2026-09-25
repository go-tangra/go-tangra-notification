# go-tangra-notification

Tenant notification and messaging service for the
[go-tangra v4 platform](https://github.com/go-tangra/go-tangra).

It delivers **outbound notifications** through configurable channels (email over
SMTP is fully implemented; sms/slack/sse are declared for later providers)
using Go-template subjects and bodies, and **internal messages** (an in-app
inbox with categories, scheduling, revoke and a live server-sent-events
stream). Channel credentials are encrypted at rest (AES-256-GCM envelope) and
never returned in full; access to channels and templates is Zanzibar-style
(owner / editor / viewer / sharer, to users, roles or the tenant, with
expiry). Every operation is audited; credentials and message bodies never
reach logs, the audit trail or a credential-free backup.

Security model: [`docs/security-model.md`](docs/security-model.md).
Operations: [`docs/operations.md`](docs/operations.md).
Dependencies: [`docs/dependencies.md`](docs/dependencies.md).
Design history: `specs/006-notification-service`.

## Place in the platform

```
go-tangra/go-tangra          platform module + @go-tangra/ui kit
        |
go-tangra-auth  <---->  go-tangra-portal (gateway)  <---->  go-tangra-lcm
                                  |
                        go-tangra-notification  <----  warden, auth (Notifier / Events gRPC)
```

- Built on `github.com/go-tangra/go-tangra/v4` (mTLS transports, identity,
  service policy, audit, observability).
- Verifies platform tokens, resolves people and checks permissions through the
  auth SDK (`github.com/go-tangra/go-tangra-auth/sdk/v4`).
- Registers with the gateway through the portal SDK
  (`github.com/go-tangra/go-tangra-portal/sdk/v4`), which fronts the browser API
  and the federated UI remote.
- Can enroll for its SVID with lcm over the network
  (`github.com/go-tangra/go-tangra-lcm/sdk/v4`), as the platform stack does.

The repository holds one Go module, `github.com/go-tangra/go-tangra-notification/v4`.
Other services call it through the `sdk` module (`github.com/go-tangra/go-tangra-notification/sdk/v4`): `pkg/notifyclient` and the `notification.v1` protos.

## Layout

| Path | What |
|------|------|
| `api/openapi/notification.yaml` | browser API contract (served under `/api/notification/v1`) |
| `sdk/api/proto/notification/v1/` | `Notifier` (Send, SendTest) and `Events` (Publish) gRPC for services |
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
| `internal/app`, `cmd/notificationsvc` | wiring and the service binary (serve, `bootstrap`, `version`) |
| `pkg/notificationmanifest` | gateway manifest built from the OpenAPI document |
| `sdk/pkg/notifyclient` | Go client other services use to Send / Publish |
| `deploy` | compose stack, dev configuration, service policy |
| `tests/{contract,fuzz,integration}` | contract, fuzz and Docker-backed integration suites |
| `ui/` | Vue 3 + FlyonUI federated remote on `@go-tangra/ui` (channels, templates, log, messages, inbox, permissions, ops) |

## Build and test

You need Go 1.26, Node 22, Docker (for integration tests and the image), and a
GitHub token with `read:packages` to install `@go-tangra/ui` from GitHub Packages.

```bash
go build ./... && go vet ./... && go test -race ./...
(cd sdk && buf lint)
make test-integration                     # -tags integration, needs Docker (see below)
make lint cover fuzz redaction-scan vuln

cd ui
export NODE_AUTH_TOKEN=$(gh auth token)   # ui/.npmrc only references this variable
npm ci && npm run lint && npm run test:unit && npm run build
```

The unit coverage gate requires at least 80 % overall and 100 % for the
authorization, rendering, sealing, stream and email packages. Generated code,
SQL bindings and wiring are covered by the integration suite instead.

The integration suite runs the real auth and gateway services next to this
one. It builds them from checkouts of
[go-tangra-auth](https://github.com/go-tangra/go-tangra-auth) and
[go-tangra-portal](https://github.com/go-tangra/go-tangra-portal): by default
sibling clones next to this repository, or the directories named by
`GO_TANGRA_AUTH_DIR` and `GO_TANGRA_PORTAL_DIR`. The auth policy contract check
uses the same auth checkout and is skipped without one.

## Run locally

```bash
make compose-up                           # TimescaleDB :5434, Valkey :6381, Mailpit :8027/:1027
go run ./cmd/notificationsvc bootstrap -config deploy/dev.yaml
go run -tags ui ./cmd/notificationsvc -config deploy/dev.yaml   # after the ui build
```

`deploy/dev.yaml` expects a development SVID and a key-encryption key at
`deploy/kek.dev` (git-ignored). The full platform (gateway, auth, lcm and this
service) runs from the go-tangra platform stack (`deploy/stack`), which mounts
its own configuration and development KEK. See
`specs/006-notification-service/quickstart.md` for the end-to-end walkthrough.

## Container image

The image is `ghcr.io/go-tangra/go-tangra-notification`, built by
`.github/workflows/ci.yaml`. It carries `notificationsvc` with the embedded UI remote.

```bash
docker buildx build --secret id=npm_token,env=NODE_AUTH_TOKEN \
  --build-arg APP_VERSION=4.0.0 -t go-tangra-notification:dev .
docker run --rm go-tangra-notification:dev version
```

The image runs `notificationsvc -config deploy/dev.yaml` as user `app` (uid 10001).
It contains no key material: deployments mount their own configuration and
key-encryption key (the platform stack mounts it at `/app/deploy/kek.dev`).

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

## Versioning

- Releases are tagged `vX.Y.Z`. CI publishes the image as `X.Y.Z`, `X.Y`, `X`
  and `sha-<short>`. There is no `latest` tag.
- v4.0.0 rebuilds the service on the go-tangra v4 platform. The v3 line stays on
  the `v3` branch and its `v3.x` tags.
