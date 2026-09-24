# Security review checklist — notification service

Maps the research.md threats and the success criteria to the code and tests
that enforce them. Every box is verified by the referenced test.

## Credentials at rest and in transit (T1, SC-001, SC-002)

- [X] Channel settings sealed with AES-256-GCM (DEK wrapped by the KEK), associated data binds the channel id — `internal/sealed` (100%)
- [X] Responses carry only public fields and `"__set__"`; an update with the marker keeps the stored value — `internal/notify`, `TestChannelsLifecycle`
- [X] Provider errors scrubbed before the log and audit — `internal/notify` `shortReason`, `TestSend`
- [X] Credential-free export strips secret fields; with-credentials export is audited — `internal/transfer`, `TestBackup`
- [X] No marker (`NOTIF-MARKER-PW-`) in any table, log or credential-free export — `TestChannels`/`TestSend` scans, `make redaction-scan`

## Template safety (T2, SC-002)

- [X] Fixed FuncMap; no file/env/network; values never executed — `internal/render` (100%), `FuzzTemplate`
- [X] HTML bodies escape contextually; `{{` in a value is data — `FuzzVariables`
- [X] Render bounded to 1 s and 1 MiB — `render` tests

## Email transport (T3)

- [X] Header injection refused (CR/LF/control) — `internal/channel/email` (100%), `FuzzHeaderValue`
- [X] Recipient is one address; PLAIN auth only over TLS; plaintext only when allowed — `email` tests, `FuzzRecipient`

## Tenant isolation & access (T4, T5, SC-003, SC-004, SC-009)

- [X] Per-call tenant transaction under RLS; cross-tenant insert refused — `TestMigrationsAndRLS`
- [X] Unreadable ids answer `not_found` — `TestChannelsLifecycle`, `TestAccess`
- [X] Granter needs share; never above own relation; `use` gates sending — `internal/authz` (100%), `TestAccess`
- [X] Every grant and refusal audited — `TestAccess`

## Service API (T6)

- [X] mTLS + policy allow-list; tenant from the request, actor from the SPIFFE id; tenant-wide use required — `internal/grpcapi`, `TestNotifierRPC`, policy contract test

## Live stream (T7, SC-007)

- [X] Events targeted per user; 5/person, 2000/tenant; replay window then `reset` — `internal/stream` (100%), `TestStream`
- [X] Framing never broken by event type/data — `FuzzSSEFrame`

## Rate & fan-out (T8, SC-005, SC-006)

- [X] Rate limits fail closed — `internal/stream` `Limited`, `notify` tests
- [X] Scheduler publishes exactly once after a crash — `TestScheduler`

## Backup (T9, SC-008)

- [X] Import bounded (size/depth/items) and schema-validated — `internal/transfer`, `FuzzBackup`, `TestBackup`

## Audit (T10)

- [X] Closed vocabulary; detail guard drops credential/content keys; strings truncated — `internal/audit` (99%), `TestEveryMutationAudited`

## Gates

- [X] `make cover` — 100% on `internal/{authz,render,sealed,stream,channel/email}`, >=80% overall
- [X] `make fuzz` — every target runs clean
- [X] `make redaction-scan` — zero markers in captured output
