# Quickstart results — notification service

Recorded on 2026-09-17 against the development stack (TimescaleDB, Valkey,
Mailpit, plus the gateway and auth services built from the sibling modules)
via the tagged integration suite, which drives every quickstart scenario end
to end through the gateway exactly as a browser would.

| Quickstart section | Scenario | Test(s) | Result |
|--------------------|----------|---------|--------|
| 1 | Stack and gates | `make cover` (total 91.2%, security packages 100%), `make fuzz`, `make redaction-scan` (0 matches) | PASS |
| 2 | Start and register | `TestHarnessBoots` (operator 200, anonymous 401, member 403 on stats / 200 on inbox, health ok) | PASS |
| 3 | Channels, templates, send (US1) | `TestChannels`, `TestTemplates`, `TestSend` (Mailpit delivery, default move, delete-in-use, preview without a log entry, scrubbed failure, no credential leak) | PASS |
| 4 | Access (US2) | `TestAccess` (owner/editor/viewer/sharer matrix incl. `use`, role-through-group, tenant use, expiry, granter ceiling, cross-tenant 404, audit) | PASS |
| 5 | Internal messages & inbox (US3) | `TestMessages`, `TestInbox`, `TestScheduler` (everyone via `ListMembers`, duplicates collapsed, deactivated skipped, revoke, exactly-once after a crash) | PASS |
| 6 | Live stream (US4) | `TestStream`, `TestNotifierRPC` (targeted delivery < 2 s, module publish, `Last-Event-ID` replay, reset, per-person limit, sign-out) | PASS |
| 7 | Backup & operations (US5) | `TestBackup`, `TestOps` (credential-free by default, with-credentials audited, skip/overwrite, stats, audit names, Valkey-down health) | PASS |
| 8 | UI through the gateway | `ui`: `vue-tsc` + `eslint` clean, 18 Vitest unit tests pass, `npm run build` produces the remote, Playwright specs parse (channels, templates, permissions, messages-inbox, backup — run against a live stack with `E2E_OPERATOR_*`) | PASS (e2e specs authored; run in CI/manually with credentials) |
| 9 | Redaction & coverage | `make redaction-scan`, `make cover` | PASS |

Notes:

- `TestPerformance` (10,000-member fan-out < 60 s, 601st send refused, delivery
  to 500 open streams < 2 s) is authored and gated behind `NOTIFICATION_PERF=1`
  so the default suite stays fast; it exercises the real store, publisher and
  gateway.
- A cross-service cache-collision bug was found and fixed: the gateway and auth
  decision caches shared the `dec:` Valkey namespace with different value
  encodings, which corrupted the module's `messages:manage`/`stats:read`
  listing-widening checks. The gateway now uses a `gwdec:` namespace.
