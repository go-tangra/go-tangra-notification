# Dependencies

Every direct dependency is justified here (Constitution VI). Versions are pinned by `go.sum` / `package-lock.json`; `govulncheck` and `npm audit --audit-level=high` run in CI.

| Dependency | Purpose | Alternatives rejected | Maintenance |
|------------|---------|-----------------------|-------------|
| `github.com/go-freya/freya` (`replace ../..`) | mTLS transports, identity, service policy, audit, observability | — | this repository |
| `github.com/go-freya/freya/services/auth` (`pkg/authclient`) | verify the platform token forwarded by the gateway; resolve members (`Profiles.ListMembers`) and decisions (`Authorization.Check`) | re-implementing JWT/revocation checks | this repository |
| `github.com/go-freya/freya/services/gateway` (`pkg/gatewayclient`, `api/schema`) | gateway registration, manifest types and schema | — | this repository |
| `github.com/jackc/pgx/v5` | TimescaleDB driver and pool (per-call tenant transactions, RLS) | database/sql + lib/pq | Active |
| `github.com/pressly/goose/v3` | embedded SQL migrations | golang-migrate | Active |
| `github.com/valkey-io/valkey-go` | live-event streams (`XADD`/`XREAD`) and rate-limit counters (research R4) | go-redis | Active |
| `github.com/getkin/kin-openapi` | OpenAPI parsing and per-route request validation for the browser API | manual validation per handler | Active |
| `github.com/santhosh-tekuri/jsonschema/v6` | validate the backup document against `backup.schema.json` and the manifest against the gateway schema | hand-written checks | Active |
| `github.com/golang-jwt/jwt/v5` | (test) mint platform tokens for the httpapi tests | — | Active |
| `google.golang.org/grpc`, `google.golang.org/protobuf` | the `notification.v1` service-to-service API | — | Google, active |
| `gopkg.in/yaml.v3` | configuration and policy parsing | — | Active |

**Standard library only** for the security-sensitive paths: `net/smtp` +
`crypto/tls` for email delivery (research R2), `mime`/`mime/quotedprintable`
for MIME assembly, `text/template`+`html/template` for rendering (research
R3), `crypto/aes`+`crypto/cipher` for the AES-256-GCM envelope (research R1).
No third-party SMTP, template, or crypto library is used.

## UI

| Dependency | Purpose |
|------------|---------|
| `vue`, `vue-router`, `pinia` | the SPA framework, routing and state |
| `vuetify`, `@mdi/font` | the Materio-styled component library |
| `@casl/ability`, `@casl/vue` | ability checks mirrored from the gateway's decisions |
| `@module-federation/vite` | build the remote consumed by the shell |
| `vitest`, `@vue/test-utils`, `jsdom` | unit tests |
| `@playwright/test`, `@axe-core/playwright` | end-to-end tests with accessibility checks |
| `openapi-typescript` | generate request/response types from the OpenAPI contract |
