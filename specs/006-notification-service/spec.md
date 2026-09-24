# Feature Specification: Notification Service

**Feature Branch**: `006-notification-service`

**Created**: 2026-09-17

**Status**: Draft

**Input**: User description: "Create a new service under services/ named notification (like services/auth, services/gateway and services/warden; it will later move to its own repository), replicating the functionality of /home/jadmin/projects/go-tangra/go-tangra-notification as a Freya platform module: a tenant-scoped multi-channel notification service. Administrators manage notification channels (name, type email|sms|slack|sse, provider configuration such as SMTP host/port/username/password/from stored encrypted at rest and never returned in full, enabled flag, one default channel per type; email is fully implemented via SMTP with a "send a test message" action, the other types are declared for later providers) and notification templates (name, channel, subject and body as Go templates with a declared variable list, one default per channel, preview with sample variables). Any module or user with permission can send a notification: pick a template (optionally overriding the channel), a recipient and variables; the service renders subject and body, delivers through the channel and records a notification log entry (channel, template, recipient, rendered subject/body, delivery status pending|sent|failed with error message, sender, timestamps) that can be listed with filters and inspected. Access to templates and channels is Zanzibar-style like warden: Owner (read, write, delete, share, use), Editor (read, write, use), Viewer (read), Sharer (read, share, use) granted to users, roles or the whole tenant with optional expiry; the API offers grant, revoke, list, check and effective permissions, and "use" is what sending requires. The service also provides internal messages (in-app inbox): message categories (name, description, ordering), messages (title, content, type notification|private|group, status draft|published|scheduled|revoked|archived, sender, category, scheduled publish time) sent to one or many recipients (users, or everyone in the tenant) with per-recipient status sent|received|read|revoked|deleted, an inbox per user (list, mark read, mark status in bulk, delete from inbox), revoke by the sender, and scheduled publishing executed by the service itself. Real-time push: a server-sent-events stream per signed-in user through the gateway that delivers inbox events and arbitrary events published by other modules (publish to one user, publish in bulk), with reconnection and heartbeat. Tenant backup export/import of channels, templates and categories (skip or overwrite duplicates). Creator/updater tracking and an audit trail on every operation. It registers with the application gateway (routes, API permissions such as channels:read/manage, templates:read/manage, notifications:send/read, messages:read/manage, inbox:read, permissions:manage, backup:manage, plus CASL abilities), exposes gRPC methods for module-to-module sending and event publishing, and ships its UI as a Module Federation remote (channels list with drawer editor and test send, templates list with drawer editor and preview, notification log, messages and categories management, an inbox with unread badge in the shell header, permissions manager) composed by the platform shell following the Materio design; user identity, tenant, roles and display names come from the auth service."

## Overview

The notification service is how the platform and its modules reach people:
by email (and, later, text message, chat or other providers) through
**channels** an administrator configures, using **templates** that turn a few
variables into a finished subject and body, and by **internal messages** that
land in each person's in-app **inbox** and are pushed to their open browser
tabs the moment they arrive. Other modules use it the way a person would: pick
a template, name a recipient, hand over the variables, and the service renders,
delivers and records the outcome.

It is a platform module like every other: it registers with the application
gateway (feature 003), takes who-you-are, your tenant, your roles and display
names from the authentication service (features 002 and 004), and shows its
screens inside the platform shell following the shell's design.

Five kinds of people use it: **tenant administrators** who configure channels
and templates, decide who may use them and keep backups; **senders** (members
or other modules) who send notifications and internal messages; **recipients**
who read their inbox and receive live updates; **template authors** who write
and preview templates they own or may edit; and **platform operators** who
watch delivery outcomes and health without reading anyone's inbox.

Provider credentials (such as a mail relay password) are the only secrets the
service holds; they are stored encrypted and are never shown again once saved.
Every operation is audited.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure Channels and Templates, Send a Notification (Priority: P1)

