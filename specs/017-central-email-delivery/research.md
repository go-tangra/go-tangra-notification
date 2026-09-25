# Research: Central Email Delivery (017)

Findings come from the current code of go-tangra-notification (master, v4.1.0),
go-tangra-auth (main, v4.1.0) and go-tangra-warden (main, v4.1.0).

## Current state (evidence)

- **notification** already delivers email through tenant channels
  (`internal/channel/email/email.go`): modes `implicit|starttls|none`,
  STARTTLS enforced without fallback (`ErrPlaintext`), certificate verified
  against `host` (`ServerName`), credentials refused with `none`, password
  sealed with the KEK envelope (`internal/sealed`). Its config `smtp:` block
  only bounds tenant channels (`allow_plaintext`, `dial_timeout_seconds`);
  there is no relay setting and no secret-reference mechanism.
- `Notifier.Send` (`internal/grpcapi/notifier.go`, `internal/notify/send.go`)
  needs a template **id**, a tenant-wide `use` grant on template and channel,
  and stores the **rendered subject and body in clear text** in
  `notification_log` (`SetLogOutcome`). Email bodies are rendered with
  `html/template`, so a link appears HTML-escaped in the body.
- Rate limits: `send_per_tenant_per_minute` (600) and per sender (60, keyed
  by SPIFFE id for services), fail closed.
- `pkg/notifyclient` imports only grpc + `api/proto/notification/v1`, but it
  lives in the service module, which requires the auth, lcm and portal sdks
  and heavy deps: importing it from auth would create a module cycle
  (auth → notification → auth sdk is fine, but notification's go.mod also
  pulls everything else) and a heavy dependency.
- **auth** outbox (`internal/email/outbox.go`): payload is a pre-rendered
  `{subject, text}` sealed per row; `RunOnce` always claims with
  `Backoff(1)` (flat 30 s); rows over `maxAttempts` are never retired and are
  logged on every pass (`ClaimOutbox` has no attempts filter). Producers build
  text in Go: invite/resend/LDAP activation (`invite.queue`, 72 h), recovery
  (`password/recovery.go`, 30 min), bootstrap operator invite (`app.go`,
  7 days), admin reset (`app/reset.go`, 7 days). The outbox is built before
  the Freya app exists (`app.go:193` vs `:195`).
- **warden** share mail (`internal/share/mail.go`, `share.go:157-221`): link
  `PublicOrigin + "/warden/share#" + token`, body with secret name, expiry,
  openings and optional sender message; on send failure the share is
  cancelled and `ErrMail` (503) returned — the link is never shown.
- notification's policy already allows `svc/auth` and `svc/warden` to call
  `Notifier/Send`.
- auth's platform tenant: `PlatformTenantID = 00000000-0000-0000-0000-000000000001`.

## Decisions

### D1 — Where the platform relay lives
**Decision**: a new `platform_email:` config block in notification (host,
port, tls, username, password_file, from, reply_to, allow_plaintext). At
start notification upserts one channel in the platform tenant, flagged
`managed`, default email channel of that tenant, enabled, with the password
sealed like any channel secret.
**Rationale**: reuses the existing, hardened email provider (TLS modes,
STARTTLS enforcement, verification, sealed settings) and the existing log.
**Alternatives**: keeping SMTP in each caller (status quo; three places to
configure); a separate mail relay sidecar (new component, no log/templates).

### D2 — Relay password source
**Decision**: `password_file` only (a mounted secret file, read at start,
trimmed of one trailing newline, must be mode ≤ 0640 and non-empty). A
literal `password:` key is rejected at config load. Warden references are
out of scope for this release: resolving them needs a long-lived module
token that modules do not have yet (ticket T069 gap).
**Rationale**: Constitution "Secrets" (secrets provider interface, never in
config files); matches how ticket's relay password is mounted today.

### D3 — System templates and keys
**Decision**: templates gain `system_key` (unique per tenant),
`builtin_subject`, `builtin_body`, `required_variables`, `secret_variables`.
System templates live in the platform tenant with `channel_id NULL` and
`channel_type email`; the channel is resolved per send. Seeding inserts
missing keys only; an edited template is never overwritten. Keys:

| Key | Variables (secret in bold) | Used by |
|---|---|---|
| `auth.invite` | **link**, valid_for, tenant | invite, resend, LDAP activation, first operator |
| `auth.account_reset` | **link**, valid_for | admin reset (`reset-user`) |
| `auth.recovery` | **link**, valid_for | password recovery |
| `auth.message` | subject, **text** | rows queued by auth ≤ 4.1 (legacy payload) |
| `warden.share` | **link**, secret_name, expires, openings, message | share creation |

Required = all non-optional variables (`tenant`, `message` optional).
Deletion of a system template is refused; `POST …/templates/{id}/restore`
resets subject/body to the built-in text.
**Alternatives**: templates in code only (no operator wording change, spec
US4 needs editing); per-tenant copies (N× seeding, drift).

### D4 — Send by key, namespace and grants
**Decision**: `SendRequest` gains `template_key` (mutually exclusive with
`template_id`). For a key send, notification derives the caller's service
name from its SPIFFE id (`…/svc/<name>`) and requires the key to start with
`<name>.`; otherwise `PermissionDenied` + `access_refused` audit (SR-002).
Key sends skip the per-tenant `use` grant checks (FR-011); the mesh policy
still limits callers to `svc/auth` and `svc/warden`.
**Alternatives**: a new RPC `SendSystem` (duplicate plumbing); per-tenant
grants seeded for each tenant (tenants are created by auth, notification
does not know them).

