# Security model

The threats and mitigations for feature 006 (research.md STRIDE; SC-001...SC-009)
and feature 017, central email delivery (T11-T15).

| # | Threat | Mitigation | Enforced in | Tested by |
|---|--------|------------|-------------|-----------|
| T1 | Channel credentials read from the database, an export, a log or an error | Settings sealed with an AES-256-GCM DEK wrapped by the KEK; responses carry only public fields and the `"__set__"` marker; provider errors are scrubbed before audit/log; credential-free export strips secret fields | `internal/sealed`, `internal/notify`, `internal/transfer` | `sealed` (100%), `notify`, `TestChannels`/`TestSend` scans, redaction-scan |
| T2 | Template injection / SSRF / data exfiltration through a template | Fixed FuncMap (no file/env/network); values are strings, never executed; HTML bodies escape contextually; render bounded to 1 s / 1 MiB | `internal/render` | `render` (100%), `FuzzTemplate`, `FuzzVariables` |
| T3 | Email header injection / relay abuse | Every header checked for CR/LF/control; recipient parsed as one address; PLAIN auth only over TLS; plaintext only when the service allows it | `internal/channel/email` | `email` (100%), `FuzzHeaderValue`, `FuzzRecipient` |
| T4 | Cross-tenant read/write | Per-call tenant transaction under Postgres RLS; ids of unreadable resources answer `not_found`, never `forbidden` | `internal/store` (RLS), `internal/httpapi` | `TestMigrationsAndRLS`, `TestAccess`, `TestChannels` |
| T5 | Privilege escalation through a grant | Granter needs `share` and may not hand out a relation above their own; `use` gates sending | `internal/authz` | `authz` (100%), `TestAccess` |
| T6 | Spoofed service caller on the gRPC API | mTLS + `deploy/policy.yaml` allow-list; the tenant comes from the request and the actor from the SPIFFE id; tenant-wide `use` required | `internal/grpcapi`, `deploy/policy.yaml` | `grpcapi`, `TestNotifierRPC`, policy contract test |
| T7 | Live-stream cross-delivery / resource exhaustion | Events targeted per user; 5 streams/person, 2000/tenant; replay bounded to the window then `reset`; the module closes at 290 s | `internal/stream` | `stream` (100%), `FuzzSSEFrame`, `TestStream` |
| T8 | Unbounded fan-out or send flooding | Valkey rate limits (600/min tenant, 60/min sender) that fail closed; fan-out batched; the scheduler leases due messages | `internal/notify`, `internal/messages` | `notify`, `TestScheduler`, perf check |
| T9 | Backup as a bulk-disclosure or DoS vector | Credentials only on request and audited as `backup_exported_with_credentials`; import bounded (16 MiB, depth, item count) and schema-validated | `internal/transfer`, `internal/httpapi` | `transfer`, `FuzzBackup`, `TestBackup` |
| T10 | Audit trail leaking credentials or content | Closed vocabulary; a detail guard drops keys containing `password`, `secret`, `token`, `api_key`, `body`, `content`; strings truncated | `internal/audit` | `audit` (99%), `TestEveryMutationAudited` |
| T11 | One-time links (invitations, recovery, share links) disclosed through the delivery log, audit, logs or errors | System templates mark the link variables secret; the stored log copy is a second render with `[redacted]` in their place (any escaping context), long secret values are also string-scrubbed; failure reasons are scrubbed of secret values; audit rows carry no variables | `internal/render` (`RenderRedacted`), `internal/notify` | `render` (100%), `TestSystemSendRedaction`, `TestSystemSendRedactionScan` |
| T12 | A service sending another service's system template (phishing with legitimate wording) | The key namespace comes from the verified SPIFFE id (`svc/<name>` → `<name>.*`); a foreign key is `PermissionDenied` and audited as `access_refused`/`key_namespace`; mesh policy admits only auth and warden | `internal/grpcapi`, `internal/notify` | `TestNotifierSendKeyNamespaces`, `TestSendKeyRefusals`, `FuzzSystemKey`, `FuzzServiceFromSPIFFE` |
| T13 | Relay password disclosed through configuration, API reads, backups or logs | `password_file` only (literal refused, mode 0640 or stricter); sealed like channel credentials; the managed channel is read-only and never exported | `internal/config`, `internal/notify`, `internal/transfer` | `TestPlatformEmailRejects`, `TestEnsurePlatformChannel`, `TestExportSkipsManaged` |
| T14 | Silent downgrade to plaintext or credentials in clear | `tls: none` needs the named opt-out (warned at every start); a username with `none` refused; STARTTLS without fallback; certificate verified against the configured host | `internal/config`, `internal/channel/email` | `TestPlatformEmailRejects`, `TestPlatformEmailChannel` |
| T15 | Mail flooding by a bulk operation (500 activations) | `system:<service>` rate limit (300/min), throttled sends reported retryable so auth's queue drains later | `internal/notify` | `TestSendKeyRateLimit` |

Cross-service note: the gateway and the auth service both cache authorization
decisions in Valkey; their keys are namespaced (`gwdec:` vs `dec:`) so a shared
Valkey never lets one service read the other's value and mis-decide — a
prerequisite for the module's `stats:read`/`messages:manage` listing widening.