A tenant administrator creates an email channel with the mail relay settings
(host, port, username, password, sender address), enables it, marks it as the
default email channel and sends a test message to their own address to prove
it works. A template author creates a template "welcome" bound to that channel
with a subject and a body that mention `{{.Name}}` and `{{.Link}}`, declares
those two variables, previews it with sample values, and marks it as the
channel's default. A sender (a person, or another module acting for its
tenant) sends "welcome" to `alice@example.org` with `Name=Alice` and a link;
the service renders subject and body, delivers the email and records a log
entry that the administrator can find by recipient, template, channel, status
or period, open, and see exactly what was sent and whether it reached the
relay.

**Why this priority**: Delivering a rendered message through a configured
channel is the core of the service; everything else builds on it.

**Independent Test**: Create an email channel against the development mail
catcher, send a test message, create a template with two variables, preview it,
send it to a recipient with variables, see the email in the mail catcher and
a `sent` log entry with the rendered subject and body; send with the relay
down and see a `failed` entry with the reason; the relay password never
appears in any listing, log entry, audit event or error message.

**Acceptance Scenarios**:

1. **Given** a signed-in administrator of tenant "acme", **When** they create
   an email channel "relay" with valid relay settings, **Then** the channel is
   listed with its name, type, enabled flag and default flag, the settings are
   shown without the password (a "password set" indicator only), the creator
   is recorded, and the creation is audited.
2. **Given** an email channel, **When** the administrator sends a test message
   to an address, **Then** a message with a recognisable test subject reaches
   that address and a log entry marked as a test records the outcome; a relay
   that refuses the connection yields a `failed` entry with a readable reason
   and no password in it.
3. **Given** a channel of type "email", **When** the administrator marks it as
   default, **Then** it becomes the only default email channel of the tenant
   (the previous default loses the flag).
4. **Given** a channel referenced by templates, **When** the administrator
   tries to delete it, **Then** the deletion is refused with the count of
   templates using it; a channel with no templates is deleted and audited.
5. **Given** a template author with write access, **When** they save a
   template whose body refers to a variable that is not declared, **Then**
   the save is refused naming the variable; **When** the body has a syntax
   error, **Then** the save is refused with the position of the error.
6. **Given** a template with declared variables `Name` and `Link`, **When**
   the author previews it with sample values, **Then** they see the rendered
   subject and body without anything being sent or logged as a delivery.
7. **Given** a template bound to channel "relay", **When** a sender sends it
   to a recipient with all declared variables, **Then** the recipient receives
   the rendered message, a `sent` log entry stores channel, template,
   recipient, rendered subject and body, sender and times, and the send is
   audited.
8. **Given** the same template, **When** a sender sends it while overriding
   the channel with another enabled email channel they may use, **Then** the
   message goes through that channel and the log entry names it.
9. **Given** a send with a missing variable, an unknown template, a disabled
   channel, a channel of a different type than the template's, or a recipient
   that is not a valid address for the channel type, **Then** the send is
   refused with the reason and nothing is delivered.
10. **Given** a log listing, **When** an operator filters by channel, template,
    recipient, status or period, **Then** only matching entries are returned,
    newest first, in pages.

---

### User Story 2 - Decide Who May Read, Edit, Share and Use (Priority: P1)

An administrator who owns a template grants "Editor" to the role
"support-leads", "Viewer" to a colleague and "Sharer" to the role "ops"; the
support leads can now edit the template, the colleague can read it, ops can
use it to send and grant others "use" without being able to change it. The
same applies to channels: a module or team is given "use" on a channel so it
can send through it without seeing or changing its configuration. Every
grant carries an optional expiry and can be revoked; the service answers
"may this subject do this on that resource?" and explains where a person's
permissions come from.

**Why this priority**: Templates and channels are shared infrastructure; the
value of the service depends on delegating their use safely without handing
over credentials or editing rights.

**Independent Test**: As the owner of a template grant Editor to a role,
Viewer to a user and Sharer to another role; verify each can do exactly their
actions (edit / read only / send and grant use) and nothing more; revoke the
Editor grant and verify editing is refused within one request; verify the
Sharer cannot grant Owner or Editor.

**Acceptance Scenarios**:

