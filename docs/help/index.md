---
title: Notifications
summary: Email channels and templates, the delivery log, internal messages and your inbox. Also the platform's central path for outbound mail from the other modules.
kind: guide
order: 0
tags: [notification, email, templates, messages, inbox, overview]
---
The notifications module sends mail and in-portal messages for your
tenant, and keeps a log of what was sent.

## Concepts

- **Channel**: a way out, with its settings and credentials. **Email**
  (an SMTP relay) delivers today. The types `sms`, `slack` and `sse` can be
  configured but have no provider yet, so they cannot deliver. Credentials
  are encrypted and never shown again.
- **Template**: a subject and body for one channel, written as a Go
  template with named variables, for example `Hello {{.name}}`.
- **Delivery log**: every email sent, with its status (`pending`, `sent`,
  `failed`) and the rendered text.
- **Message**: an internal announcement to some people, or to everyone in
  the tenant. It lands in their **inbox** in the portal, now or at a
  scheduled time.
- **System template**: the wording of the mail other modules send through
  this one, such as invitations from auth or share links from warden.

Access to each channel and template is decided by **grants** (`owner`,
`editor`, `viewer`, `sharer`) on top of your role; see
[permissions](reference.md#access-levels).

## Pages in the portal

| Page | Shows | Needs |
|---|---|---|
| Inbox | the messages addressed to you; the bell in the top bar shows the unread count | `inbox:read` |
| Channels | channels, their status, default and template count; **Send test** | `channels:read` |
| Templates | templates, search, preview | `templates:read` |
| Log | the delivery log; statistics, audit and backup for administrators | `notifications:read` |
| Messages | internal messages and their state | `messages:read` |
| Categories | message categories | `messages:manage` |
| Permissions | grants on channels and templates | `permissions:manage` |

## Where to go next

- [Get started](getting-started.md): set up an email channel and a template.
- [Write templates](guides/templates.md)
- [Send internal messages](guides/messages.md)
- [Check deliveries in the log](guides/log.md)
- [Example: announce a maintenance window](examples/maintenance-announcement.md)
- [Reference](reference.md): fields, states, limits, permissions and roles.
