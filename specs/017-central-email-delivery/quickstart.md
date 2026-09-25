# Quickstart: validating Central Email Delivery (017)

Prerequisites: go-tangra-docker v4 branch with the 017 changes, images
notification ≥ 4.2.0, auth ≥ 4.2.0, warden ≥ 4.2.0; development stack
(Mailpit on http://localhost:8025).

## Scenario 1 — one relay setting (US1)

1. `configs/notification.yaml` has `platform_email` (Mailpit); auth and
   warden configs have no relay keys (`transport: notification`).
2. `docker compose up -d`; `docker compose logs notification | grep -i "platform email"`
   → "platform email channel ready" (and the `allow_plaintext` warning in dev).
3. Console → Notification → Channels: "Platform email" shows **Managed**,
   edit/delete disabled; **Test** to your address → message in Mailpit.
4. Change `platform_email.from`, `docker compose restart notification`, test
   again → new sender; auth/warden untouched.
5. Negative: set `tls: none` without `allow_plaintext` → notification refuses
   to start naming `platform_email.allow_plaintext`.

## Scenario 2 — invitations (US2)

1. Invite `alice@example.org` in the console → Mailpit has the invitation
   with a working link.
2. Notification → Log → open the entry: template `auth.invite`, body shows
   `[redacted]` where the link was; no token anywhere in the entry.
3. `docker compose stop notification`; invite `bob@example.org`; wait 1 min;
   `docker compose start notification` → Bob's mail arrives within ~1 min
   of notification being ready (auth retried).
4. auth DB: `SELECT attempts, sent_at, failed_at FROM outbox ORDER BY created_at DESC LIMIT 2;`
   → both sent.
5. Poison message: invite `nobody@invalid.` (relay 5xx) → one `email_given_up`
   warning/audit, then silence (no repeated log lines).

## Scenario 3 — share links (US3)

1. warden → share a secret with `carol@example.org` → Mailpit has the share
   mail; log entry `warden.share`, link redacted.
2. Stop notification, share again → UI reports the mail could not be sent;
   the share is listed as cancelled.

## Scenario 4 — template wording (US4)

1. Notification → Templates → `auth.invite` (badge **System**): change the
   subject to "Welcome to Tangra", save → next invite uses it.
2. Remove `{{.link}}` from the body → save refused (`missing_required_variable link`).
3. Delete → refused; **Restore built-in** → original wording back.
4. `docker compose restart notification` → the edited subject survives.

## Automated checks

- notification: `make test` (unit + contract), `make test-integration`
  (Postgres/Valkey testcontainers + in-process SMTP server), coverage gate.
- auth: `make test`, `make test-integration` (fake Notifier gRPC server
  replaces Mailpit in the harness), coverage gate (`internal/password` 100 %).
- warden: `go test ./...`.
- Redaction scan: integration tests assert that no stored log row, audit row
  or captured log line contains the link token for every system template.