1. **Given** a template created by Alice, **When** Bob (no grant) tries to
   read, edit, send with or share it, **Then** every attempt is refused and
   audited; the template is not even listed for Bob.
2. **Given** Alice grants Bob "Viewer", **When** Bob lists templates, **Then**
   the template appears with his effective permissions (read only); sending
   with it is refused.
3. **Given** Alice grants role "ops" "Sharer", **When** Carol (holding "ops")
   sends with the template, **Then** the send succeeds; **When** Carol grants
   Dave "Viewer", **Then** it succeeds; **When** Carol grants Dave "Editor" or
   "Owner", **Then** it is refused as above her own relation.
4. **Given** a grant with an expiry in the past, **When** the subject acts,
   **Then** the grant has no effect and the listing marks it expired.
5. **Given** a channel with "use" granted to the whole tenant, **When** any
   member sends a notification through it, **Then** the send is allowed
   while the channel's configuration remains hidden from them.
6. **Given** a resource, **When** its owner asks for the effective permissions
   of a subject, **Then** the answer lists the strongest relation, the
   resulting actions and each grant that contributed (direct, through a role,
   through the tenant).

---

### User Story 3 - Internal Messages and the Inbox (Priority: P2)

A sender composes an internal message with a title, content, a type
(notification, private or group) and a category ("Maintenance"), and sends it
to selected people or to everyone in the tenant, now or at a scheduled time.
Each recipient sees it in their inbox with an unread badge in the shell header,
opens it (which marks it read), can mark several as read or unread at once and
remove it from their inbox. The sender can revoke a message, which withdraws it
from every inbox where it is still unread. Administrators manage the
categories and can list, archive and delete messages.

**Why this priority**: The inbox is the platform's own channel to its users
and needs no external provider, but it depends on the identity, audit and
push foundations of stories 1 and 4.

**Independent Test**: Create a category, send a message to two users and one
scheduled message to everyone; both users see the immediate message with the
unread count 1; the first reads it (count 0), the second deletes it; when the
scheduled time comes every member sees the second message; the sender revokes
it and it disappears from every inbox where it was unread.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they create categories "Maintenance"
   and "Announcements" with an ordering, **Then** the categories are listed in
   that order and can be renamed or deleted (deletion is refused while
   messages use the category).
2. **Given** a sender, **When** they send a message titled "Downtime tonight"
   to Bob and Carol, **Then** Bob and Carol each have an inbox entry with
   status "sent", the message shows status "published", and each recipient's
   unread count increases by one.
3. **Given** a message sent to "everyone", **When** it is published, **Then**
   every active member of the tenant has an inbox entry; members who join
   later do not receive it (membership is resolved at publish time).
4. **Given** a message scheduled for a future time, **When** that time comes,
   **Then** the service publishes it within one minute without any user
   action, and the message status becomes "published".
5. **Given** an unread inbox entry, **When** the recipient opens it, **Then**
   its status becomes "read" and the unread count decreases; **When** they
   mark ten entries read at once, **Then** all ten change in one action.
6. **Given** an inbox entry, **When** the recipient deletes it, **Then** it no
   longer appears in their inbox while the sender's message and other
   recipients' entries are unaffected.
7. **Given** a published message, **When** the sender (or an administrator)
   revokes it, **Then** entries that were not yet read are marked "revoked" and
   hidden from the inbox; entries already read remain visible marked
   "revoked".
8. **Given** a draft message, **When** the sender edits and then sends it,
   **Then** recipients receive the edited version; a published message cannot
   be edited, only revoked or archived.

---

### User Story 4 - Live Updates in the Browser (Priority: P2)

A signed-in person keeps the platform open; when an internal message arrives,
their inbox badge updates without reloading. Other modules push their own
events to a person (for example "your import finished") or to many people at
once, and those events reach the open tabs of exactly those people. If the
connection drops, the browser reconnects on its own and misses nothing that
was sent in the meantime up to a short replay window.

**Why this priority**: Live delivery is what makes the inbox feel immediate
and lets other modules notify users, but the inbox also works without it.

