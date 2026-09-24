# Tasks: Notification Service

**Input**: Design documents from `/specs/006-notification-service/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests are mandatory** (Constitution IV): every story lists its tests before
its implementation; security packages (`internal/{authz,render,sealed,stream,channel/email}`)
are gated at 100 % coverage, the module at ≥ 80 %.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1–US5 from spec.md; setup, foundational and polish tasks carry no label
- Every task names the file(s) it produces or changes

## Path Conventions

Platform module `services/notification/` mirroring `services/warden/`
(plan.md §Project Structure): Go under `cmd/`, `internal/`, `pkg/`, `api/`,
`tests/`; the federated remote under `ui/`; cross-service changes under
`services/auth/` and `services/gateway/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: module skeleton, contracts in place, dev stack, CI

- [X] T001 Create the service module skeleton per plan.md (`go.mod` with `replace github.com/go-freya/freya => ../..` and requires on `services/auth` and `services/gateway` pkg paths, `cmd/notificationsvc`, `internal/{config,store,memstore,sealed,audit,authz,channel,channel/email,render,notify,messages,inbox,stream,transfer,stats,httpapi,grpcapi,app}`, `pkg/{notificationmanifest,notifyclient}`, `deploy`, `docs`, `scripts`, `tests/{contract,fuzz,integration}`, `doc.go` per package) in services/notification/
- [X] T002 [P] Copy contracts into the module and generate code: `specs/006-notification-service/contracts/notification.v1.proto` → `services/notification/api/proto/notification/v1/notification.proto` (buf.yaml, buf.gen.yaml, `*.pb.go`), `notification-api.openapi.yaml` → `services/notification/api/openapi/notification.yaml` (embedded via `api/openapi/openapi.go`), `backup.schema.json` → `services/notification/api/schema/backup.schema.json` (embedded)
- [X] T003 [P] Makefile mirroring services/warden (lint = vet+staticcheck+gosec, vuln, test, test-integration, cover with `COVERPKG` excluding `api/proto`, `*db`, `store`, `app`, `cmd`, `tests`, `ui`; fuzz; generate; ui-build; redaction-scan; compose-up/down with `-p notification`) and `scripts/{coverage-gate.sh (100 % for internal/{authz,render,sealed,stream,channel/email}), redaction-scan.sh (marker corpus: `NOTIF-MARKER-PW-`, `NOTIF-MARKER-BODY-`)}` in services/notification/Makefile and services/notification/scripts/
- [X] T004 [P] Development stack `services/notification/deploy/{compose.yaml (TimescaleDB :5434 with `notification` database + `notification_app` role via init-db.sql, Valkey :6381 with `&*` channel ACL, Mailpit :8027/:1027), dev.yaml (kek.source file → deploy/kek.dev, smtp.allow_plaintext true, limits.max_request_bytes 16842752, scheduler.interval 15s, gateway service "gateway"), policy.yaml (rules from contracts/manifest.md; notification → auth.v1 Keys/List, Sessions/RevokedSince|Watch, Profiles/Lookup|ListMembers, Authorization/RegisterPermissions; notification → gateway.v1.Registry)}` and `.gitignore` for `deploy/kek.dev`
- [X] T005 [P] UI scaffold `services/notification/ui/` copied from services/warden/ui (Vite 8 + Vue 3.5 + Vuetify 4 + vue-router 5 + Pinia 4 + TypeScript 5.9, `@mdi/font`, `@module-federation/vite` exposing `./routes`, `./nav` and `./header`, `npm run gen:api` from api/openapi/notification.yaml, Vitest + jsdom, Playwright + axe with `testIdAttribute: 'data-test'` and `PW_CHANNEL`) with `package.json`, `vite.config.ts`, `module-federation.config.ts`, `tsconfig*.json`, `src/main.ts` (standalone dev shell using the Materio theme tokens copied from services/gateway/shell/src/theme), `src/remote/{routes.ts,nav.ts,header.ts}`
- [X] T006 [P] Dependency justification `services/notification/docs/dependencies.md` (research R2/R3: stdlib SMTP, MIME, templates, AES-GCM; Valkey client, pgx, goose, kin-openapi already vetted; frontend list) and `.github/workflows/ci.yml` jobs `notification-service` (lint, vuln, test, cover, fuzz smoke, ui lint/unit/build/audit) and `notification-service-integration` (`-tags integration` with compose, Playwright)
- [X] T007 [P] Root `Makefile` `testca` services list gains `notification`; root `.gitignore` gains `services/notification/deploy/kek.dev`; root `README.md` and `CHANGELOG.md` mention the module

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: config, schema, sealed settings, audit, authz core, identity middleware, gateway registration, cross-service prerequisites — everything every story needs

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Tests (write first)

