---
title: Write templates
summary: Write subjects and bodies with variables and functions, preview them, send through the API, and edit the wording of system mail.
kind: guide
order: 20
tags: [notification, templates, variables, preview, system-templates]
requires: templates:read
---
A template belongs to one channel and holds a **Subject** and a **Body**.
Both are Go templates: `{{.name}}` inserts the value of the variable
`name`. Creating and changing templates needs `templates:manage` and, for a
new template, read access to its channel.

## Variables

List every variable the template uses under **Declared variables
(comma-separated)**. Names start with a letter and contain letters, digits
and `_`, for example `first_name`. Saving refuses a template that uses a
variable it does not declare, and reports a syntax error with its position.

When a template is sent, a value must be supplied for every variable the
subject or body uses.

## Functions

| Function | Example | Result |
|---|---|---|
| `upper`, `lower` | `{{upper .code}}` | `ABC123` |
| `title` | `{{title .name}}` | `Ada Lovelace` |
| `trim` | `{{trim .note}}` | the value without surrounding spaces |
| `default` | `{{default "there" .name}}` | `there` when `name` is empty |
| `date` | `{{date "2006-01-02"}}` | today's date (UTC) in Go layout |
| `join` | `{{join ", " .a .b}}` | `a, b` |

Go template actions such as `{{if .message}}...{{end}}` work too.

## Email bodies

Email bodies are HTML. They open in a visual editor; variables and other
template actions show as chips, and **Insert variable** adds one of the
declared variables. Use the source toggle for the raw HTML. A body the
editor cannot keep exactly (inline styles, tables, images, an action between
blocks) opens in source mode with a warning and is never rewritten. Values
inserted into an HTML body are escaped.

Bodies of `sms`, `slack` and `sse` templates are plain text.

## Preview

Under **Preview**, enter a value for each variable and click **Preview**.
The rendered subject and body appear below; nothing is sent.

## Send

The portal has no send form: notifications are sent by integrations through
the API, with `notifications:send` and `use` access to both the template and
its channel (`owner`, `editor` or `sharer`). The request is
`POST /api/notification/v1/notifications/send` with this body:

```json
{
  "template_id": "0b9e6b1e-6a0e-4c43-9a43-6f1f1b0c2a10",
  "recipient": "ada@example.com",
  "variables": { "name": "Ada" }
}
```

`channel_id` may name another enabled channel of the same type. The answer
is the log entry with status `sent` or `failed`; see
[check deliveries](log.md).

## System templates

Mail that other modules send through this one (auth invitations, warden
share links, lcm expiry warnings, signing, hr) uses **system templates**.
They live in the platform tenant, where they show a **System** badge, and
**Edited** once changed.

- Only **Subject** and **Body** can change; name, channel and variables are
  fixed.
- Variables marked **(required)** must stay, because the sending module
  always supplies them (for example the link in an invitation).
- Variables listed as **Secret** are stored as `[redacted]` in the delivery
  log.
- **Restore built-in** brings back the original wording. Upgrades refresh the
  built-in copy but keep your edits.

System mail goes out through the tenant's enabled default email channel if
it has one, otherwise through the platform's **Platform email** channel.