**Independent Test**: Open two tabs as Alice and one as Bob; send Alice an
internal message and see her badge change in both tabs and not Bob's; publish
an event of type "import-finished" to Alice and see it arrive; kill the
connection, publish an event during the outage, restore it and see the event
delivered after reconnection.

**Acceptance Scenarios**:

1. **Given** a signed-in person, **When** their browser opens the live stream,
   **Then** the stream stays open, sends a heartbeat at least every 30 seconds
   and delivers only events addressed to that person or to everyone in their
   tenant.
2. **Given** an internal message sent to Alice, **When** it is published,
   **Then** every open tab of Alice receives an "inbox" event with the message
   id and title within two seconds.
3. **Given** another module publishing an event to a user id, **Then** that
   person's tabs receive the event with its type and payload; publishing to a
   list of user ids delivers to each of them; nobody else receives it.
4. **Given** a dropped connection, **When** the browser reconnects with the
   id of the last event it saw, **Then** events published in the meantime
   (within the replay window) are delivered in order; beyond the window the
   client is told to refresh its inbox.
5. **Given** a person who signs out, **Then** their stream is closed and no
   further events reach that tab.

---

### User Story 5 - Back Up and Operate (Priority: P3)

An administrator exports the tenant's channels, templates and categories to
a file and imports it into another tenant or after a reset, choosing to skip
or overwrite items that already exist (matched by name and type); channel
credentials are included only when the administrator asks for them and
otherwise must be re-entered. Operators check the service's health, see
counts (channels, templates, notifications by status, messages, live
connections) and browse the audit trail.

**Why this priority**: Portability and operability matter but come after the
service delivers value.

**Independent Test**: Export a tenant with two channels, three templates and
two categories; import into an empty tenant and see all seven items; import
again with "skip" and see zero changes; rename a template, import with
"overwrite" and see it restored; the exported file contains no credential
unless "include credentials" was chosen.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they export the tenant, **Then** they
   receive a file with channels (credentials omitted unless asked), templates
   and categories, and the export is audited as a bulk disclosure when it
   includes credentials.
2. **Given** a backup file, **When** it is imported with "skip", **Then**
   items whose name and type already exist are left untouched and the report
   counts created/skipped per entity; with "overwrite" they are replaced and
   the report counts overwritten.
3. **Given** a malformed or oversized file, **When** it is imported, **Then**
   it is refused with the reason and nothing changes.
4. **Given** an operator, **When** they ask for health, **Then** they learn
   whether the service, its database and the publisher of delayed messages
   are working, without any tenant data.

---

### Edge Cases

- A template's channel is disabled: sends through it are refused with
  "channel disabled"; the template stays editable and can be re-bound.
- A channel's credentials are changed while sends are in flight: in-flight
  sends finish with the settings they started with; later sends use the new
  ones.
- A send fails at the provider (relay down, refused recipient): the log entry
  is `failed` with the provider's reason (credentials scrubbed); the service
  does not retry on its own — the sender may send again.
- The same recipient is listed twice for an internal message: one inbox entry
  is created.
- A recipient is deactivated: no inbox entry is created for a new message;
  existing entries remain until deleted.
- A scheduled message whose time is already past when saved: published
  immediately.
- The service restarts with scheduled messages pending: they are published
  when their time comes after the restart; none is published twice.
- Two administrators mark different channels default at the same time: one
  default remains per type; the later write wins.
- A template variable list declares a variable the body never uses: allowed
  (the variable is optional in sends).
- Variables carry HTML or template syntax: rendered as plain text values in
  email bodies (escaped), never executed as template code.
- A module publishes an event to a user of another tenant: refused; events
  never cross tenants.
- Live stream clients that never read: the service drops the connection after
  the write buffer fills, and the client reconnects.
- Very large tenants (50k members): "everyone" messages create inbox entries
  in batches; the publish completes within minutes and the message shows
  "published" only when every entry exists.
- Backup import references a channel by name that does not exist for a
  template: the template is imported bound to no channel and reported as
  "needs a channel".

## Requirements *(mandatory)*

### Functional Requirements

**Channels**

