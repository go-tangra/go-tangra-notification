---
title: Operate notifications
summary: Configuration keys and defaults, the platform email relay, the key-encryption key, service policies, the scheduler task, and fixes for common start-up and delivery problems.
kind: reference
order: 90
tags: [notification, operations, configuration, smtp, policy, troubleshooting]
audience: operator
---
The module runs as `notificationsvc` (image
`ghcr.io/go-tangra/go-tangra-notification`, uid 10001).

```sh
notificationsvc -config /app/config.yaml           # serve; applies migrations (-no-migrate to skip)
notificationsvc bootstrap -config /app/config.yaml # migrate, check the KEK and dependencies, print a report, exit
notificationsvc bootstrap -config /app/config.yaml -rotate-kek /path/new.kek
notificationsvc version
```

It needs PostgreSQL/TimescaleDB, Valkey, a 32-byte key-encryption key (KEK),
and on the mesh auth and the gateway. SMTP relays are reached directly.

## Configuration

YAML; unknown keys are refused. Besides the framework keys (`service_name`,
`trust_domain`, `env`, `identity`, `authz`, `server`, `admin`, `discovery`,
`limits`):

| Key | Default | Notes |
|---|---|---|
| `db.dsn` | — | required; role without `BYPASSRLS`; production needs `sslmode=verify-full` or `verify-ca` |
| `db.migrate_dsn` | `db.dsn` | migration role |
| `db.max_conns` | 16 | |
| `valkey.addresses` | — | required; rate limits and the live event stream |
| `valkey.username`, `valkey.password`, `valkey.ca_file` | — | TLS |
| `valkey.allow_plaintext` | false | refused in production |
| `kek.source` | `file` | `file` (with `kek.path`) or `env` (with `kek.env`): 32 bytes, raw or base64 |
| `smtp.allow_plaintext` | false | lets tenant channels use `tls: none`; refused in production |
| `smtp.dial_timeout_seconds` | 30 | 1–120 |
| `scheduler.interval_seconds` | 15 | 1–60, how often scheduled messages are checked |
| `scheduler.lease_seconds` | 60 | interval–600 |
| `task_scheduler.enabled`, `task_scheduler.service` | false, `scheduler` | register the `notification:send-test-email` task with the scheduler module |
| `gateway.service` | `gateway` | mesh name of the gateway; the only peer allowed to fetch the help pages |
| `gateway.issuer` | — | required `https://` origin of the platform token issuer |
| `platform_email.*` | — | the platform relay, see below |
| `platform_tenant_id` | `00000000-0000-0000-0000-000000000001` | tenant that holds the platform channel and the system templates |
| `limits_notification.send_per_tenant_per_minute` | 600 | |
| `limits_notification.send_per_sender_per_minute` | 60 | |
| `limits_notification.system_send_per_minute` | 300 | 1–10000, per calling module |
| `limits_notification.streams_per_user` | 5 | live connections per person |
| `limits_notification.streams_per_tenant` | 2000 | at least `streams_per_user` |
| `limits_notification.replay_window_seconds` | 300 | 60–3600, how far a reconnecting client catches up |
| `limits_notification.backup_max_bytes` | 16 MiB | 4–64 MiB; `limits.max_request_bytes` must be at least this |
| `enroll.*` | — | `enabled`, `enroll_url`, `lcm_grpc`, `tenant_id`, `token_file`, `state_file`, `insecure`: SVID enrollment with lcm |

## Platform email

The relay for all platform mail (auth invitations and recovery, warden
share links, lcm, signing and hr mail) is configured once:

```yaml
platform_email:
  host: mx01.example.net        # the name on the relay's certificate, not an IP
  port: 587
  tls: starttls                 # implicit | starttls (default) | none
  username: tangra@example.net  # optional; needs password_file; refused with tls none
  password_file: /run/secrets/smtp.password   # mode 0640 or stricter
  from: tangra@example.net
  reply_to: ""
  allow_plaintext: false        # must be true for tls: none
```

A literal `password:` is refused. At start the module seeds the system
templates and creates or updates the **Platform email** channel in the
platform tenant (log: `platform email channel ready`). Without the block it
logs `platform email disabled` and disables an existing platform channel.
To change the relay, edit the block and restart this module.

For a tenant's system mail the module uses the tenant's enabled default
email channel, otherwise the platform channel.

## Key-encryption key

The KEK seals every channel's credentials. Losing it makes them
unrecoverable: back it up out of band. To rotate, run `bootstrap
-rotate-kek <new key file>` with the **old** key still in the
configuration; it re-seals every channel and reports the count. Then switch
the configuration to the new key and restart.

## Service policies

The module's own policy:

```yaml
rules:
  - id: gateway-forwards
    from: ["spiffe://<trust domain>/svc/gateway"]
    to: ["notification"]
    operations: ["*"]
    effect: allow
  - id: modules-send
    from: ["spiffe://<trust domain>/svc/warden", "spiffe://<trust domain>/svc/auth", "spiffe://<trust domain>/svc/lcm", "spiffe://<trust domain>/svc/signing", "spiffe://<trust domain>/svc/hr"]
    to: ["notification"]
    operations: ["/notification.v1.Notifier/Send", "/notification.v1.Notifier/SendTest", "/notification.v1.Events/Publish"]
    effect: allow
  - id: scheduler-execute
    from: ["spiffe://<trust domain>/svc/scheduler"]
    to: ["notification"]
    operations: ["/scheduler.v1.TaskExecutor/ExecuteTask"]
    effect: allow
```

The gateway rule also covers the help pages (`GET /docs/v1/bundle`). A
module may send only system templates of its own namespace (`warden.*` for
warden); other keys are refused and audited as `access_refused` with reason
`key_namespace`.

## Scheduler task

With `task_scheduler: {enabled: true, service: scheduler}` and
`discovery.static.scheduler`, the module registers the task
`notification:send-test-email` with the scheduler module and re-registers
every five minutes. The task sends a plain-text test mail through the
tenant's default (or a chosen) email channel, never the platform channel.

## Troubleshooting

**The module refuses to start with `config: ...`.** The message names the
key. Common: `platform_email.password is not accepted: use password_file`,
`platform_email.password_file must have mode 0640 or stricter`,
`kek.path is required for kek.source file`,
`limits.max_request_bytes must be at least limits_notification.backup_max_bytes`.

**The module refuses to start with `help docs: ...`.** The help pages built
into the binary are invalid. CI catches this with `tangra-docs lint`;
rebuild from a commit that passed CI.

**Platform mail fails with `relay offers no STARTTLS`.** The relay does not
support STARTTLS on that port. Use `tls: implicit` on 465, or fix the relay;
the module never falls back to plaintext.

**Platform mail fails with a certificate error.** `platform_email.host` must
be the name on the relay's certificate, not an IP address.

**Modules report `email_not_configured`.** The tenant has no enabled default
email channel and `platform_email` is not configured (the log says
`platform email disabled`).

**Sends are refused with `rate_limited` while Valkey is down.** Rate limits
fail closed. Health (`GET /api/notification/v1/health`, `stats:read`)
reports Valkey as `unreachable` and the status as `degraded` until it is
back.

**Scheduled messages are not published.** The health report shows the last
scheduler tick; check the database and the log.

**Roles are missing in auth.** Look for `auth registration: ...` in the log.
The module retries every 5 seconds until auth accepts, then registers every
five minutes.
