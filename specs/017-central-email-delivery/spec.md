# Feature Specification: Central Email Delivery Through the Notification Module

**Feature Branch**: `017-central-email-delivery`

**Created**: 2026-09-25

**Status**: Draft

**Input**: User description: "Central outbound email through the notification module (spec 017). notification bootstraps one platform-wide default SMTP channel from its configuration at startup (create or update; password via secret file or warden reference; TLS required unless explicitly opted out) and built-in system templates with fixed keys (auth.invite, auth.recovery, warden.share) resolvable by key, editable later in the UI. Sends for a tenant without its own email channel fall back to the platform default channel. auth's outbox worker and warden's share-link mail send through notification.v1.Notifier/Send over the mTLS mesh instead of their own SMTP settings (auth keeps its outbox queue and retries); their email/mail SMTP config blocks are retired. Invite/recovery links are one-time secrets: system-template variables marked secret are redacted in notification's delivery log and audit. notification gets a nested sdk module (proto + client) so auth can depend on it without a module cycle. ticket keeps its own SMTP (threading headers, per-queue sender, attachments) and is out of scope. go-tangra-docker production configs and PRODUCTION.md move SMTP settings to notification only. Repos: go-tangra-notification (primary, spec lives here), go-tangra-auth, go-tangra-warden, go-tangra-docker."

**Repositories**: go-tangra-notification (primary; this spec), go-tangra-auth,
go-tangra-warden, go-tangra-docker. go-tangra-ticket is out of scope.

## Context