- **FR-001**: Administrators MUST be able to create, read, list, update, enable
  or disable and delete notification channels with a name (1–100 characters,
  unique per tenant), a type (email, sms, slack, sse), provider settings
  (≤ 8 KiB, validated for the type at save time), an enabled flag and a
  default flag; at most one channel per type is default in a tenant.
- **FR-002**: Provider settings MUST be stored encrypted at rest; reads MUST
  return them with credential fields replaced by an indicator that a value is
  set; an update that omits a credential field MUST keep the stored value.
- **FR-003**: The email type MUST deliver through a mail relay (host, port,
  optional username and password, transport security, sender address,
  optional reply-to) with a connection timeout; the other types MUST be
  accepted for configuration and reported as "no provider" when a send is
  attempted.
- **FR-004**: Administrators MUST be able to send a test message through a
  channel to a recipient of their choice; the outcome is recorded in the
  notification log marked as a test.
- **FR-005**: Deleting a channel MUST be refused while templates reference it.

**Templates**

- **FR-006**: Template authors MUST be able to create, read, list, update and
  delete templates with a name (1–100 characters, unique per tenant), a
  channel, a subject (≤ 998 characters) and a body (≤ 256 KiB) written in the
  platform's template language, a declared list of variable names, and a
  default flag (at most one default template per channel).
- **FR-007**: Saving a template MUST validate the subject and body: syntax
  errors are refused with their position, references to undeclared variables
  are refused by name, and only the allowed functions of the template
  language are available.
- **FR-008**: Template authors MUST be able to preview a template with sample
  variables and receive the rendered subject and body without any delivery or
  log entry.

**Sending and the log**

- **FR-009**: Senders MUST be able to send a notification by template id,
  recipient (≤ 512 characters, valid for the channel type), variables
  (≤ 64 KiB in total) and an optional channel override; the service renders
  subject and body, delivers through the channel and answers with the log
  entry id and status.
- **FR-010**: Every send attempt MUST create a notification log entry with
  channel, channel type, template, recipient, rendered subject and body,
  status (pending, sent, failed), failure reason, sender, creation time and
  sent time; entries are immutable once final.
- **FR-011**: Readers of the log MUST be able to list entries newest first
  in pages filtered by channel, template, recipient, status and period, and
  read one entry.
- **FR-012**: Other modules MUST be able to send notifications and publish
  live events over the platform's service-to-service interface, acting for a
  tenant, with the same permission checks as a person holding "use" through
  the tenant.

**Access control**

- **FR-013**: Access to templates and channels MUST be expressed as grants of
  one relation (Owner: read, write, delete, share, use; Editor: read, write,
  use; Viewer: read; Sharer: read, share, use) on one resource to one subject
  (user, role, or the whole tenant) with an optional expiry.
- **FR-014**: The creator of a template or channel MUST become its Owner;
  tenant administrators (owner/admin roles) hold Owner on every resource of
  their tenant.
- **FR-015**: Only holders of `share` MAY grant or revoke on a resource and
  MUST NOT grant a relation stronger than their own; a repeated grant to the
  same subject replaces the earlier one.
- **FR-016**: The service MUST answer permission checks, list the grants on a
  resource, and explain a subject's effective permissions with the grants
  that contribute; listings MUST include only resources the caller may read
  together with the caller's effective actions.

**Internal messages and inbox**

- **FR-017**: Administrators MUST be able to manage message categories (name
  1–100 characters unique per tenant, description ≤ 500, ordering); deleting a
  category in use MUST be refused.
- **FR-018**: Senders MUST be able to create, read, list, update (while draft
  or scheduled), delete (draft), archive and revoke internal messages with a
  title (1–200), content (≤ 64 KiB), type (notification, private, group),
  category, and an optional scheduled publish time; status is one of draft,
  scheduled, published, revoked, archived.
- **FR-019**: Sending a message MUST create one inbox entry per distinct
  active recipient (chosen users, or every active member of the tenant),
  each with status sent, received, read, revoked or deleted, and publish an
  inbox event to each recipient's live stream.
