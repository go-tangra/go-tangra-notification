# Operations

## Deploy

The service needs TimescaleDB (with the TimescaleDB extension), Valkey (TLS in
production), an SMTP relay reachable from the network, the auth and gateway
services on the Freya channel, and a 32-byte key-encryption key.

- **KEK**: `kek.source` is `file` (a 32-byte key, raw or base64) or `env`. Losing
  it makes every sealed channel setting unrecoverable; back it up out of band and
  rotate with `notificationsvc rotate-kek` (re-seals every channel under a new key
  in one transaction).
- **Database roles**: migrations run as a role that owns the schema; the service
  runs as `notification_app` (no `BYPASSRLS`). The migration DSN is separate.
- **Policy**: `deploy/policy.yaml` admits the gateway on every route and named
  services (warden, auth) on `Notifier/Send`, `Notifier/SendTest` and
  `Events/Publish`. Tighten `from:` per deployment.
- **Gateway allow-list**: `gatewaysvc bootstrap -allow
  "spiffe://<td>/svc/notification=/api/notification,/ui;notification"`.

## Platform email (central delivery)

The platform's outbound mail (auth invitations and recovery, warden share
links) goes through this module. The relay is configured once, here:

```yaml
platform_email:
  host: mx01.example.net        # the name on the relay's certificate, not its IP
  port: 587
  tls: starttls                 # implicit (465) | starttls (default) | none
  username: tangra@example.net  # optional; refused with tls: none
  password_file: /run/secrets/smtp.password   # mode 0640 or stricter
  from: tangra@example.net
  reply_to: ""
  allow_plaintext: false        # must be true for tls: none (development)
platform_tenant_id: 00000000-0000-0000-0000-000000000001
limits_notification:
  system_send_per_minute: 300   # per calling service
```

- **Password**: only through `password_file` (a mounted secret file, one
  trailing newline ignored). A literal `password:` is refused at start. The
  password is sealed like every channel credential and never returned,
  logged or audited.
- **TLS**: `starttls` never falls back to plaintext (a relay without STARTTLS
  fails with `relay offers no STARTTLS`); the relay certificate is verified
  against `host`, so a bare IP fails with a certificate error naming the
  mismatch. `tls: none` needs `allow_plaintext: true` and is warned at every
  start; a username with `tls: none` is refused.
- **Start-up**: after the migrations the module seeds the system templates
  and creates or updates the managed **Platform email** channel in the
  platform tenant ("platform email channel ready" in the log). Without the
  block it logs "platform email disabled" and disables an existing managed
  channel; sends by key then fail with `email_not_configured` unless the
  tenant has its own default email channel. Changing the relay = edit the
  block and restart this module only.
- **Managed channel**: shown with a *Managed* badge; update and removal answer
  `409 managed_channel`; *Send test* works. It is never part of a backup.
- **Channel choice for system mail**: the tenant's enabled default email
  channel, otherwise the platform channel.
- **System templates** (`auth.invite`, `auth.account_reset`, `auth.recovery`,
  `auth.message`, `warden.share`) live in the platform tenant. Operators edit
  subject and body in *Templates* (badge *System*); required variables (the
  link) must stay; *Restore built-in* returns to the original. Upgrades
  refresh only the built-in copy, never the edited wording.
- **Email body editor**: bodies of email templates open in a visual
  (WYSIWYG) editor with an *HTML source* toggle; sms, slack and sse keep the
  plain text area. Template actions show as chips and are stored exactly as
  written (also inside link addresses, e.g. `href="{{.link}}"`); *Insert
  variable* adds `{{.name}}` for the declared (system: required) variables.
  A body the editor cannot keep exactly (inline styles, other elements such
  as `div`/`img`/tables, an action between blocks like the built-in
  `warden.share` `{{if .message}}`, an action inside a tag, an unterminated
  action) opens in source mode with a warning and is not rewritten; switching
  such a body to the visual editor asks first. The server still validates and
  renders every body with `html/template`.
- **Verifying delivery**: *Log* shows every system send with its
  `template_key`, status and `sent_at`; links appear as `[redacted]`. A
  failed entry carries the relay's reason (scrubbed); callers retry the
  retryable ones (auth keeps its queue).
- **Policy**: only `svc/auth` and `svc/warden` may call `Notifier/Send`; each
  may send only keys of its own namespace (`auth.*`, `warden.*`), refusals are
  audited as `access_refused` with reason `key_namespace`.

## Limits and rates

Configured under `limits_notification` (send rates, system template sends per
calling service, stream counts, replay window, backup size). Rate limits fail **closed**: if Valkey is unavailable a
send is refused rather than allowed to bypass the limit. Health reports Valkey
as `unreachable` and the status as `degraded` while it is down; the gateway
lease lapses and the module re-registers on recovery.

## Live stream

One Valkey stream per tenant (`notif:events:<tenant>`, `XADD MAXLEN ~ 10000`);
each instance runs one subscriber loop per tenant that fans out to the SSE
connections it holds. Reconnecting clients replay from `Last-Event-ID` inside
the 5-minute window, or receive a `reset` beyond it. The gateway relays the
stream with immediate flushing; the module closes each stream at 290 s so the
client reconnects before the gateway's 300 s route timeout.

## Scheduler

A single SQL lease claim (`ClaimDueMessages ... FOR UPDATE SKIP LOCKED`) hands
each due message to exactly one instance; a crash mid-publish leaves the lease
to expire and the idempotent fan-out (inbox rows keyed per recipient) completes
on the next claim, so a message is published exactly once.

## Backups

`POST /backup/export` omits credentials by default; `include_credentials`
returns the sealed values decrypted and is audited as a bulk disclosure. Import
matches by name (channels by name and type), runs each entity type in its own
transaction, and reports created/skipped/overwritten/failed with warnings.

## Observability

Every request carries a correlation id; every mutation emits one audit event
(closed vocabulary, no credentials or content). `GET /stats` reports per-tenant
counts including open streams; `GET /audit` lists events with resolved
actor/subject names; `GET /health` reports the database, Valkey and the last
scheduler tick.