Today three platform services each hold their own mail relay settings: auth
(invitations, account recovery), warden (share links) and notification (its
own channels). An operator who installs or changes the mail relay has to edit
every one of them, and a mistake in one (wrong port, wrong host name for the
relay's certificate) shows up only as silently missing mail from that
service. The notification module already owns email channels, templates, a
delivery log and a service-to-service send interface. This feature makes it
the single place where the platform's outbound email is configured and sent.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One mail relay setting for the whole platform (Priority: P1)

An operator installing the platform enters the mail relay (host, port,
transport security, optional login, sender address) once, in the
notification module's configuration. When the platform starts, the
notification module has a working platform-wide email channel, and every
platform email (invitations, account recovery, share links) goes out through
it. Changing the relay later means editing that one setting and restarting
the notification module.

**Why this priority**: This is the operational pain that motivated the
feature: today a wrong relay setting has to be found and fixed in three
places, and a first-time install cannot even invite its first operator when
one of them is wrong.

**Independent Test**: Configure only the notification module's relay setting,
start the platform, and send a test message through the platform default
channel; it arrives. Change the relay setting, restart notification only, and
the next test message goes through the new relay.

**Acceptance Scenarios**:

1. **Given** a fresh installation with a relay configured only in the
   notification module, **When** the platform starts, **Then** a platform
   default email channel exists, is enabled, is marked as managed by
   configuration, and a test send through it succeeds.
2. **Given** a running platform, **When** the operator changes the relay host
   or port in the configuration and restarts the notification module,
   **Then** the platform default channel reflects the new setting and no
   other service needs a change or restart.
3. **Given** a relay setting without transport security, **When** the
   notification module starts without the explicit plaintext opt-out,
   **Then** it refuses to start and names the setting.
4. **Given** the explicit plaintext opt-out, **When** the notification
   module starts, **Then** it starts and logs a warning naming the opt-out.
5. **Given** a relay password supplied as a secret reference, **When** the
   configuration is read, **Then** the password is never present in the
   configuration file, logs, channel reads or audit events.

---

### User Story 2 - Invitations and recovery mail from auth arrive (Priority: P1)

An administrator invites a user, activates an imported directory user, or a
user requests account recovery. auth queues the message as today, and its
delivery worker hands it to the notification module instead of talking to a
mail relay itself. The recipient receives the email with a working link.
Nobody reading the notification delivery log can use that link.

**Why this priority**: Invitations are how every account on the platform
starts, including the first operator. If they fail, the platform cannot be
used.

**Independent Test**: With auth configured without any mail relay, invite a
user; the email arrives through the notification module, the link works
once, and the delivery log entry shows the send without the link.

**Acceptance Scenarios**:

1. **Given** auth without its own relay settings, **When** an administrator
   invites a user, **Then** the invitation email is delivered through the
   notification module and the log entry records recipient, template,
   status and time.
2. **Given** a delivered invitation, **When** anyone with read access opens
   the notification delivery log entry, **Then** the invitation link and its
   token are shown only as a redaction marker, in both subject and body.
3. **Given** the notification module is unreachable or the relay refuses the
   message, **When** auth's worker tries to deliver, **Then** the message
   stays queued in auth and is retried with backoff until it is delivered or
   the retry limit is reached; nothing is lost because notification was down.
4. **Given** a message that reached the retry limit, **When** the worker runs
   again, **Then** it is not attempted again and is reported once as given
   up, instead of being logged on every pass.
5. **Given** an invitation for a user of any tenant, **When** that tenant has
   no email channel of its own, **Then** the platform default channel is
   used; **When** the tenant has its own default email channel, **Then** that
   channel is used.

---

### User Story 3 - Share links from warden arrive (Priority: P2)

A user shares a secret from warden with an external recipient. warden asks
the notification module to send the share link instead of using its own
relay settings. The link is redacted in the delivery log exactly like auth's
links.

**Why this priority**: Share-link mail matters, but fewer flows depend on it
than on invitations, and the rest of warden works without mail.

**Independent Test**: With warden configured without any mail relay, create
a share addressed to an email recipient; the email arrives through the
notification module and the log entry does not reveal the link.

**Acceptance Scenarios**:

1. **Given** warden without its own relay settings, **When** a user shares a
   secret with an email recipient, **Then** the share email is delivered
   through the notification module.
2. **Given** a delivered share email, **When** the delivery log entry is
   read, **Then** the share link is redacted.
3. **Given** the notification module cannot deliver, **When** the share is
   created, **Then** the user is told the email could not be sent and the
   share is cancelled (the link is only ever mailed, never shown, so an
   unsent share could not be opened by anyone).

---

### User Story 4 - Operators adjust the wording of platform emails (Priority: P3)

A platform operator opens the notification module's templates and finds the
built-in system templates (invitation, account recovery, share link). They
change the wording or add their organisation's name. The next invitation
uses the new wording. Upgrading or restarting the platform does not undo
their change.

**Why this priority**: Useful for branding and language, but the built-in
wording is sufficient to run the platform.

**Independent Test**: Edit the invitation system template's body, invite a
user, and the email carries the edited text; restart notification and the
edit is still there.

**Acceptance Scenarios**:

1. **Given** a fresh installation, **When** notification starts, **Then** the
   system templates exist with built-in wording, each identified by a fixed
   key.
2. **Given** an operator edited a system template, **When** notification
   restarts or is upgraded, **Then** the edited template is kept, not
   replaced by the built-in wording.
3. **Given** an operator removes a variable a system template needs (for
   example the link), **When** they save, **Then** the save is refused and
   names the required variable.
4. **Given** a system template, **When** an operator tries to delete it,
   **Then** the deletion is refused; the operator may restore the built-in
   wording instead.

---

### Edge Cases

- The relay setting is missing entirely: notification starts without a
  platform default channel and logs that platform email is disabled; auth and
  warden keep their mail queued (auth) or report "email not sent" (warden)
  instead of failing requests.
- An operator edits the configuration-managed default channel in the UI: the
  edit is refused with a message that the channel is managed by
  configuration.
- A tenant disables its own default email channel: sends for that tenant fall
  back to the platform default channel.
- A bulk activation of many directory users exceeds the notification send
  rate for one caller: sends beyond the limit are refused as throttled and
  auth retries them later; every invitation is eventually delivered.
- The relay's certificate does not match the configured host (for example a
  bare IP address): sends fail with a reason that names the certificate
  mismatch, and the test send in the UI reports the same.
- An existing installation still has the old relay settings in auth or warden
  configuration: they start, ignore those settings and log a warning that
  names them and says email now goes through notification.
- A caller asks for a system template key that does not exist or belongs to
  another service (warden asking for an auth template): refused.
- The same message is handed to notification twice after a timeout (auth did
  not see the answer): the recipient may receive it twice; the link inside is
  still single-use.

## Requirements *(mandatory)*

### Functional Requirements

**Platform default channel (notification)**

