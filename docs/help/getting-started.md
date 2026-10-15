---
title: Get started
summary: Add an email channel for your tenant, send it a test message, then write a template and preview it.
kind: guide
order: 10
tags: [notification, getting-started, channels, email, templates]
requires: channels:manage
---
This walk-through sets up email for your tenant. You need the
*Notifications administrator* role (or `channels:manage` and
`templates:manage`) and the details of an SMTP relay.

## 1. Add an email channel

1. Open **Notifications → Channels** and click **New channel**.
2. Fill in:
   - **Name**: `Office mail`.
   - **Type**: `email` (cannot change later).
   - **SMTP host**: the relay's host name, the one on its certificate (an IP
     address fails the certificate check).
   - **Port**: `587` for STARTTLS, `465` for implicit TLS.
   - **TLS**: `starttls` (default), `implicit` or `none`. `none` is refused
     unless the module allows plaintext, which production does not.
   - **From**: the sender address, for example `noreply@example.com`.
   - **Reply-To (optional)**, **Username (optional)** and **Password**.
3. Switch on **Enabled**. Switch on **Default for this type** to make this
   the tenant's email channel: everyone in the tenant may then use it.
4. Click **Save**.

You own the new channel. The password is stored encrypted and shown only as
set; leave the field blank when editing to keep it.

## 2. Send a test message

Open the channel again. Under **Send a test message**, enter your address as
**Recipient** and click **Send test**. The drawer reports *Test message
sent.* or the relay's error. The test also appears in the
[log](guides/log.md) with a **test** badge.

## 3. Write a template

1. Open **Notifications → Templates** and click **New template**.
2. Fill in:
   - **Name**: `Welcome`.
   - **Channel**: `Office mail (email)`.
   - **Subject**: `Welcome, {{.name}}`.
   - **Body**: for email, a visual editor; use **Insert variable** to add
     `{{.name}}`.
   - **Declared variables (comma-separated)**: `name`.
3. Under **Preview**, enter a value for `name` and click **Preview**.
4. Click **Save**.

The template is ready for anyone you grant it to; see
[write templates](guides/templates.md).