- [X] T008 [P] Unit tests for `config`: defaults (secure: KEK required, SMTP TLS required unless `smtp.allow_plaintext`, rate limits 600/60 per minute, 5 streams per user, replay window 5 min, scheduler interval 15 s), `Validate` (production refuses `smtp.allow_plaintext`, missing KEK source, weak db sslmode, limits below 16 MiB for backup import), `Warnings` in services/notification/internal/config/config_test.go
- [X] T009 [P] Migration + repository tests (`//go:build integration`): tables from data-model.md under RLS (`tenant_isolation` on all eight tables), hypertables + retention, partial unique indexes (default channel per type, default template per channel, `lower(name)` per tenant), `UNIQUE (message_id, recipient_id)` on inbox, scheduler claim query returns each due message to exactly one of two concurrent claimers, cross-tenant reads return nothing, app role grants in services/notification/internal/store/migrate_test.go
- [X] T010 [P] Unit tests for `sealed`: seal/open round trip with associated data `channel:<id>`, tampered ciphertext refused, wrong KEK refused, `Redact(settings, secretFields)` replaces credential values with `"__set__"`, `Merge(stored, incoming)` keeps stored values for `"__set__"`/omitted and clears on `""`, `Scrub(text, stored)` removes every stored credential value from an error string, KEK loading from file/env (32 bytes, 0600) in services/notification/internal/sealed/sealed_test.go
- [X] T011 [P] Unit tests for `audit`: closed vocabulary from data-model.md, required fields, detail guard drops keys containing `password`, `secret`, `token`, `api_key`, `body`, `content`, `settings` and truncates strings > 256, marker corpus never stored, batching, `Flush` in services/notification/internal/audit/audit_test.go
- [X] T012 [P] Unit tests for `authz` (memstore): relation → actions lattice incl. `use` (owner/editor/sharer yes, viewer no), subjects from identity (user id, effective role slugs, tenant), tenant owner/admin roles imply owner, expired grants ignored, granter cannot exceed own relation (`relation_above_granter` audited), repeated grant replaces, `Check`, `Grant`, `Revoke`, `ListGrants`, `Effective` with contributing grants, cross-tenant not_found, default-channel tenant grant materialised and removed when the flag moves in services/notification/internal/authz/authz_test.go
- [X] T013 [P] Unit tests for `httpapi` skeleton: OpenAPI validation (unknown fields, lengths, uuid params, CR/LF patterns), `{"reason","detail"}` errors only, 5xx → `temporarily_unavailable`, `authclient` middleware on every route (401 without/with forged token, tenant and roles from the token), per-route `x-freya-max-body-bytes` honoured, security headers in services/notification/internal/httpapi/server_test.go
- [X] T014 [P] Contract tests: OpenAPI document parses, every declared route mounted and every mounted route declared, every operation carries `x-freya-permission` from the manifest's permission list, no public route, proto shapes, manifest builds from the document (37 paths, 13 permissions, 9 abilities, 7 nav entries, `./header` expose, timeouts 300/120/60, 16 MiB on backup import), `policy.yaml` refuses an unlisted SPIFFE id on `Notifier/Send` in services/notification/tests/contract/{openapi_test.go,grpc_test.go,manifest_test.go,policy_test.go}
- [X] T015 [P] Integration harness (`//go:build integration`): testcontainers TimescaleDB, Valkey, Mailpit; gateway and auth built from `../gateway` / `../auth` as subprocesses like feature 005's harness; `Env` with `Seed(tenant, users[], roles)`, `Token(user)`, `JSON`, `Raw`, `Stream(user) (SSE reader with Last-Event-ID)`, `AuditCount`, `Mailpit` (list/clear/stop/start via the moby client), `Valkey` pause/unpause, `ServiceCall(spiffe)` (mTLS client with a test identity for gRPC), `ScanForCredentials(markers)` over DB, audit, logs and responses; `TestHarnessBoots` in services/notification/tests/integration/harness_test.go
- [X] T016 [P] Cross-service tests (contracts/auth-changes.md): `auth.v1.Profiles/ListMembers` paging over 2,500 synthetic active members, deactivated users excluded, cursor stability, refusal for an unlisted identity in services/auth/internal/grpcapi/profiles_test.go and services/auth/tests/contract/policy_test.go
- [X] T017 [P] Cross-service tests (contracts/shell-changes.md): manifest schema accepts `./header`, shell unit test `tests/unit/header-slot.spec.ts` (stubbed remote exposing `./header` renders in the app bar with `data-test="header-<module>"`, a remote without it renders nothing, a throwing header component is isolated) in services/gateway/internal/manifest/manifest_test.go and services/gateway/shell/tests/unit/header-slot.spec.ts

### Implementation

