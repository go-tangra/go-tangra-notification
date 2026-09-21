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

## Limits and rates

Configured under `limits_notification` (send rates, stream counts, replay
window, backup size). Rate limits fail **closed**: if Valkey is unavailable a
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