- **FR-001**: The notification module MUST read an optional platform email
  setting from its configuration: relay host, port, transport security
  (implicit TLS, STARTTLS, or none), optional username, password as a secret
  reference, sender address, optional reply-to, and an explicit plaintext
  opt-out.
- **FR-002**: At every start, when the setting is present, the notification
  module MUST create the platform default email channel in the platform
  tenant if it does not exist, or update it to match the configuration if it
  does; the channel MUST be marked as managed by configuration.
- **FR-003**: Transport security "none" MUST be refused at start unless the
  explicit plaintext opt-out is set; when accepted it MUST be logged as a
  warning at every start. A username with transport security "none" MUST be
  refused (credentials are never sent unencrypted).
- **FR-004**: The relay password MUST be accepted only as a secret reference
  (mounted secret file or secrets-manager reference), resolved at start; a
  literal password in the configuration MUST be refused.
- **FR-005**: The configuration-managed channel MUST be readable in the UI
  (credentials shown only as "set") and MUST NOT be editable or deletable
  there; a test send through it MUST be possible.

**System templates (notification)**

- **FR-006**: At every start the notification module MUST ensure system
  templates exist for the keys `auth.invite` (invitations, including the
  first operator's), `auth.account_reset` (an administrator reset an
  account), `auth.recovery` (password reset), `auth.message` (messages queued
  by an older auth release, delivered verbatim) and `warden.share`, each with
  built-in subject and body, its declared variables, and the variables that
  are secret marked as such.
- **FR-007**: An existing system template MUST NOT be overwritten at start;
  operators MUST be able to edit its subject and body and restore the
  built-in wording.
- **FR-008**: Saving a system template MUST be refused when it no longer
  references a variable that the owning service always supplies and the
  template requires (for example the link); deleting a system template MUST
  be refused.
- **FR-009**: Callers MUST be able to send by system template key instead of
  template id. Each calling service MAY use only the keys in its own
  namespace (auth: `auth.*`, warden: `warden.*`).

**Sending and channel resolution (notification)**

- **FR-010**: A send by system template key for a tenant MUST use that
  tenant's enabled default email channel when one exists, otherwise the
  platform default channel; when neither exists the send MUST fail with a
  distinct "email not configured" reason.
- **FR-011**: Sends by system template key MUST be allowed for any tenant on
  behalf of the calling service without per-tenant grants on the platform
  channel or the system template; ordinary template sends keep the existing
  grant rules.
- **FR-012**: The send answer MUST distinguish delivered, failed with a
  retryable reason (relay unreachable, temporary refusal, throttled) and
  failed permanently (invalid recipient, template missing, email not
  configured), so callers can decide whether to retry.

**Redaction (notification)**

- **FR-013**: Values of variables marked secret MUST be replaced by a
  redaction marker wherever they appear in the stored subject and body of
  the delivery log entry, and MUST NOT appear in audit events, logs or error
  messages; the recipient still receives the unredacted message.
- **FR-014**: Delivery log entries of system template sends MUST still record
  recipient, template key, channel, status, failure reason and times.

**auth**

- **FR-015**: auth's delivery worker MUST send invitation and recovery
  messages through the notification module by system template key, passing
  the link as a secret variable, and MUST keep its queue: a message is
  marked sent only when notification reports it delivered.
- **FR-016**: auth MUST retry retryable failures with backoff up to its retry
  limit and MUST NOT retry permanent failures; a message that reaches the
  retry limit or fails permanently MUST be taken out of the queue and
  reported once (log and audit), not on every pass.
- **FR-017**: auth MUST no longer require or use its own relay settings;
  existing relay settings in its configuration MUST be accepted, ignored and
  reported by a start-up warning for the 4.x releases.

**warden**

- **FR-018**: warden MUST send share-link emails through the notification
  module by system template key, passing the link as a secret variable; a
  failed send MUST cancel the share and be reported to the user, as today.
- **FR-019**: warden MUST no longer require or use its own relay settings;
  existing settings MUST be accepted, ignored and reported by a start-up
  warning for the 4.x releases.

**Packaging and deployment**

- **FR-020**: The notification module MUST publish its service-to-service
  interface and client as a separately versioned module, so auth and warden
  can depend on it without depending on the notification service itself.
