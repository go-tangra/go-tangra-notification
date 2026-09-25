---

description: "Task list for 017 Central Email Delivery"
---

# Tasks: Central Email Delivery Through the Notification Module

**Input**: Design documents from `specs/017-central-email-delivery/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: MANDATORY (Constitution IV). Test tasks precede implementation in
every phase and must fail before the implementation task runs. Negative
security tests and fuzz tests are included for key parsing, payload decoding,
secrets and redaction.

**Paths**: prefixed by repository — `notification/` = go-tangra-notification,
`auth/` = go-tangra-auth, `warden/` = go-tangra-warden, `docker/` =
go-tangra-docker (branch v4).

## Format: `[ID] [P?] [Story] Description`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: the nested sdk module every caller depends on.

- [X] T001 Create nested module `notification/sdk/go.mod` (`github.com/go-tangra/go-tangra-notification/sdk/v4`, go 1.26, requires grpc + protobuf only) and add `replace github.com/go-tangra/go-tangra-notification/sdk/v4 => ./sdk` + require to `notification/go.mod`
- [X] T002 Move `notification/api/proto/notification/v1` to `notification/sdk/api/proto/notification/v1` (update `go_package`, `notification/buf.gen.yaml`, `notification/buf.yaml`) and `notification/pkg/notifyclient` to `notification/sdk/pkg/notifyclient`; rewrite imports in `notification/internal/**` and `notification/cmd/**`
- [X] T003 [P] Add the sdk module to the CI matrix (go vet/test, buf lint) in `notification/.github/workflows/ci.yaml`; `go build ./... && go test ./...` in both modules

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: contract, config, schema and store changes all stories build on.

### Tests first

- [ ] T004 [P] Contract test for the new proto fields (`template_key`, `retryable`) and the exactly-one-template-ref rule in `notification/tests/contract/notifier_key_test.go`
- [X] T005 [P] Config tests in `notification/internal/config/config_test.go`: `platform_email` defaults (tls starttls), refusals (literal `password`, `tls: none` without `allow_plaintext`, username with `none`, missing host/from, bad port, empty/unreadable/world-readable `password_file`), warning for `allow_plaintext`, `platform_tenant_id` default, `system_send_per_minute` bounds
- [X] T006 [P] Fuzz test for the template key parser (`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]{0,62}$`, service prefix extraction) in `notification/tests/fuzz/systemkey_fuzz_test.go`
- [X] T007 [P] Store tests for managed channel, system template columns, `template_key` on log entries, `channels_one_managed` and `templates_system_key` uniqueness in `notification/internal/store/store_integration_test.go`

### Implementation

- [X] T008 Add `template_key = 7` to `SendRequest` and `retryable = 5` to `SendResponse` in `notification/sdk/api/proto/notification/v1/notification.proto`; regenerate (`buf generate`)
- [X] T009 Add `PlatformEmail`, `PlatformTenantID`, `Limits.SystemSendPerMinute` with Validate/Warnings and `password_file` loading in `notification/internal/config/config.go`
- [X] T010 Migration `notification/internal/store/migrations/0005_system_email.sql` per data-model.md
- [X] T011 Store/repo: `managed` on channels, `system_key`/`builtin_*`/`required_variables`/`secret_variables` on templates, `template_key` on log rows, `TemplateByKey(tenant, key)`, `ManagedChannel(tenant)`, `DefaultEmailChannel(tenant)` in `notification/internal/store/` and `notification/internal/repo/` (+ memstore fakes)
- [X] T012 Template key parser + service-prefix helper in `notification/internal/notify/systemkey.go`

**Checkpoint**: both modules build; foundation tests green.

---

## Phase 3: User Story 1 — One mail relay setting for the whole platform (Priority: P1) 🎯 MVP

**Goal**: notification creates/updates the managed platform channel from config; it is read-only in the UI and testable.

**Independent Test**: only `platform_email` configured → "Platform email" channel exists, managed, test send arrives; config change + restart updates it.

### Tests for User Story 1

- [ ] T013 [P] [US1] Unit tests for `EnsurePlatformChannel` (insert, update on change, no-op when equal, disable when block removed, password sealed, never logged) in `notification/internal/notify/platform_test.go`
- [ ] T014 [P] [US1] Negative tests: update/delete of a managed channel refused (`managed_channel`), permissions `write/delete=false`, test send allowed, in `notification/internal/notify/channels_test.go` and `notification/internal/httpapi/channels_test.go`
- [ ] T015 [P] [US1] Integration test with an in-process SMTP server (STARTTLS with a test CA): start with `platform_email`, test send delivered; relay without STARTTLS → failure `relay offers no STARTTLS`, no plaintext fallback, in `notification/tests/integration/platform_email_test.go`

### Implementation for User Story 1

- [ ] T016 [US1] `EnsurePlatformChannel` (system scope, platform tenant, name "Platform email", default+enabled email, tenant-wide `use` grant, sealed settings) in `notification/internal/notify/platform.go`
- [ ] T017 [US1] Run seeding at start after migrations and before serving (`Wire`), log "platform email channel ready" / "platform email disabled" in `notification/internal/app/wire.go`
- [ ] T018 [US1] Managed guards in `notification/internal/notify/channels.go` (update/remove refused; permissions projection) and HTTP mapping `409 managed_channel` in `notification/internal/httpapi/`
- [ ] T019 [P] [US1] OpenAPI: `managed` on Channel, 409 responses in `notification/api/openapi/notification.yaml`
- [ ] T020 [P] [US1] UI: "Managed" badge, edit/delete disabled, test enabled in `notification/ui/src/views/channels/index.vue`, `notification/ui/src/schemas/channel.ts`; vitest in `notification/ui/tests/`

**Checkpoint**: US1 independently demonstrable (quickstart Scenario 1).

---

## Phase 4: User Story 2 — Invitations and recovery mail from auth arrive (Priority: P1)

**Goal**: key sends with namespace check, channel resolution, redaction and retryable outcomes in notification; auth's outbox delivers through it.

**Independent Test**: auth without relay settings invites a user → mail arrives via notification; log entry shows `[redacted]`; notification down → auth retries and delivers later; dead message reported once.

### Tests for User Story 2 — notification

- [ ] T021 [P] [US2] Unit tests for `EnsureSystemTemplates` (all five keys seeded, existing edited template untouched, builtin_* refreshed, required/secret sets) in `notification/internal/notify/systemtemplates_test.go`
- [ ] T022 [P] [US2] Send-by-key tests in `notification/internal/notify/send_key_test.go`: tenant default channel preferred, platform fallback, disabled tenant channel falls back, none → `email_not_configured` (permanent), missing required variable, unknown key, no grant needed, `system:<service>` rate limit → throttled (retryable)
- [ ] T023 [P] [US2] Negative security tests: auth sending `warden.share` and warden sending `auth.invite` refused + `access_refused` audit; `channel_id` override with key refused; both id and key refused, in `notification/internal/grpcapi/notifier_test.go`
- [ ] T024 [P] [US2] Redaction tests (100 % of `RenderRedacted`): link with `&`, `=`, `<`, quotes in html and text bodies and subjects never appears in stored subject/body/error; audit rows carry no variables, in `notification/internal/render/redact_test.go` and `notification/internal/notify/send_redaction_test.go`
- [ ] T025 [P] [US2] Classification tests (4xx/5xx `textproto.Error`, dial/greeting timeouts, `ErrPlaintext`, x509 errors, throttled) in `notification/internal/channel/email/classify_test.go`
- [X] T026 [P] [US2] Client tests for `SendKey` status mapping (Unavailable/DeadlineExceeded/ResourceExhausted/Aborted → retryable; others → error) in `notification/sdk/pkg/notifyclient/client_test.go`

### Implementation for User Story 2 — notification

- [ ] T027 [US2] Built-in wording and variable sets for `auth.invite`, `auth.account_reset`, `auth.recovery`, `auth.message`, `warden.share` + `EnsureSystemTemplates` in `notification/internal/notify/systemtemplates.go`; call from `notification/internal/app/wire.go`
- [ ] T028 [US2] `RenderRedacted` (second render with secret variables replaced by `[redacted]`) in `notification/internal/render/redact.go`
- [ ] T029 [US2] Retryable classification in `notification/internal/channel/email/classify.go` (wrap provider errors, keep scrubbing)
- [ ] T030 [US2] Key send path in `notification/internal/notify/send.go`: resolve template by key in platform tenant, channel resolution (D5), skip grants, `system:<service>` limiter, store redacted render + `template_key`, scrub secret values from error, set retryable
- [ ] T031 [US2] gRPC handler: exactly-one ref, key regex, namespace check from verified SPIFFE id, `retryable` in response, error mapping in `notification/internal/grpcapi/notifier.go`
- [X] T032 [US2] `SendKey` + `Result` in `notification/sdk/pkg/notifyclient/client.go`
- [ ] T033 [US2] Integration test: auth-like caller identity sends `auth.invite` through the in-process SMTP server; stored log row, audit rows and captured logs contain no token (redaction scan) in `notification/tests/integration/system_send_test.go`

### Tests for User Story 2 — auth

- [ ] T034 [P] [US2] Payload tests: v2 encode/decode, legacy `{subject,text}` decoded as `auth.message`, AAD unchanged; fuzz the decoder in `auth/internal/email/outbox_test.go` and `auth/tests/fuzz/outbox_payload_fuzz_test.go`
- [ ] T035 [P] [US2] Outbox tests with a fake Deliverer: sent → MarkSent; retry → stays pending; permanent → failed_at set, one `email_given_up` report; attempts > max → retired once; no re-report on later passes, in `auth/internal/email/outbox_test.go`
- [ ] T036 [P] [US2] Store tests for `ClaimOutbox` (exponential `next_attempt_at` capped at 1 h, `failed_at` filter) and `MarkOutboxFailed` in `auth/internal/store/outbox_integration_test.go`
- [ ] T037 [P] [US2] Notification deliverer tests with a fake Notifier gRPC server (lazy connection on first use, key + vars + correlation id passed, retryable/permanent mapping, notification down → retry) in `auth/internal/email/notify_test.go`
- [ ] T038 [P] [US2] Producer tests: invite/resend/LDAP activation enqueue `auth.invite` {link, valid_for "72 hours", tenant}; bootstrap operator `auth.invite` {valid_for "7 days"}; reset `auth.account_reset`; recovery `auth.recovery` {valid_for "30 minutes"} — in `auth/internal/invite/invite_test.go`, `auth/internal/password/recovery_test.go` (keep 100 %), `auth/internal/app/reset_test.go`
- [ ] T039 [P] [US2] Config tests: `transport: notification` default, `log` refused in production, `smtp` + relay keys accepted and reported in one warning in `auth/internal/config/config_test.go`

### Implementation for User Story 2 — auth

- [ ] T040 [US2] Require `github.com/go-tangra/go-tangra-notification/sdk/v4 v4.2.0` in `auth/go.mod`
- [ ] T041 [US2] Migration `auth/internal/store/migrations/0009_outbox_retire.sql`; `ClaimOutbox` backoff + filter and `MarkOutboxFailed` in `auth/internal/store/repos.go`; `auth/internal/email/emaildb/db.go`; memstore fake
- [ ] T042 [US2] Payload v2 + legacy decode, `Deliverer` interface, outcome handling and single give-up report in `auth/internal/email/outbox.go`
- [ ] T043 [US2] Notification deliverer (lazy `Freya.Client(ctx, "notification")`, `SendKey`, platform tenant/tenant id from the item) in `auth/internal/email/notify.go`; log deliverer keeps printing the link (dev)
- [ ] T044 [US2] Producers enqueue template key + vars in `auth/internal/invite/invite.go`, `auth/internal/password/recovery.go`, `auth/internal/app/app.go` (bootstrap), `auth/internal/app/reset.go`
- [ ] T045 [US2] Config: `email.transport` notification|log, deprecated keys kept + warning, in `auth/internal/config/config.go`; wiring in `auth/internal/app/app.go`
- [ ] T046 [US2] Integration harness: fake Notifier gRPC server recording key/vars replaces Mailpit; `LastMail` returns the rendered link; update `auth/tests/integration/*` and `auth/deploy/{dev,dev-standalone,gateway-mode}.yaml`, `auth/deploy/compose.yaml`
- [ ] T047 [US2] Docs: `auth/docs/operations.md` (email section, given-up messages), `auth/docs/security-model.md` (links leave auth only through notification)

**Checkpoint**: invitations flow end to end through notification (quickstart Scenario 2).

---

## Phase 5: User Story 3 — Share links from warden arrive (Priority: P2)

**Goal**: warden sends `warden.share` through notification; failure cancels the share as today.

**Independent Test**: warden without relay settings shares a secret → mail arrives via notification, link redacted in the log; notification down → share cancelled, user told.

### Tests for User Story 3

- [ ] T048 [P] [US3] Share tests with a fake sender: `warden.share` key, vars {link (secret), secret_name, expires, openings, message}; failure → cancelled + `ErrMail` in `warden/internal/share/share_test.go`, `warden/internal/httpapi/share_test.go`
- [ ] T049 [P] [US3] Notification sender tests with a fake Notifier server (lazy connection, retryable → error, permanent → error) in `warden/internal/share/notify_test.go`
- [ ] T050 [P] [US3] Config tests: `mail.transport` notification default, `log` dev only, relay keys accepted + warned in `warden/internal/config/config_test.go`

### Implementation for User Story 3

- [ ] T051 [US3] Require the notification sdk in `warden/go.mod`
- [ ] T052 [US3] `Message{To, Template, Vars}` and notification sender in `warden/internal/share/mail.go` / `warden/internal/share/notify.go`; `Create` builds vars in `warden/internal/share/share.go`
- [ ] T053 [US3] Wiring with `Freya.Client(ctx, "notification")` in `warden/internal/app/wire.go`, `warden/internal/app/app.go`; config in `warden/internal/config/config.go`
- [ ] T054 [P] [US3] Docs `warden/docs/` (mail now through notification)

**Checkpoint**: quickstart Scenario 3 passes.

---

## Phase 6: User Story 4 — Operators adjust the wording of platform emails (Priority: P3)

**Goal**: system templates editable (subject/body), required variables enforced, delete refused, restore available.

**Independent Test**: edit `auth.invite` subject → next invite uses it and survives restart; removing `{{.link}}` refused; restore works.

### Tests for User Story 4

- [ ] T055 [P] [US4] Template guard tests: only subject/body editable, missing required variable refused with its name, delete refused, restore resets, `edited` flag, in `notification/internal/notify/templates_test.go`
- [ ] T056 [P] [US4] HTTP tests for `PUT` 422 reasons, `remove` 409, `POST /templates/{id}/restore` (200 / 409 non-system / permission) in `notification/internal/httpapi/templates_test.go`

### Implementation for User Story 4

- [ ] T057 [US4] System template guards + `Restore` in `notification/internal/notify/templates.go`
- [ ] T058 [US4] Route `POST /templates/{id}/restore`, read model fields (`system_key`, `required_variables`, `secret_variables`, `edited`) in `notification/internal/httpapi/` and `notification/api/openapi/notification.yaml`
- [ ] T059 [US4] UI: "System" badge, delete hidden, "Restore built-in" action, required variables shown in `notification/ui/src/views/templates/index.vue`, `notification/ui/src/stores/templates.ts`; vitest

**Checkpoint**: quickstart Scenario 4 passes.

---

## Phase 7: Polish, Deployment & Release

- [ ] T060 [P] notification docs: `notification/docs/` (platform_email, system templates, redaction), `notification/CHANGELOG.md` 4.2.0, `notification/README.md`
- [ ] T061 [P] notification `deploy/dev.yaml` `platform_email` (Mailpit, `tls: none`, `allow_plaintext: true`)
- [ ] T062 Coverage gates and `govulncheck` in notification, auth, warden; fix gaps
- [ ] T063 Release notification: PR, CI, tag `sdk/v4.2.0` then `v4.2.0` (confirm with the user before tagging)
- [ ] T064 Release auth v4.2.0 and warden v4.2.0 against sdk/v4.2.0 (confirm with the user)
- [ ] T065 [P] docker: `configs/notification.yaml` `platform_email` (Mailpit); `configs/auth.yaml` / `configs/warden.yaml` `transport: notification` without relay keys
- [ ] T066 docker: `scripts/prod-init.sh` writes `platform_email` from `SMTP_*` (tls from port 465/587), password to `prod/secrets/smtp.password`; drop auth/warden relay rewrites; overlay mounts the secret into notification in `docker-compose.production.yaml.example`
- [ ] T067 [P] docker: refresh `policies/notification.yaml` from notification 4.2.0; `.env.example` versions (notification/auth/warden 4.2.0)
- [ ] T068 docker: `PRODUCTION.md` — single relay setting, certificate host-name rule, verifying delivery (log entry, `sent_at`), upgrade steps for existing installs (move `email:`/`mail:` values into `platform_email`), troubleshooting rows
- [ ] T069 Run quickstart.md scenarios 1–4 on the development stack; record results in `specs/017-central-email-delivery/quickstart-results.md`

---

## Dependencies & Execution Order

- **Setup (T001–T003)** → **Foundational (T004–T012)** → user stories.
- **US1 (T013–T020)**: needs Foundational.
- **US2 notification (T021–T033)**: needs Foundational; T030 uses the platform channel from US1 for fallback tests.
- **US2 auth (T034–T047)** and **US3 (T048–T054)**: need sdk/v4.2.0 contents (T008, T032). During development use a local `replace` to `../go-tangra-notification-v4/sdk`, removed before release (T064).
- **US4 (T055–T059)**: needs T027 (seeded templates).
- **Release/deploy (T063–T069)**: after all stories; notification released before auth/warden.

### Parallel Opportunities

- T004–T007 in parallel; T013–T015; T021–T026; T034–T039; T048–T050; T055–T056.
- auth (T034–T047) and warden (T048–T054) proceed in parallel in separate repos.
- UI tasks (T020, T059) parallel to backend work of the same story after the API contract is fixed.

## Parallel Example: User Story 2 (notification tests)

```text
T021 systemtemplates_test.go
T022 send_key_test.go
T023 notifier_test.go (namespace)
T024 redact_test.go
T025 classify_test.go
T026 client_test.go
```

## Implementation Strategy

### MVP

Phases 1–3 (US1) + Phase 4 (US2): one relay setting and invitations through
notification — the operational problem that motivated the feature. Release
notification 4.2.0 and auth 4.2.0 first; warden can follow.

### Incremental Delivery

1. US1 → platform channel visible and testable.
2. US2 → invitations/recovery via notification (MVP complete).
3. US3 → warden share mail.
4. US4 → wording editable.
5. Deployment/docs → single relay setting in production.
