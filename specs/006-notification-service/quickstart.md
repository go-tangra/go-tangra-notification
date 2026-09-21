# Quickstart — Notification Service

Validates the feature end-to-end on the platform stack (auth in gateway mode +
gateway with the shell + notification), with Mailpit as the mail relay.
Contracts are in [contracts/](contracts/), entities in
[data-model.md](data-model.md), decisions in [research.md](research.md).

## Prerequisites

Go 1.26, Node 22, Docker with compose; test CA from `make testca` with
`-services auth,gateway,hello,warden,notification`; the auth and shell changes
in `contracts/auth-changes.md` and `contracts/shell-changes.md` applied.

## 1. Stack and gates

```bash
make -C services/notification compose-up   # TimescaleDB (notification db, :5434), Valkey (:6381), Mailpit (:8027 UI / :1027 SMTP); compose project "notification"
make -C services/notification lint vuln cover fuzz   # vet, staticcheck, gosec, govulncheck, coverage gate (100 % on internal/{authz,render,sealed,stream,channel/email}), fuzz corpus
(cd services/notification/ui && npm ci && npm run lint && npm run test:unit && npm run build)
go test ./services/notification/tests/contract/...   # OpenAPI ↔ routes, manifest, proto, policy refusal
```

## 2. Start and register

```bash
head -c 32 /dev/urandom | base64 > services/notification/deploy/kek.dev          # dev KEK (never committed)
(cd services/notification && go run ./cmd/notificationsvc bootstrap -config deploy/dev.yaml)   # migrations, KEK check
(cd services/gateway && go run ./cmd/gatewaysvc bootstrap -config deploy/dev.yaml -allow "spiffe://example.org/svc/notification=/api/notification,/ui;notification")
(cd services/notification && go run -tags ui ./cmd/notificationsvc -config deploy/dev.yaml)
```

Expected: gateway log `registration_accepted module=notification`; the shell
shows a **Notifications** menu (Inbox, Channels, Templates, Log, Messages,
Categories, Permissions) and a bell in the header for every signed-in
member; `GET /api/notification/v1/health` reports `database: ok, valkey: ok,
scheduler_last_tick` within the last minute.

## 3. Channels, templates, send (US1)

```bash
go test -tags integration ./services/notification/tests/integration -run 'TestChannels|TestTemplates|TestSend' -v
```

Covers: email channel against Mailpit (STARTTLS off via `allow_plaintext` in
dev), test send lands in Mailpit with the test subject, default-per-type flag
moves, delete refused while templates reference the channel; template
validation (syntax position, undeclared variable), preview without a log
entry; send renders and delivers, log entry `sent` with rendered subject/body,
override channel honoured, refusals for a missing variable / disabled channel
/ type mismatch / bad recipient; Mailpit stopped → `failed` with a scrubbed
reason; the relay password (`NOTIF-MARKER-PW-…`) never appears in listings,
log, audit, exports or errors (SC-001, SC-002, SC-003).

Manual, in the shell: **Channels → New** (Mailpit host/port), **Send test**,
**Templates → New** with `{{.Name}}`, **Preview**, **Send** from the template
drawer; open **Log** and the entry.

## 4. Access (US2)

```bash
go test -tags integration ./services/notification/tests/integration -run 'TestAccess' -v
```

Covers: owner/editor/viewer/sharer matrix on a template and a channel
including `use`; role grants through groups; tenant-wide `use` on a default
channel; expiry; granter cannot exceed own relation; cross-tenant 404;
effective permissions with sources; every grant and refusal audited (SC-004,
SC-009).

## 5. Internal messages and inbox (US3)

```bash
go test -tags integration ./services/notification/tests/integration -run 'TestMessages|TestInbox|TestScheduler' -v
```

Covers: categories (ordering, delete refused in use); message to two users
(inbox entries, unread counts), to everyone (auth `ListMembers` paging over
2,500 synthetic members in < 60 s), duplicate recipients collapsed,
deactivated users skipped; read/mark/delete; revoke hides unread entries and
keeps read ones; scheduled message published by the worker within one
interval after its time; the service is killed mid-publish and restarted →
published exactly once (SC-005, SC-006).

## 6. Live stream (US4)

```bash
go test -tags integration ./services/notification/tests/integration -run 'TestStream' -v
```

Covers: two streams for Alice and one for Bob through the gateway; inbox
event reaches Alice's streams only within 2 s; module `Publish` to one and to
many users; reconnect with `Last-Event-ID` replays the events published
during the gap; an id older than the window yields `reset`; sixth stream
refused; stalled reader dropped; sign-out ends the stream (SC-005, SC-007).

Manual: two browser tabs; send a message from the Messages view; the bell
badge changes in both tabs without reload.

## 7. Backup and operations (US5)

```bash
go test -tags integration ./services/notification/tests/integration -run 'TestBackup|TestOps' -v
```

Covers: export (credentials omitted by default; included with the flag and
audited as bulk disclosure), import skip / overwrite report, unknown channel
name reported, malformed and oversized files refused; stats and audit
listing; health with Valkey paused (SC-008).

## 8. UI through the gateway

```bash
(cd services/notification/ui && PW_CHANNEL=chrome E2E_MAIL=http://localhost:8027 E2E_OPERATOR_EMAIL=… E2E_OPERATOR_PASSWORD=… npx playwright test)
```

Specs: `channels`, `templates`, `send-log`, `permissions`, `messages-inbox`
(two contexts, live badge), `backup`; each with an axe check (no critical
violations).

## 9. Redaction scan and coverage

```bash
make -C services/notification redaction-scan   # markers NOTIF-MARKER-PW-, NOTIF-MARKER-BODY- absent from logs, audit, exports-without-credentials
make -C services/notification cover            # gate: 100 % on the security packages, >= 80 % overall
```

Results are recorded in `quickstart-results.md` by `/speckit-implement`.