- [X] T018 Implement `config` (Freya `config.Config` inline + `DB`, `Valkey`, `KEK{Source, Path, Env}`, `SMTP{AllowPlaintext, DialTimeout}`, `Gateway{Service}`, `Limits{BackupMaxBytes, SendPerTenantPerMinute, SendPerSenderPerMinute, StreamsPerUser, StreamsPerTenant, ReplayWindow}`, `Scheduler{Interval, LeaseDuration}`) with `Load/Validate/Warnings` in services/notification/internal/config/config.go
- [X] T019 Migrations `0001_schema.sql` (channels, templates, grants, message_categories, messages, inbox + indexes from data-model.md), `0002_hypertables.sql` (notification_log, notification_audit_events, retention 400 d), `0003_rls.sql` (policies via `app_tenant_matches`), `0004_grants.sql` (notification_app) and `store.Open/Migrate/Tx(Scope)` copied from warden in services/notification/internal/store/{store.go,migrations/*.sql}
- [X] T020 Models and repositories: `Channel`, `Template`, `LogRow`, `Grant`, `Category`, `Message`, `InboxRow`, `AuditRow` and functions `Insert/Get/List/Update/Delete` per table, `ClearDefault(tenant, type)`, `TemplateCountByChannel`, `LogPage(filter)`, `LogSetOutcome`, `ExpirePendingLogs`, `GrantsFor(resource)`, `GrantsBySubjects`, `AccessibleIDs(resourceType, subjects)`, `ClaimDueMessages(now, lease, limit)`, `InsertInboxBatch(ON CONFLICT DO NOTHING)`, `InboxPage(recipient, filter)`, `InboxUnread`, `InboxSetStatus(ids, recipient)`, `RevokeUnread(message)`, `Stats`, `InsertAuditRows`, `QueryAudit(with subject_name)` in services/notification/internal/store/{models.go,repos.go}
- [X] T021 [P] In-memory double `memstore.Store` implementing every repository interface with the same semantics (RLS by tenant argument, uniqueness, partial-unique defaults, claim exclusivity, `FailOn(op, err)` injection) in services/notification/internal/memstore/memstore.go
- [X] T022 [P] `sealed` package: `Envelope` (AES-256-GCM DEK wrapped by the KEK, copied from services/auth/internal/crypto/envelope.go), `LoadKEK(cfg)`, `Redact`, `Merge`, `Scrub`, `SecretFields(channelType)` in services/notification/internal/sealed/sealed.go
- [X] T023 [P] `audit` package: `Event`, `EventType` vocabulary, `Writer` (batched, `Flush`), detail guard, `Query` with `subject_name` and `auditdb` inserter/querier in services/notification/internal/audit/{audit.go,query.go,auditdb/db.go}
- [X] T024 `authz` package copied from warden without ancestors: `Subjects(identity)`, `Check(resource, action)`, `Grant`, `Revoke`, `ListGrants`, `Effective`, `AccessibleIDs`, `EnsureOwner(creator, resource)`, `SetDefaultTenantGrant(channel, on/off)`, uniform not_found, audit of refusals in services/notification/internal/authz/{authz.go,grants.go}
- [X] T025 `httpapi` skeleton: `Server` with embedded OpenAPI, kin-openapi validation with per-route body limits, `authclient.Middleware` on every route, `Caller` accessor (`user id`, `tenant`, `roles`, `stats:read` flag), error helpers incl. `detail`, `MustHandle`/`Declared`/`Implemented`, security headers in services/notification/internal/httpapi/{server.go,middleware.go,errors.go}
- [X] T026 [P] `pkg/notificationmanifest`: builds the gateway manifest from the embedded OpenAPI document (`x-freya-permission`, `x-freya-max-body-bytes`, `x-freya-timeout-seconds`), permissions, abilities, nav, `remote.exposes` incl. `./header` from contracts/manifest.md, `Version = "1.0.0"` in services/notification/pkg/notificationmanifest/manifest.go
- [X] T027 `app.Build`: Freya app, DB, Valkey, KEK/envelope, audit writer, verifier (`authclient` with keys/revocations from auth), HTTP + gRPC servers, health (`db`, `valkey`, `scheduler_last_tick`), gateway registration with lease renewal, builtin grants registration (`app/permissions.go`, research R12), route coverage check in services/notification/internal/app/{app.go,wire.go,permissions.go}; `cmd/notificationsvc/{main.go (run), bootstrap.go (migrate, KEK check, `rotate-kek` re-sealing channels)}`
- [X] T028 Cross-service (contracts/auth-changes.md): `Profiles.ListMembers` in services/auth/api/proto/auth/v1/auth.proto (+ generated code), `store.ListActiveMemberIDs(tenant, after, limit)` in services/auth/internal/store/, handler in services/auth/internal/grpcapi/profiles.go, memstore double, `deploy/policy.yaml` entries for `spiffe://example.org/svc/notification` (ListMembers, Lookup, RegisterPermissions, Keys/List, Sessions/RevokedSince|Watch)
- [X] T029 Cross-service (contracts/shell-changes.md): `./header` in services/gateway/api/schema/manifest.schema.json and services/gateway/internal/manifest validation; `HeaderExpose`, `headerSlots` map, load in `mountModule`, removal in `unmountModule` in services/gateway/shell/src/federation/boot.ts; render slots inside `RemoteBoundary` in services/gateway/shell/src/layouts/Default.vue; docs services/gateway/docs/module-guide.md and specs/003-application-gateway/contracts/federation.md updated; dev allow-list line documented in services/gateway/README.md

**Checkpoint**: `notificationsvc bootstrap` migrates and loads the KEK; the service registers with the gateway (manifest 1.0.0 with `./header`); every route answers 401/403 correctly with no handler yet; the shell renders header slots

---

## Phase 3: User Story 1 - Configure Channels and Templates, Send a Notification (Priority: P1) 🎯 MVP

**Goal**: email channel with sealed settings and test send, validated templates with preview, render + deliver + immutable log with filters (FR-001…FR-012, SC-001…SC-003)

**Independent Test**: quickstart §3 — channel against Mailpit, test send, template with two variables, preview, send with variables lands in Mailpit with a `sent` log entry; relay down → `failed`; the relay password never appears anywhere

### Tests for User Story 1 (MANDATORY) ⚠️

- [X] T030 [P] [US1] Unit tests for `channel/email` (in-process fake SMTP server): STARTTLS negotiated, implicit TLS on 465, plaintext refused unless allowed, PLAIN auth only over TLS, dial timeout, MIME message (multipart/alternative, quoted-printable, Message-ID, Date), header validation rejects CR/LF and control characters in recipient/subject/from/reply-to, `net/mail` single-address recipient, provider error scrubbed of credentials, `Validate(settings)` rules, `Secret()` fields; `nop` providers report `no_provider` in services/notification/internal/channel/email/email_test.go and services/notification/internal/channel/channel_test.go
- [X] T031 [P] [US1] Fuzz tests `FuzzHeaderValue`, `FuzzRecipient` (no panic, never a CR/LF in the produced message) in services/notification/tests/fuzz/email_fuzz_test.go
- [X] T032 [P] [US1] Unit tests for `render`: parse subject/body, syntax error position, undeclared variable named, declared-unused allowed, fixed FuncMap only (calling `env`/`exec`/unknown → refused), variables are strings only, HTML escaping for email bodies, `missingkey=error`, 1 s deadline and 1 MiB limit enforced, preview equals send rendering in services/notification/internal/render/render_test.go
- [X] T033 [P] [US1] Fuzz tests `FuzzTemplate` (parse/validate never panics, never executes outside the FuncMap), `FuzzVariables` (`{{` in values never renders as code) in services/notification/tests/fuzz/render_fuzz_test.go
- [X] T034 [P] [US1] Unit tests for `notify` (memstore, fake provider): send pipeline order (pending row before delivery, `sent`/`failed` after), channel resolution (template's or override; disabled → `channel_disabled`; type mismatch → `validation_failed`), `use` required on template and channel, missing variable refused, recipient validated per type, rate limits (601st per tenant, 61st per sender → `rate_limited`), test send with the built-in template and `test=true`, `ExpirePending` marks stale rows, log listing filters and the `sender = caller` restriction without `stats:read`, every outcome audited, no credential in any error in services/notification/internal/notify/notify_test.go
- [X] T035 [P] [US1] Unit tests for `httpapi` channels/templates/notifications routes: channel create/update/delete/test (redacted settings, `"__set__"` merge, default flag moves, delete refused with `detail.templates`), template CRUD/preview (422 `detail.position` / `detail.variable`), send (200 with entry, 403 without use, 422 variants, 429), log list/get (rendered_body only on get) in services/notification/internal/httpapi/{channels_test.go,templates_test.go,notifications_test.go}
- [X] T036 [P] [US1] gRPC unit tests: `Notifier/Send` and `SendTest` with a service identity acting for a tenant (tenant-wide `use` required, `PERMISSION_DENIED` otherwise, `INVALID_ARGUMENT` for bad input, audited as `actor_kind=service`) in services/notification/internal/grpcapi/notifier_test.go
- [X] T037 [P] [US1] Integration tests `TestChannels`, `TestTemplates`, `TestSend` per quickstart §3 (Mailpit delivery, stop Mailpit → `failed` with scrubbed reason, `ScanForCredentials` after every step, SC-002 timing) in services/notification/tests/integration/send_test.go
- [X] T038 [P] [US1] UI unit tests (Vitest): `stores/channels.ts`, `stores/templates.ts`, `stores/log.ts`; `ChannelDrawer` (password field write-only with "set" indicator, test-send action), `TemplateDrawer` (variable chips, preview pane, validation errors shown by position/variable), `SendDialog`, `LogTable` with filters in services/notification/ui/tests/unit/{channels.spec.ts,templates.spec.ts,log.spec.ts}
- [X] T039 [P] [US1] Playwright specs `channels.spec.ts` (create Mailpit channel, test send, see the mail via `E2E_MAIL`), `templates.spec.ts` (create with `{{.Name}}`, preview, send, open the log entry) with axe checks in services/notification/ui/tests/e2e/

### Implementation for User Story 1

- [X] T040 [P] [US1] `channel` package: `Provider` interface (`Type`, `Validate(settings)`, `Secret() []string`, `Send(ctx, settings, Message)`), registry by type, `nop` providers for sms/slack/sse in services/notification/internal/channel/channel.go; `email` provider (net/smtp, MIME builder, header validation, TLS modes, dial timeout, error scrubbing) in services/notification/internal/channel/email/{email.go,mime.go}
- [X] T041 [P] [US1] `render` package: `Parse(subject, body, kind)` → `Compiled` with referenced variables, `Validate(declared)`, `Render(ctx, values)` with deadline and limited writer, fixed `FuncMap`, `Preview` in services/notification/internal/render/render.go
- [X] T042 [US1] `notify` package: `Channels` service (create/update/delete/test/list/get with sealed settings, default-flag move + tenant grant via authz, template-count guard), `Templates` service (CRUD with render validation, default per channel, preview), `Send` pipeline (research R8) with Valkey rate limiter, built-in test template, `ListLog`/`GetLog`, `ExpirePending` ticker in services/notification/internal/notify/{channels.go,templates.go,send.go,log.go}
- [X] T043 [US1] `httpapi` handlers for channels, templates, notifications routes of the OpenAPI document in services/notification/internal/httpapi/{channels.go,templates.go,notifications.go}
- [X] T044 [US1] `grpcapi` `Notifier` service (Send, SendTest) with service-identity caller, tenant from the request, policy enforced by the Freya server in services/notification/internal/grpcapi/notifier.go; `pkg/notifyclient` (`New(freya)`, `Send`, `SendTest`) in services/notification/pkg/notifyclient/client.go
- [X] T045 [P] [US1] UI stores `channels.ts`, `templates.ts`, `log.ts`, `api/{client.ts,types.ts}` (generated types) and `directory.ts` (user/role name resolution copied from warden) in services/notification/ui/src/{api,stores}/
- [X] T046 [P] [US1] UI views `views/channels/index.vue` (table, `ChannelDrawer` with type-specific settings form, enable/default toggles, delete, test send), `views/templates/index.vue` (table, `TemplateDrawer` with subject/body editors, variable chips, `PreviewPane`, `SendDialog`), `views/log/index.vue` (`LogTable` with filters, entry drawer with rendered body) in services/notification/ui/src/views/ and services/notification/ui/src/components/
- [X] T047 [US1] Routes `/notification/channels`, `/notification/templates`, `/notification/log` in services/notification/ui/src/remote/routes.ts; `ui/embed.go` (`-tags ui`) and `app` serving `/ui/` in services/notification/ui/embed.go and services/notification/internal/app/app.go

**Checkpoint**: quickstart §3 passes; the shell shows Channels, Templates, Log; a notification sent through Mailpit is logged as `sent` and the password never leaks

---

## Phase 4: User Story 2 - Decide Who May Read, Edit, Share and Use (Priority: P1)

**Goal**: grants on channels and templates with the `use` action, effective permissions, permission manager UI (FR-013…FR-016, SC-004)

**Independent Test**: quickstart §4 — Editor/Viewer/Sharer matrix incl. `use`, revoke effective on the next request, Sharer cannot grant above own relation

### Tests for User Story 2 (MANDATORY) ⚠️

- [X] T048 [P] [US2] Unit tests for `httpapi` grants/access routes: list (requires read), grant (requires share, relation ≤ own, replace), revoke, check, effective for self and for a subject (share required), expired flag in services/notification/internal/httpapi/grants_test.go
- [X] T049 [P] [US2] Integration test `TestAccess` per quickstart §4 (matrix on a template and a channel incl. `use` via role through a group, tenant-wide `use` on a default channel, expiry with the clock advanced, cross-tenant 404, effective sources, every grant/refusal audited) in services/notification/tests/integration/access_test.go
- [X] T050 [P] [US2] UI unit tests: `stores/permissions.ts` (grant/revoke/effective, `grantable(held)`), `PermissionDrawer` (subject pickers from auth search/roles, resolved names, `use` shown in the matrix), `views/permissions` resource picker in services/notification/ui/tests/unit/permissions.spec.ts
- [X] T051 [P] [US2] Playwright spec `permissions.spec.ts` (grant Sharer to a role on a template, second browser context sends with it, revoke, send refused) with axe in services/notification/ui/tests/e2e/permissions.spec.ts

### Implementation for User Story 2

- [X] T052 [US2] `httpapi` handlers for grants, revoke, access check and effective in services/notification/internal/httpapi/grants.go
- [X] T053 [P] [US2] UI `stores/permissions.ts`, `components/PermissionDrawer.vue` (copied from warden with the `use` action), `views/permissions/index.vue` (pick a channel or template, open the drawer), "Manage access" actions in the channel and template drawers in services/notification/ui/src/
- [X] T054 [US2] Route `/notification/permissions` in services/notification/ui/src/remote/routes.ts; `permissions` list in the `Channel`/`Template` views drives per-row actions (edit/delete/send/share)

**Checkpoint**: quickstart §4 passes; a Sharer can send and delegate `use` without editing rights

---

## Phase 5: User Story 3 - Internal Messages and the Inbox (Priority: P2)

**Goal**: categories, messages with fan-out to inbox rows, scheduled publishing by the lease-loop worker, revoke, per-user inbox (FR-017…FR-022, SC-005, SC-006)

**Independent Test**: quickstart §5 — message to two users and one scheduled to everyone; unread counts; read/delete; scheduled publish within one interval; kill-during-publish → exactly once

### Tests for User Story 3 (MANDATORY) ⚠️

- [X] T055 [P] [US3] Unit tests for `messages` (memstore, fake auth Profiles client, fake clock): categories CRUD with delete-in-use refusal and ordering; message state machine (edit only in draft/scheduled, send now vs scheduled, cancel, revoke, archive, delete rules); fan-out to distinct active recipients (duplicates collapsed, unknown/foreign ids dropped and reported, deactivated skipped, "everyone" paged through `ListMembers` in batches of 1,000); revoke marks unread entries; scheduler claim/lease/idempotent re-run after a simulated crash; live `inbox` events emitted after commit in services/notification/internal/messages/{messages_test.go,scheduler_test.go}
- [X] T056 [P] [US3] Unit tests for `inbox`: listing hides deleted and unread-revoked rows, unread count, read marks `read_at`, bulk status (read/unread/received) only on the caller's rows, delete from inbox, another person's ids ignored (not_found) in services/notification/internal/inbox/inbox_test.go
- [X] T057 [P] [US3] Unit tests for `httpapi` categories/messages/inbox routes: CRUD, send (200 with `recipient_count` and `dropped_recipients`), cancel/revoke/archive/remove transitions (409 otherwise), recipients listing (manage only), inbox list/unread/read/status/remove bound to the caller, `messages:read` without manage lists own messages only in services/notification/internal/httpapi/{categories_test.go,messages_test.go,inbox_test.go}
- [X] T058 [P] [US3] Integration tests `TestMessages`, `TestInbox`, `TestScheduler` per quickstart §5 (2,500-member tenant fan-out < 60 s, kill `notificationsvc` mid-publish and restart → published exactly once, SC-006) in services/notification/tests/integration/messages_test.go
- [X] T059 [P] [US3] UI unit tests: `stores/{categories,messages,inbox}.ts`; `MessageDrawer` (recipient picker: users via auth search or everyone, category, schedule), `views/messages` status actions, `views/categories`, `views/inbox` (list, mark read/unread bulk, delete, unread badge count) in services/notification/ui/tests/unit/{messages.spec.ts,inbox.spec.ts}
- [X] T060 [P] [US3] Playwright spec `messages-inbox.spec.ts` (create category, send to a second user in another browser context, that user reads and deletes; scheduled message; revoke) with axe in services/notification/ui/tests/e2e/messages-inbox.spec.ts

### Implementation for User Story 3

- [X] T061 [US3] `messages` package: `Categories` service, `Messages` service (state machine, `Send` = publish now or schedule, `Publish` fan-out with `authclient`/`auth.v1.Profiles` `Lookup`/`ListMembers` client, `Revoke`, `Archive`, `Delete`, `Recipients`), `Scheduler` worker (claim loop, lease, idempotent publish, `LastTick` for health), events emitted through a `Publisher` interface (no-op until US4) in services/notification/internal/messages/{categories.go,messages.go,publish.go,scheduler.go}
- [X] T062 [P] [US3] `inbox` package: `List`, `Unread`, `Read`, `SetStatus`, `Delete` scoped to the caller in services/notification/internal/inbox/inbox.go
- [X] T063 [US3] `httpapi` handlers for categories, messages and inbox routes in services/notification/internal/httpapi/{categories.go,messages.go,inbox.go}; scheduler started/stopped by `app` with health tick in services/notification/internal/app/app.go
- [X] T064 [P] [US3] UI stores `categories.ts`, `messages.ts`, `inbox.ts`; views `views/categories/index.vue`, `views/messages/index.vue` (+ `MessageDrawer.vue`, `RecipientPicker.vue`), `views/inbox/index.vue` (+ `InboxList.vue`, `InboxEntryDrawer.vue`) in services/notification/ui/src/
- [X] T065 [US3] Routes `/notification/inbox`, `/notification/messages`, `/notification/categories` in services/notification/ui/src/remote/routes.ts

**Checkpoint**: quickstart §5 passes; scheduled messages publish exactly once across a restart

---

## Phase 6: User Story 4 - Live Updates in the Browser (Priority: P2)

**Goal**: module-served SSE relayed by the gateway, Valkey stream fan-out with replay, module event publishing, header bell with live unread badge (FR-023…FR-025, SC-005, SC-007)

**Independent Test**: quickstart §6 — two tabs for Alice, one for Bob; inbox event reaches Alice only; module publish; reconnect replays the gap; sixth stream refused

### Tests for User Story 4 (MANDATORY) ⚠️

- [X] T066 [P] [US4] Unit tests for `stream` (miniredis or the real Valkey container behind a build tag): `Publish(tenant, to, type, data)` returns a stream id, subscriber loop fans out to per-user hubs, `to` filtering (user list vs `*`), replay from `Last-Event-ID` inside the window in order, id older than the window → `reset`, limits (5 per user → refused, 2,000 per tenant, stalled writer dropped after the 1 KiB buffer fills), heartbeat every 15 s, close at 290 s with `bye`, trimming to the replay window, reserved types refused for module publishes in services/notification/internal/stream/stream_test.go
- [X] T067 [P] [US4] Fuzz test `FuzzSSEFrame` (event type/data encoding never breaks framing: no bare `\n` inside `data:`) in services/notification/tests/fuzz/stream_fuzz_test.go
- [X] T068 [P] [US4] Unit tests for the SSE route and `Events/Publish` gRPC: `inbox:read` required, stream bound to the token's user, `Last-Event-ID` header honoured, `429` on the sixth stream, `stream_opened`/`stream_refused` audited; gRPC publish validates type/payload/user ids (1..1000 or all), cross-tenant ids dropped, `event_published` audited in services/notification/internal/httpapi/stream_test.go and services/notification/internal/grpcapi/events_test.go
- [X] T069 [P] [US4] Integration test `TestStream` per quickstart §6 through the gateway (relay flushes events < 2 s, reconnect with `Last-Event-ID` after Valkey pause/unpause, sign-out ends the stream, `notifyclient.Publish` from a test service identity) in services/notification/tests/integration/stream_test.go
- [X] T070 [P] [US4] UI unit tests: `stores/live.ts` (single shared `EventSource`, `inbox` increments unread and prepends, `inbox.revoked`, `reset` refetches, other types re-dispatched as `freya:live`, closed on unmount), `HeaderBell.vue` (badge count, menu with five newest, link to inbox, renders nothing without `inbox:read`) in services/notification/ui/tests/unit/live.spec.ts
- [X] T071 [P] [US4] Playwright spec extension in `messages-inbox.spec.ts` (second context sees the badge change without reload) and shell `composition.spec.ts` update (bell visible for a member, absent without `inbox:read`) in services/notification/ui/tests/e2e/messages-inbox.spec.ts and services/gateway/shell/tests/e2e/composition.spec.ts

### Implementation for User Story 4

- [X] T072 [US4] `stream` package: Valkey stream publisher (`XADD MAXLEN ~ 10000`), per-tenant subscriber loop (`XREAD BLOCK`) started on first stream and stopped when idle, per-user hubs with bounded buffers, `Replay(lastID)`, `XTRIM MINID` housekeeping, `ServeSSE(w, r, user)` writer (retry, ping, ids, `bye`), limits and metrics (`open_streams`) in services/notification/internal/stream/{stream.go,hub.go,sse.go}
- [X] T073 [US4] SSE route handler (`GET /api/notification/v1/stream`) in services/notification/internal/httpapi/stream.go; `messages` `Publisher` wired to `stream` for `inbox`/`inbox.revoked` events after commit in services/notification/internal/app/wire.go; `grpcapi` `Events/Publish` in services/notification/internal/grpcapi/events.go; `pkg/notifyclient.Publish` in services/notification/pkg/notifyclient/client.go
- [X] T074 [P] [US4] UI `stores/live.ts` and `components/HeaderBell.vue`; `src/remote/header.ts` exposing the bell as `./header`; inbox view subscribes to the store in services/notification/ui/src/

**Checkpoint**: quickstart §6 passes; the bell updates in every open tab within two seconds

---

## Phase 7: User Story 5 - Back Up and Operate (Priority: P3)

**Goal**: backup export/import with skip/overwrite, stats, health, audit trail UI (FR-026…FR-030, SC-008, SC-009)

**Independent Test**: quickstart §7 — export/import round trip, skip = zero changes, overwrite restores, credentials only with the flag; health with Valkey paused

### Tests for User Story 5 (MANDATORY) ⚠️

- [X] T075 [P] [US5] Unit tests for `transfer` (memstore): export omits credential fields by default and includes them with the flag (audited `backup_exported_with_credentials`), import validates the whole document first (size, item counts, schema) and changes nothing on error, skip/overwrite per entity type with report counts, template with unknown channel imported without a channel and reported, `is_default` conflicts resolved (imported default replaces) in services/notification/internal/transfer/backup_test.go
- [X] T076 [P] [US5] Fuzz test `FuzzBackup` (parser never panics, bounds enforced) in services/notification/tests/fuzz/backup_fuzz_test.go
- [X] T077 [P] [US5] Unit tests for `stats` and the ops routes: counts per tenant incl. `open_streams`, health states (db down, valkey down, scheduler stale), audit listing with `subject_name`, backup routes (413 over 16 MiB, 422 on malformed) in services/notification/internal/stats/stats_test.go and services/notification/internal/httpapi/ops_test.go
- [X] T078 [P] [US5] Integration tests `TestBackup`, `TestOps` per quickstart §7 (100-template round trip < 10 s, Valkey paused → health degraded, streams reconnect after unpause) in services/notification/tests/integration/ops_test.go
- [X] T079 [P] [US5] UI unit tests: `stores/ops.ts`, `StatsCard`, `AuditTable` (resolved actor/subject names), backup export/import dialog with the credentials checkbox and report in services/notification/ui/tests/unit/ops.spec.ts
- [X] T080 [P] [US5] Playwright spec `backup.spec.ts` (export, delete a template, import with overwrite, template back) with axe in services/notification/ui/tests/e2e/backup.spec.ts

### Implementation for User Story 5

- [X] T081 [P] [US5] `transfer` package: `Export(tenant, includeCredentials)`, `DecodeBounded`, `Validate`, `Import(mode)` with per-entity transactions and report in services/notification/internal/transfer/backup.go
- [X] T082 [P] [US5] `stats` package (`Counts(tenant)`, `Health`) in services/notification/internal/stats/stats.go
- [X] T083 [US5] `httpapi` handlers for backup export/import (attachment download, 16 MiB), stats, audit, health in services/notification/internal/httpapi/ops.go
- [X] T084 [P] [US5] UI `stores/ops.ts`, `components/{StatsCard.vue,AuditTable.vue,BackupDialog.vue}`, ops section on the log view (stats + audit toggle) and backup actions in the channels view's ⋮ menu in services/notification/ui/src/

**Checkpoint**: quickstart §7 passes

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T085 [P] Documentation: `services/notification/README.md` (module map, run, permissions, limits), `docs/security-model.md` (threat table T1–T11 → mitigations → tests), `docs/operations.md` (KEK rotation, SMTP TLS, rate limits, stream limits, scheduler, retention, Valkey streams, runbooks)
- [X] T086 [P] Security review checklist `specs/006-notification-service/checklists/security-review.md` mapping research.md threats to code and tests; contract test `TestEveryMutationAudited` in services/notification/tests/contract/audit_test.go
- [X] T087 [P] Redaction scan and coverage gates run clean (`make redaction-scan`, `make cover` at 100 % on `internal/{authz,render,sealed,stream,channel/email}`, ≥ 80 % overall), `govulncheck` clean; `.github/workflows/ci.yml` wired (T006) and green
- [X] T088 Performance check: 10,000-member "everyone" fan-out < 60 s, 601st send refused, stream delivery < 2 s under 500 open streams (integration `perf_test.go`, results in `specs/006-notification-service/quickstart-results.md`)
- [X] T089 [P] Warden follow-up note: `services/warden/docs/operations.md` records that share mail can move to `pkg/notifyclient` (out of scope for this feature); root `CHANGELOG.md` entry for the notification module, the auth `ListMembers` RPC and the shell header slot
- [X] T090 Run quickstart.md end to end on the dev stack (gateway + auth + notification with the shell) and record outcomes in `specs/006-notification-service/quickstart-results.md`

---

## Dependencies & Execution Order

- **Phase 1 → Phase 2 → stories**: skeleton and contracts first; every story needs config, schema, sealed settings, audit, authz, the HTTP skeleton with identity middleware, the manifest and the two cross-service changes (T028 auth `ListMembers` is needed by US3 only but is a foundational change to another service; T029 shell slot is needed by US4).
- **US1 (channels, templates, send)** depends on Phase 2 only. **US2 (grants)** depends on Phase 2 and on US1's channels/templates existing as resources (the authz core is foundational, T024; US2 adds the routes and UI).
- **US3 (messages, inbox)** depends on Phase 2 and T028; its live `inbox` events go through a `Publisher` interface that is a no-op until US4 wires the `stream` package (T073).
- **US4 (stream)** depends on Phase 2, T029 and US3 for inbox events (module publishing works without US3).
- **US5 (backup, ops)** depends on US1 (channels/templates), US3 (categories) and US4 (`open_streams` stat).
- Within a story: tests (all `[P]`) → packages → handlers → UI stores → UI views → routes.

## Parallel Execution Examples

- **Phase 1**: T002–T007 in parallel after T001.
- **Phase 2 tests**: T008–T017 in parallel; implementation T021/T022/T023/T026 in parallel after T018–T020; T028 and T029 in parallel with the module work.
- **US1**: T030–T039 in parallel; then T040 ∥ T041; T042 → T043 → T044 while T045 ∥ T046 → T047.
- **US3/US4**: US3's Go work (T061–T063) can run alongside US4's `stream` package (T072) since they meet at the `Publisher` interface.
- **US5**: T081 ∥ T082 → T083 while T084 runs.

## Implementation Strategy

- **MVP = Phase 1 + Phase 2 + US1**: a working, audited email notification module with channels, validated templates, sends and a log, demonstrable in the shell.
- **Increment 2 = US2**: delegation with `use` (needed before other modules send through shared templates).
- **Increment 3 = US3 + US4**: the inbox with live push and the header bell.
- **Increment 4 = US5 + Polish**: backups, operations, docs, gates, quickstart results.
- Every increment leaves the build green: lint, unit, contract, integration for the stories done so far, UI unit and e2e.
