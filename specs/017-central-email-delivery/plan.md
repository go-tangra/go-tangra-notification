# Implementation Plan: Central Email Delivery Through the Notification Module

**Branch**: `017-central-email-delivery` | **Date**: 2026-09-25 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/017-central-email-delivery/spec.md`

## Summary

notification becomes the single outbound-email path of the platform. At start
it upserts a configuration-managed platform email channel from a new
`platform_email` block (password from a mounted secret file) and seeds five
system templates keyed `auth.invite`, `auth.account_reset`, `auth.recovery`,
`auth.message`, `warden.share`. `Notifier.Send` accepts a `template_key`,
restricted to the caller's own namespace, resolves the tenant's default email
channel or the platform channel, stores the log with secret variables
redacted by double rendering, and tells callers whether a failure is
retryable. The proto and client move into a nested `sdk` module. auth's
outbox sends through it (structured payload, lazy mesh client, exponential
backoff, retirement of dead messages); warden's share mail too. Relay
settings in auth/warden are accepted-and-ignored with a warning; the docker
deployment configures the relay only in notification.

## Technical Context

**Language/Version**: Go 1.26 (services), TypeScript/Vue 3 (notification UI)

**Primary Dependencies**: go-tangra/v4 framework (Freya mesh, authz policy,
config), grpc/protobuf, pgx/goose, stdlib `net/smtp`, `html/template`,
`text/template`; no new third-party dependencies

**Storage**: PostgreSQL/TimescaleDB (notification: channels, templates,
notification_log; auth: outbox), Valkey (rate-limit counters)

**Testing**: `go test` (unit, contract, negative security, fuzz for the key
parser and payload decoder), testcontainers integration suites, vitest (UI)

**Target Platform**: Linux containers (docker compose), linux/amd64

**Project Type**: multi-repo web services (notification primary; auth,
warden; go-tangra-docker deployment)

**Performance Goals**: system sends ≥ 300/min per calling service; an
invitation is delivered ≤ 1 min after notification is available

**Constraints**: backwards-compatible proto (additive); strict YAML decoding
in all services (deprecated keys must stay declared); auth coverage gate
(80 % total, 100 % `internal/password`); notification security packages
(`internal/sealed`, `internal/render`, redaction) 100 %

**Scale/Scope**: 5 system templates, 1 platform channel, 3 services, 1
deployment repo; ~4 releases (notification sdk/v4.2.0 + v4.2.0, auth v4.2.0,
warden v4.2.0)

## Constitution Check

*GATE: re-checked after Phase 1 design — all PASS.*

- [x] **I. Secure by Default**: TLS (`starttls`) is the default; `tls: none`
      requires `platform_email.allow_plaintext` (named opt-out, warned at
      every start); username with `none` refused; STARTTLS never falls back.
- [x] **II. Zero Trust**: key sends arrive over mTLS; policy allows only
      svc/auth and svc/warden; handler enforces the key namespace from the
      verified SPIFFE id; no unauthenticated path.
- [x] **III. Boundary Validation**: key regex, exactly-one template ref,
      variable caps (existing 50 / 64 KiB), required variables, config
      schema with refusals; system send rate limit.
- [x] **IV. Test-First**: tasks list tests first per story; negative tests
      (foreign namespace, missing variable, plaintext without opt-out,
      literal password, managed channel edit); fuzz: template key parser and
      auth payload decoder; redaction scan over every system template.
- [x] **V. Observability**: sends audited with template key and correlation
      id; `access_refused` for namespace violations; `email_given_up` once
      per dead message in auth; secret variables redacted from log rows,
      errors and logs.
- [x] **VI. Supply Chain**: no new dependencies; new nested sdk module holds
      only generated code + client (grpc/protobuf).
- [x] **VII. Simplicity**: typed config blocks; seeding is idempotent code,
      no reflection; the double-render redaction avoids escaping-aware
      string matching.
- [x] **Threat Model**: STRIDE table in research.md.

## Project Structure

### Documentation (this feature)

```text
specs/017-central-email-delivery/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── notification-grpc.md
│   ├── notification-http.md
│   └── config.md
├── checklists/requirements.md
└── tasks.md
```

### Source Code

```text
go-tangra-notification/
├── sdk/                                   # NEW nested module .../notification/sdk/v4
│   ├── go.mod
│   ├── api/proto/notification/v1/         # moved; SendRequest.template_key, SendResponse.retryable
│   └── pkg/notifyclient/                  # moved; SendKey + Result
├── internal/config/config.go              # platform_email, platform_tenant_id, system_send_per_minute
├── internal/store/migrations/0005_system_email.sql
├── internal/store/…                       # channel.managed, template system columns, log.template_key
├── internal/notify/
│   ├── platform.go                        # NEW EnsurePlatformChannel
│   ├── systemtemplates.go                 # NEW built-in wording + EnsureSystemTemplates
│   ├── send.go                            # key sends, channel resolution, double render, retryable
│   ├── channels.go / templates.go         # managed/system guards, restore
├── internal/channel/email/classify.go     # NEW retryable classification
├── internal/render/redact.go              # NEW RenderRedacted
├── internal/grpcapi/notifier.go           # template_key, namespace check
├── internal/httpapi/…                     # managed/system fields, restore route
├── internal/app/wire.go                   # run seeding at start
├── api/openapi/notification.yaml
└── ui/src/views/{channels,templates}/     # Managed / System badges, restore button

go-tangra-auth/
├── internal/email/{outbox.go,deliver.go,notify.go}   # payload v2, Deliverer, notification deliverer
├── internal/store/migrations/0009_outbox_retire.sql
├── internal/store/repos.go                # ClaimOutbox backoff + filter, MarkOutboxFailed
├── internal/invite/invite.go, internal/password/recovery.go,
│   internal/app/{app.go,reset.go}         # producers enqueue template key + vars
├── internal/config/config.go              # transport notification|log, deprecated keys warning
└── tests/integration/harness_test.go      # fake Notifier server instead of Mailpit

go-tangra-warden/
├── internal/share/{mail.go,share.go}      # Message{Template,Vars}, notification sender
├── internal/app/{app.go,wire.go}          # sender from Freya.Client("notification")
└── internal/config/config.go              # transport notification|log, deprecated keys warning

go-tangra-docker/
├── configs/{notification,auth,warden}.yaml
├── docker-compose.production.yaml.example # smtp.password secret mounted into notification
├── scripts/prod-init.sh                   # platform_email from SMTP_*; no auth/warden relay edits
├── policies/notification.yaml             # copy from notification 4.2.0
└── PRODUCTION.md
```

**Structure Decision**: primary work in go-tangra-notification (this repo);
dependent changes in auth and warden land after `sdk/v4.2.0` is tagged;
docker changes land last and pin the new images.

## Delivery order

1. notification: sdk module + proto fields (tag `sdk/v4.2.0` once merged).
2. notification: config, migration, seeding, key sends, redaction, UI
   (release v4.2.0).
3. auth and warden in parallel against sdk/v4.2.0 (release v4.2.0 each).
4. go-tangra-docker: configs, prod-init, overlay, policies, PRODUCTION.md.
5. Server rollout: notification first (auth keeps queueing and retries),
   then auth and warden.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Fifth system template `auth.message` for legacy payloads | Messages queued by auth ≤ 4.1 contain pre-rendered text only | Dropping them loses invitations queued during the upgrade |
| Accept-and-ignore deprecated relay keys | Strict YAML decoding would make every existing deployment fail to start | Hard removal breaks upgrades within v4 |