- **FR-020**: Scheduled messages MUST be published by the service itself
  within one minute of their time, exactly once, including after a restart.
- **FR-021**: Recipients MUST be able to list their inbox (newest first,
  paged, with unread count), read an entry (marking it read), mark many
  entries read or unread at once, and delete entries from their inbox; nobody
  else can read a person's inbox.
- **FR-022**: Revoking a message MUST mark every unread entry revoked and
  hide it; read entries stay visible marked revoked.

**Live stream**

- **FR-023**: Every signed-in person MUST be able to open one or more live
  streams through the gateway that carry inbox events and module events
  addressed to them or to everyone in their tenant, with a heartbeat at least
  every 30 seconds and an event id per event.
- **FR-024**: Modules MUST be able to publish an event (type ≤ 64
  characters, payload ≤ 16 KiB) to one user or to a list of up to 1,000
  users of the module's tenant; delivery to open streams happens within two
  seconds.
- **FR-025**: A stream reopened with the id of the last event seen MUST
  replay the events missed within the replay window (at least five minutes)
  in order, or tell the client to refresh when the window is exceeded.

**Transfer, audit, platform**

- **FR-026**: Administrators MUST be able to export the tenant's channels
  (credentials only on request), templates and categories, and import such a
  file with skip or overwrite handling per item, receiving a per-entity
  report; files are size-limited and validated before any change.
- **FR-027**: Every create, update, delete, grant, revoke, send, test send,
  publish, revoke of a message, export and import MUST be audited with actor,
  subject, outcome and time; refusals are audited too; audit events never
  carry credentials, rendered bodies or message content.
- **FR-028**: The service MUST register with the application gateway as
  module "notification" with its routes, permissions (`channels:read`,
  `channels:manage`, `templates:read`, `templates:manage`,
  `notifications:send`, `notifications:read`, `messages:read`,
  `messages:manage`, `inbox:read`, `events:publish`, `permissions:manage`,
  `backup:manage`, `stats:read`), abilities and navigation entries, and ship
  its screens as a remote the shell composes; the inbox badge appears in the
  shell header for every signed-in person.
- **FR-029**: Creator and last updater MUST be recorded on channels,
  templates, categories and messages; display names are resolved through the
  authentication service when shown.
- **FR-030**: Operators MUST be able to read health (service, database,
  publisher of delayed messages) and per-tenant statistics (channels,
  templates, log entries by status, messages by status, open streams).

### Security Requirements *(mandatory — Constitution: Development Workflow)*

- **Trust boundaries crossed**: public ingress through the gateway (browser
  and the live stream); service-to-service calls from other modules
  (sending, publishing); outbound connections to mail relays and future
  providers; the authentication service for identities and display names.
- **Data classification**: provider credentials (secret); rendered message
  bodies and internal message content (confidential, may contain personal
  data); recipient addresses (personal data); audit events (internal-only).
- **Authentication/Authorization**: every request carries a platform identity
  (person or module) and tenant from the gateway; API permissions gate entry
  to an operation, Zanzibar grants decide per resource; modules act for one
  tenant and never across tenants; the live stream is bound to the signed-in
  person and closes on sign-out.
- **Threat scenarios**: credential disclosure through reads, logs, errors,
  backups or audit; template injection (executing template code from
  variables, reading files or environment through template functions); mail
  header injection through subject or recipient; sending through a channel
  without "use"; reading another person's inbox or stream; cross-tenant event
  publishing; oversized bodies, variables or files; open-relay abuse by
  unbounded sends.
- **SR-001**: Provider credentials MUST be encrypted at rest with a key
  loaded from the platform's secrets configuration, never returned after
  save, never logged, never included in audit events or error messages, and
  included in exports only on explicit request with a bulk-disclosure audit.
- **SR-002**: Template rendering MUST run with a fixed, safe function set
  (no file, environment or network access), treat variable values as data
  (escaped in HTML bodies), and be bounded in time and output size.
- **SR-003**: Recipient addresses, subjects and sender addresses MUST be
  validated against header injection (no line breaks) before delivery.
