---
title: Check deliveries in the log
summary: Find a sent email, read what was delivered or why it failed, and use the statistics and audit trail.
kind: guide
order: 40
tags: [notification, log, delivery, troubleshooting, audit]
requires: notifications:read
---
Open **Notifications → Log**. The delivery log lists the last 7 days, newest
first. Without `stats:read` you see only what you sent yourself.

| Column | Meaning |
|---|---|
| When | when the send was made |
| Recipient | the address |
| Subject | the rendered subject |
| Status | `pending`, `sent` or `failed`; a **test** badge for channel tests, and the key of a system template such as `warden.share` |
| Channel | the channel type |
| Sender | the person who sent it, if any |

Filter by **Status** or by **Recipient**. Click a row to read the rendered
body, and for a failed entry the reason the relay gave.

## Why a send failed

A `failed` entry carries the relay's reason: it refused the message or
could not be reached. Check the channel's host, port, TLS mode and
credentials with **Send test** on the channel. A failed entry with
`no_provider` used a channel type (`sms`, `slack`, `sse`) that cannot
deliver yet.

Some sends are refused before anything is delivered, so they leave no log
entry; the caller gets the error instead:

- `channel_disabled`: the channel is switched off.
- `rate_limited`: the tenant or the sender is over the send rate (by default
  600 per minute per tenant and 60 per sender).
- `email_not_configured` (system mail from other modules): the tenant has no
  enabled default email channel and the platform channel is not configured.

Values of secret variables, such as invitation and share links, show as
`[redacted]`.

## Statistics and audit

With `stats:read` the page also shows the number of channels, templates,
sends in the last 24 hours, messages, open live streams and operations, and
an **Audit** tab with the events of the last 7 days (who did what, and the
outcome). Audit entries never contain credentials or message bodies.

## Backup

With `backup:manage`, **Backup** exports and imports channels, templates and
message categories:

- **Export** leaves credentials out unless you tick **Include channel
  credentials**; that export is recorded as a bulk disclosure. The managed
  platform channel is never exported.
- **Import** matches by name (channels by name and type). **On duplicates**:
  skip or overwrite. The report counts created, skipped, overwritten and
  failed entries per kind, with warnings.