- **FR-021**: The production deployment files MUST configure the mail relay
  only in the notification module (including the secret file for the relay
  password), and the production guide MUST describe that single setting, the
  certificate host-name requirement, and how to verify delivery.
- **FR-022**: ticket's own outbound mail settings MUST remain unchanged.

### Security Requirements *(mandatory — Constitution: Development Workflow)*

- **Trust boundaries crossed**: service-to-service calls on the mesh (auth
  and warden to notification); outbound connection from notification to the
  mail relay; configuration and secret files read at start.
- **Data classification**: one-time secrets (invitation, recovery and share
  links with their tokens); credentials (relay password); PII (recipient
  addresses, names in message text).
- **Authentication/Authorization**: callers are identified by their mesh
  identity; the notification policy allows only auth and warden to send by
  system template key, each only within its own key namespace; UI access to
  channels, templates and the log keeps the existing grant rules.
- **Threat scenarios**: link disclosure through the delivery log, audit,
  logs or error text; relay credential disclosure through configuration,
  reads or logs; a compromised or misconfigured service sending another
  service's system template (phishing with a legitimate template); credentials
  or links sent in clear text to the relay; mail flooding through a bulk
  operation; a silent downgrade from TLS to plaintext.
- **SR-001**: Secret variable values MUST be redacted from stored log
  entries, audit events, logs and error messages (FR-013); tests MUST prove a
  link never appears in any stored or logged artefact.
- **SR-002**: A service MUST NOT be able to send a system template outside its
  own key namespace; the refusal MUST be audited.
- **SR-003**: The relay password MUST come from a secret reference, MUST be
  stored encrypted like other channel credentials, and MUST never be
  returned, logged or audited.
- **SR-004**: With transport security STARTTLS the connection MUST fail if
  the relay does not offer STARTTLS; there is no silent fallback to
  plaintext, and the relay certificate MUST be verified against the
  configured host name.
- **SR-005**: System template sends MUST be rate-limited per calling service;
  throttled sends MUST be reported as retryable.

### Key Entities *(include if feature involves data)*

- **Platform Default Channel**: the platform tenant's email channel created
  from configuration; relay host, port, transport security, sender, optional
  reply-to, optional username, password (encrypted), plaintext opt-out,
  "managed by configuration" marker.
- **System Template**: a template identified by a fixed key and owning
  service namespace; built-in subject and body, declared variables, required
  variables, secret variables, "edited" state and the built-in wording it can
  be restored to.
- **Delivery Log Entry** (existing, extended): records the template key for
  system sends and stores subject and body with secret values redacted.
- **Queued Message** (auth, existing): recipient, message kind, encrypted
  content, attempts, next attempt, sent time; gains a final "given up" state.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator configures outbound email for the whole platform in
  exactly one place; auth and warden start and deliver mail with no relay
  settings of their own.
- **SC-002**: After a relay change in that one place and a restart of the
  notification module, the next invitation is delivered through the new
  relay within 1 minute, with no change to any other service.
- **SC-003**: 100 % of delivery log entries, audit events and log lines for
  invitation, recovery and share sends are free of the link and its token
  (verified by automated tests over every system template).
- **SC-004**: When the notification module is down for up to 1 hour, every
  invitation queued in that time is delivered after it comes back, with none
  lost.
- **SC-005**: Activating 500 imported users at once results in 500 delivered
  invitations, with throttled sends retried rather than dropped.
- **SC-006**: A message that can never be delivered is reported once when
  given up, instead of once per worker pass.

## Assumptions

- The platform tenant already exists (created by auth's bootstrap) and is the
  owner of the platform default channel and of the system templates.
- The mesh identities and policy mechanism of the platform are used for the
  new calls; no new authentication mechanism is introduced.
- Relay passwords are supplied as a mounted secret file in the compose
  deployment; secrets-manager references are also accepted where the
  notification module can resolve them at start.
- Old relay settings in auth and warden configurations are ignored with a
  warning throughout 4.x and removed in the next major version.
- A tenant's own default email channel (spec 006) keeps working and takes
  precedence for that tenant's system mail.
- ticket keeps its own relay settings; its helpdesk replies need threading
  headers, per-queue senders and attachments that template sends do not cover.
- Built-in wording is English; operators translate by editing the system
  templates.
- Duplicate delivery after a lost answer is acceptable; the links inside are
  single-use.