- **SR-004**: Every read, write, send, grant and stream subscription MUST be
  checked against the caller's identity and tenant; inbox and stream access
  is restricted to the person themselves.
- **SR-005**: Sends MUST be rate-limited per tenant and per sender, and
  payloads (variables, bodies, backup files, event payloads) size-limited,
  with limits documented.
- **SR-006**: The live stream MUST authenticate through the gateway session,
  deliver only events addressed to the stream's person or tenant, and bound
  per-person connections and replay memory.

### Key Entities *(include if feature involves data)*

- **Channel**: name, type, provider settings (credentials encrypted), enabled,
  default, creator/updater, times.
- **Template**: name, channel, subject, body, declared variables, default,
  creator/updater, times.
- **Notification Log Entry**: channel, channel type, template, recipient,
  rendered subject and body, status, failure reason, sender, test flag, times.
- **Grant**: resource (channel or template), subject (user, role, tenant),
  relation, granter, expiry, time.
- **Message Category**: name, description, ordering, creator/updater, times.
- **Internal Message**: title, content, type, status, category, sender,
  scheduled time, published time, creator/updater, times.
- **Inbox Entry**: message, recipient, status, read time, times.
- **Live Event**: id, tenant, target (user or list of users), type, payload,
  time; kept for the replay window only.
- **Audit Event**: actor, action, subject, outcome, time, redacted details.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An administrator can configure a working email channel and
  confirm it with a test message in under three minutes.
- **SC-002**: A rendered notification is handed to the mail relay within two
  seconds of the send request in 95% of cases, and its log entry is visible
  immediately with the final status.
- **SC-003**: No provider credential appears in any listing, log entry, audit
  event, export without the credentials option, or error message (verified by
  an automated scan with marker values).
- **SC-004**: A change in access (grant or revoke) takes effect on the very
  next request.
- **SC-005**: An internal message sent to a person appears in their open tabs
  within two seconds and in their inbox listing immediately; a message to
  everyone in a tenant of 10,000 members is fully delivered within one
  minute.
- **SC-006**: Scheduled messages publish within one minute of their time,
  exactly once, across service restarts.
- **SC-007**: A dropped live connection is restored automatically within ten
  seconds and no event within the replay window is lost.
- **SC-008**: Backup export and import of a tenant with 100 templates
  complete in under ten seconds each, and a round trip reproduces every
  channel, template and category.
- **SC-009**: Every operation in the audit list is present in the audit trail
  with actor, subject, outcome and time, including refusals.

## Assumptions

- The reference project's scheduler-driven "send test email" task becomes a
  direct "send a test message" action; delayed publishing of internal
  messages is done by the service itself rather than by an external
  scheduler, since the platform has none.
- Only the email provider is implemented; sms, slack and sse channel types can
  be configured but report "no provider" on send. The "sse" type is
  superseded by internal messages and the live stream and is kept only for
  backup compatibility.
- The template language is the platform's standard text/HTML template
  language with a fixed safe function set; email bodies are sent as HTML with
  a plain-text alternative derived from them.
- "Everyone in the tenant" resolves active members from the authentication
  service at publish time; membership changes afterwards do not alter
  existing deliveries.
- Notification log entries are retained for 400 days, live events for the
  replay window only (five minutes, bounded per person); no retention policy
  is applied to messages beyond archive/delete by the sender.
- Failed deliveries are not retried automatically; senders (or modules)
  decide whether to send again.
- Default API permissions per built-in role: owner/admin all; member
  `notifications:send`, `notifications:read` (own sends), `messages:read`,
  `inbox:read`, `templates:read`, `channels:read`; auditor/operator
  `stats:read`, `notifications:read`. Modules hold `notifications:send` and
  `events:publish` for their tenant.
- Sends are limited to 600 per minute per tenant and 60 per minute per
  sender by default (configurable); live streams to 5 per person.
- The reference project's mTLS "client" subject type for grants is covered by
  modules acting through the tenant relation; grants to modules by name are
  out of scope.
- The development environment uses the existing mail catcher of the platform
  stack for email delivery in tests.
