---
title: Reference
summary: Channel, template and message fields and limits, delivery and message states, access levels, permissions and roles, send limits and the system template keys.
kind: reference
order: 60
tags: [notification, reference, limits, permissions, roles, states]
---
## Channels

| Field | Notes |
|---|---|
| Name | 1–200 characters, unique |
| Type | `email`, `sms`, `slack` or `sse`; fixed after creation. Only `email` delivers today |
| Enabled | a disabled channel refuses sends |
| Default for this type | one per type; the default channel is shared with everyone in the tenant as `sharer`, so all may use it. The default email channel also carries the tenant's system mail |
| Email settings | SMTP host, port, TLS (`starttls`, `implicit`, `none`), From, Reply-To, username, password |
| Other types | account and API key |

Passwords and API keys are encrypted and never returned. A channel that
templates still use cannot be deleted.

The **Platform email** channel (badge **Managed**) comes from the module's
configuration: it cannot be edited or deleted in the portal, but **Send
test** works.

## Templates

| Field | Limit |
|---|---|
| Name | 1–200 characters, unique |
| Channel | required; the template takes its type |
| Subject | up to 500 characters in the portal |
| Body | up to 65,536 characters in the portal; HTML for email, text otherwise |
| Declared variables | up to 50; letters, digits and `_`, starting with a letter |
| Default for this channel | one per channel |

At send time: at most 50 variables, each value up to 16 KiB, 64 KiB in all;
rendering stops after one second or 1 MiB of output.

## Delivery states

`pending` (being delivered), `sent`, `failed` (with the reason). Test sends
carry `test`; system mail carries its template key.

## Messages

| Field | Limit |
|---|---|
| Title | 1–300 characters |
| Content | 1–20,000 characters, plain text |
| Type | `notification`, `private`, `group` |
| Recipients | everyone in the tenant, or up to 10,000 people |
| Schedule | optional, in the future |

| From | Action | To |
|---|---|---|
| `draft` | Send (no schedule) | `published` |
| `draft` | Send (future schedule) | `scheduled` |
| `scheduled` | Cancel, or save without a future time | `draft` |
| `scheduled` | the time arrives | `published` |
| `published` | Revoke | `revoked` |
| `published`, `revoked` | Archive | `archived` |
| `draft`, `revoked`, `archived` | Delete | removed |

Inbox entries are `unread` or `read`.

## Access levels

Grants on a channel or template go to a user, a role or the whole tenant,
optionally with an expiry. Manage them on **Notifications → Permissions**
(click a channel or template); creating a channel or template makes you its
`owner`.

| Level | Read | Change | Delete | Share | Use (send through it) |
|---|:-:|:-:|:-:|:-:|:-:|
| `owner` | yes | yes | yes | yes | yes |
| `editor` | yes | yes | no | no | yes |
| `sharer` | yes | no | no | yes | yes |
| `viewer` | yes | no | no | no | no |

You may grant at most your own level, and only where you can share.

## Permissions and roles

| Permission | Allows |
|---|---|
| `channels:read` | list and read channels (credentials hidden) |
| `channels:manage` | create, change, test and delete channels |
| `templates:read` | list, read and preview templates |
| `templates:manage` | create, change and delete templates; edit and restore system templates |
| `notifications:send` | send through templates and channels you may use |
| `notifications:read` | the delivery log (only your own sends without `stats:read`) |
| `messages:read` | list categories and messages |
| `messages:manage` | write, send, revoke and archive messages; manage categories |
| `inbox:read` | your own inbox and live updates |
| `events:publish` | push live events to users (for modules) |
| `permissions:manage` | grant and revoke access to channels and templates |
| `backup:manage` | export and import backups |
| `stats:read` | statistics, health and the audit trail |

| Role | Permissions |
|---|---|
| Notifications administrator | all except `events:publish` |
| Notifications sender | `channels:read`, `templates:read`, `notifications:send`, `notifications:read`, `messages:read`, `messages:manage`, `inbox:read` |
| Notifications viewer | `channels:read`, `templates:read`, `notifications:read`, `messages:read`, `inbox:read` |

Built-in roles: owner and admin get everything; member gets
`channels:read`, `templates:read`, `notifications:send`,
`notifications:read`, `messages:read`, `inbox:read`; auditor gets
`stats:read` and `notifications:read`; operator gets `stats:read`.

## Default limits

| Limit | Default |
|---|---|
| sends per tenant | 600 per minute |
| sends per sender | 60 per minute |
| system mail per calling module | 300 per minute |
| live connections | 5 per person, 2,000 per tenant |
| backup upload | 16 MiB |

## System template keys

`auth.invite`, `auth.account_reset`, `auth.recovery`, `auth.message`,
`warden.share`, `lcm.certificates_expiring`, `signing.invitation`,
`signing.next_signer`, `signing.certificate_setup`, `signing.reminder`,
`signing.completed`, `signing.declined`, `signing.cancelled`,
`signing.expired`, `signing.certificate_locked`, `hr.request_submitted`,
`hr.request_approved`, `hr.request_rejected`, `hr.request_revoked`,
`hr.signing_failed`, `hr.allowance_overdrawn`.

A module may send only the keys that start with its own name.
