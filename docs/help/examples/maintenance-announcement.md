---
title: "Example: announce a maintenance window"
summary: Schedule a message to everyone in the tenant in a Maintenance category, check who read it, and withdraw it if the window is cancelled.
kind: example
order: 50
tags: [notification, example, messages, scheduling, categories]
requires: messages:manage
---
**Goal.** The mail server will be down on Saturday from 22:00 to 23:00.
Everyone should see a notice in the portal on Friday morning, and you want
to know how many people have read it.

**You need** *Notifications administrator* or *Notifications sender*.

## 1. Create the category

1. Open **Notifications → Categories** and click **New category**.
2. **Name**: `Maintenance`; **Description**: `Planned downtime`;
   **Sort**: `10`.
3. Save.

## 2. Schedule the message

1. Open **Notifications → Messages** and click **New message**.
2. Fill in:
   - **Title**: `Mail down Saturday 22:00–23:00`
   - **Content**:

     ```text
     The mail server will be unavailable on Saturday from 22:00 to 23:00
     for an upgrade. Mail sent in that hour is delivered afterwards.
     ```

   - **Type**: `notification`
   - **Category**: `Maintenance`
   - **Everyone in the tenant**: on
   - **Schedule (optional)**: Friday, 08:00
3. Click **Send**.

The message shows as `scheduled`. Until Friday 08:00 you can still open and
change it; saving keeps it scheduled.

## 3. Check who read it

After Friday 08:00 the status is `published`, **Recipients** reads
`everyone` and the **Read** column counts the people who opened it.

## 4. If the window is cancelled

Click **Revoke** on the message. It disappears from the inboxes of everyone
who has not read it yet. Then post a short correction the same way, or
**Archive** the revoked message to keep the list tidy.

If you cancel before Friday 08:00, click **Cancel** instead: the message
goes back to `draft` and nobody sees it.