### D5 — Channel resolution for key sends
**Decision**: the tenant's enabled default email channel; else the platform
managed channel; else `FailedPrecondition email_not_configured` (permanent).

### D6 — Redaction by double rendering
**Decision**: render once with the real values (sent) and once with each
secret variable replaced by the marker `[redacted]` (stored in
`rendered_subject` / `rendered_body`). Templates are deterministic, so the
two outputs differ only where secrets were placed, whatever escaping
(`html/template`, URL) applied.
**Rationale**: string replacement of the raw value misses escaped forms
(`&amp;`, `%3D`) — tested with a link containing `&` and `=`.
Error text is already scrubbed of channel secrets; secret variable values are
added to the scrub set for the send's failure reason.

### D7 — Retryable vs permanent outcome
**Decision**: `SendResponse.retryable` (new field). Retryable: dial/greeting/
starttls-handshake network errors, SMTP 4xx, throttled
(`ResourceExhausted`), notification unavailable. Permanent: SMTP 5xx on
recipient/data, `ErrPlaintext` (relay without STARTTLS: configuration),
certificate verification failure, invalid recipient, unknown key,
`email_not_configured`. The email provider wraps `*textproto.Error` codes;
classification lives in `internal/channel/email/classify.go`.

### D8 — System send rate limit
**Decision**: key sends use their own counter `system:<service>` with
`limits_notification.system_send_per_minute` (default 300); throttling is
retryable, so auth's queue drains bulk activations (500 invites ≈ 2 min).

### D9 — notification sdk module
**Decision**: nested module `github.com/go-tangra/go-tangra-notification/sdk/v4`
holding `api/proto/notification/v1` and `pkg/notifyclient` (moved), required
by the service with `replace => ./sdk`; tagged `sdk/v4.2.0`. Same pattern as
lcm/auth/portal sdks. No dependency beyond grpc/protobuf.

### D10 — auth outbox changes
**Decision**:
- Payload v2 `{v:2, template, vars}`; legacy `{subject,text}` rows are sent
  with `auth.message`. Decoding detects the version.
- Sender interface becomes `Deliverer.Deliver(ctx, item) (Outcome, error)`
  with outcomes `sent|retry|failed`. The notification deliverer obtains its
  gRPC connection lazily through `Freya.Client(ctx, "notification")` on first
  use (the outbox is built before the Freya app).
- Migration: `outbox.failed_at`, `outbox.last_error` (scrubbed, ≤ 200 chars);
  `ClaimOutbox` filters `failed_at IS NULL` and sets
  `next_attempt_at = now() + least(30s·2^attempts, 1h)`; permanent failure or
  attempts > max sets `failed_at` and emits one `email_given_up` audit event
  + one warning.
- Config: `email.transport` accepts `notification` (default) and `log`
  (development only, refused in production as today). `smtp` and the relay
  keys (`host`, `port`, `username`, `password`, `from`, `allow_plaintext`)
  are still accepted by the strict decoder, ignored, and listed in one
  start-up warning (FR-017).
- The `log` sink keeps printing the link (development only, unchanged).

### D11 — warden
**Decision**: `share.Sender` gets a notification implementation sending
`warden.share` with the link as a secret variable; failure keeps today's
behaviour (share cancelled, `ErrMail` 503). `mail.transport`: `notification`
(default) | `log` (dev); relay keys accepted, ignored, warned.

### D12 — Deployment
**Decision**: go-tangra-docker: `configs/notification.yaml` gets
`platform_email` (dev: Mailpit, `tls: none`, `allow_plaintext: true`);
auth/warden dev configs switch to `transport: notification`. `prod-init.sh`
writes `platform_email` from `SMTP_*`, the password into
`prod/secrets/smtp.password` mounted into notification, and no longer edits
auth/warden relay keys. `PRODUCTION.md` §4 describes the single setting and
the certificate host-name rule (the relay name must match its certificate,
e.g. `mx01.kumo.verax.net`, not the IP). ticket unchanged.

## Threat model (STRIDE)

| Threat | Vector | Mitigation |
|---|---|---|
| Spoofing | A module other than auth/warden sends a system template | Mesh mTLS identity; policy allows only svc/auth, svc/warden on `Send`; key namespace check (D4); refusal audited |
| Spoofing | auth sends `warden.share` to phish with warden's wording | Namespace check: auth may send only `auth.*` |
| Tampering | Operator edits a system template to drop the link or add HTML/JS | Required-variable check at save (FR-008); body rendered with `html/template` (auto-escaping); allowed function set unchanged |
| Repudiation | Who sent which system mail | Log entry + `notification_sent/failed` audit with service actor, template key, correlation id |
| Information disclosure | Link/token in delivery log, audit, logs, errors | Double rendering (D6); secret values added to error scrub; audit carries no variables; tests scan every stored/logged artefact (SC-003) |
| Information disclosure | Relay password in config/logs/API | `password_file` only (D2); sealed; channel reads return `__set__`; managed channel not editable |
| Information disclosure | Credentials over plaintext | Username with `tls: none` refused; STARTTLS without fallback; certificate verified against host |
| Denial of service | Bulk activation floods relay | System send rate limit per service (D8), retryable; auth backoff up to 1 h |
| Denial of service | Poison message re-claimed forever | `failed_at` retirement, single report (D10) |
| Elevation of privilege | Key send bypasses tenant grants | Only for system keys of the caller's own namespace; ordinary template sends keep grant checks |

## Dependencies

No new third-party dependencies. auth and warden add
`github.com/go-tangra/go-tangra-notification/sdk/v4` (grpc + protobuf only,
already in their graphs).
