# Gateway manifest (feature 006)

```yaml
module: notification
display_name: Notifications
version: 1.0.0
prefixes: ["/api/notification", "/ui"]          # /ui = federated remote assets (relayed under /m/notification/)
permissions:
  - { resource: channels,      action: read,   description: "List and read notification channels the caller is granted (settings redacted)" }
  - { resource: channels,      action: manage, description: "Create, change, test and delete channels the caller is granted" }
  - { resource: templates,     action: read,   description: "List, read and preview templates the caller is granted" }
  - { resource: templates,     action: manage, description: "Create, change and delete templates the caller is granted" }
  - { resource: notifications, action: send,   description: "Send notifications through templates and channels the caller may use" }
  - { resource: notifications, action: read,   description: "Read the notification log (own sends without stats:read)" }
  - { resource: messages,      action: read,   description: "List message categories and messages" }
  - { resource: messages,      action: manage, description: "Create, send, revoke and archive internal messages; manage categories" }
  - { resource: inbox,         action: read,   description: "Read and manage the caller's own inbox and live stream" }
  - { resource: events,        action: publish, description: "Publish live events to users of the tenant (modules)" }
  - { resource: permissions,   action: manage, description: "Grant and revoke access on channels and templates the caller may share" }
  - { resource: backup,        action: manage, description: "Export and import tenant backups (bulk disclosure with credentials on request)" }
  - { resource: stats,         action: read,   description: "Read statistics, health and the audit trail" }
abilities:
  - { action: [read, create, update, delete, share, use], subject: [Channel], requires: "channels:read" }     # refined client-side by grants
  - { action: [read, create, update, delete, share, use], subject: [Template], requires: "templates:read" }
  - { action: [send], subject: [Notification], requires: "notifications:send" }
  - { action: [read], subject: [NotificationLog], requires: "notifications:read" }
  - { action: [manage], subject: [Message, MessageCategory], requires: "messages:manage" }
  - { action: [read], subject: [Inbox], requires: "inbox:read" }
  - { action: [manage], subject: [NotificationGrant], requires: "permissions:manage" }
  - { action: [manage], subject: [NotificationBackup], requires: "backup:manage" }
  - { action: [read], subject: [NotificationStats, NotificationAudit], requires: "stats:read" }   # subjects are platform-unique
nav:
  - { title: Inbox,       path: /notification/inbox,       icon: mdi-inbox-outline,            order: 200, requires: "inbox:read" }
  - { title: Channels,    path: /notification/channels,    icon: mdi-radio-tower,              order: 210, requires: "channels:read" }
  - { title: Templates,   path: /notification/templates,   icon: mdi-file-document-edit-outline, order: 220, requires: "templates:read" }
  - { title: Log,         path: /notification/log,         icon: mdi-history,                  order: 230, requires: "notifications:read" }
  - { title: Messages,    path: /notification/messages,    icon: mdi-message-text-outline,     order: 240, requires: "messages:read" }
  - { title: Categories,  path: /notification/categories,  icon: mdi-tag-multiple-outline,     order: 250, requires: "messages:manage" }
  - { title: Permissions, path: /notification/permissions, icon: mdi-shield-account-outline,   order: 260, requires: "permissions:manage" }
routes: derived from notification-api.openapi.yaml: every operation carries `x-freya-permission`;
        the stream route carries `x-freya-timeout-seconds: 300` (the gateway maximum),
        send/test routes 60, message send and backup routes 120, backup import
        `x-freya-max-body-bytes: 16777216`. No public route.
methods: []            # notification.v1 is called service-to-service, not through the gateway
remote: { entry: /m/notification/mf-manifest.json, exposes: ["./routes", "./nav", "./header"] }
```

Built-in grants registered with `auth.v1.Authorization/RegisterPermissions`
(`builtin_grants`, research R12):

| Role | Permissions |
|------|-------------|
| owner, admin | all thirteen |
| member | channels:read, templates:read, notifications:send, notifications:read, messages:read, inbox:read |
| auditor | stats:read, notifications:read |
| operator | stats:read |

Gateway allow-list entry for development:
`spiffe://example.org/svc/notification=/api/notification,/ui;notification`.

`policy.yaml` of the module (service callers of `notification.v1`):

```yaml
rules:
  - { method: "/notification.v1.Notifier/Send",     allow: ["spiffe://example.org/svc/warden", "spiffe://example.org/svc/auth"] }
  - { method: "/notification.v1.Notifier/SendTest", allow: ["spiffe://example.org/svc/gateway"] }
  - { method: "/notification.v1.Events/Publish",    allow: ["spiffe://example.org/svc/warden", "spiffe://example.org/svc/auth", "spiffe://example.org/svc/gateway"] }
```
