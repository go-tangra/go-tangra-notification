---
title: Send internal messages
summary: Write a message to some people or the whole tenant, send it now or schedule it, follow who read it, revoke or archive it, and read your own inbox.
kind: guide
order: 30
tags: [notification, messages, inbox, categories, scheduling]
requires: messages:manage
---
An internal message appears in the recipients' **Inbox** in the portal and
in the bell in the top bar. Nothing is emailed. Writing messages needs
`messages:manage` (*Notifications administrator* or *Notifications sender*).

## Write and send

1. Open **Notifications → Messages** and click **New message**.
2. Fill in:
   - **Title** (up to 300 characters) and **Content** (plain text, up to
     20,000 characters).
   - **Type**: `notification`, `private` or `group`, a label for the reader.
   - **Category** (optional), from **Notifications → Categories**.
   - **Everyone in the tenant**, or **Add recipient** to pick people by name
     (at least one).
   - **Schedule (optional)**: a future date and time.
3. Click **Send**, or **Save draft** to finish later.

Without a schedule the message is published at once. With a future
schedule it becomes `scheduled` and is published within seconds of that
time. Recipients who are no longer active in the tenant are dropped when the
message is published.

## Follow and change a message

The list shows each message's **Status**, **Category**, **Recipients**
(`everyone` or a count) and **Read** count. Filter by **Status**.

| Status | You can |
|---|---|
| `draft` | edit, **Send**, delete |
| `scheduled` | edit (saving with a future time keeps it scheduled), **Cancel** to turn it back into a draft |
| `published` | **Revoke**, **Archive** |
| `revoked` | **Archive**, delete |
| `archived` | delete |

- **Revoke** withdraws a published message: inbox entries not yet read are
  removed from the inboxes.
- Deleting a revoked or archived message also removes it from every inbox.

To publish a scheduled message right away, **Cancel** it, open the draft,
clear **Schedule** and click **Send**.

## Categories

**Notifications → Categories** lists categories with their **Sort** order
and how many messages use them. A category has a name (up to 120
characters), an optional description and a sort number. A category still
used by messages cannot be deleted.

## Your inbox

Everyone with `inbox:read` has an inbox at **Notifications → Inbox**:

- **All**, **Unread** and **Read** filter the list; unread entries are bold.
- Opening an entry shows the whole message and marks it read.
- **Delete** removes it from your inbox only.

The bell in the top bar shows the unread count and the newest entries. Both
update live while the portal is open; the inbox shows whether the live
connection is up.
