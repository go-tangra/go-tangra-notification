# Live stream contract (feature 006)

`GET /api/notification/v1/stream` (permission `inbox:read`, relayed by the
gateway like any module route; research R4).

## Response

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-store
X-Accel-Buffering: no

retry: 3000
: connected <instance-id>

: ping                                   ← every 15 s

id: 1758100000000-0                      ← Valkey stream id of the event
event: inbox
data: {"message_id":"…","title":"Downtime tonight","category":"Maintenance","unread":3}

id: 1758100004000-0
event: inbox.revoked
data: {"message_id":"…","unread":2}

id: 1758100010000-0
event: warden.import.finished           ← module events: type as published
data: {"folder":"Imported","created":120}

event: reset                             ← Last-Event-ID older than the replay window
data: {"reason":"replay_window"}
```

- The module closes the response after 290 s (`event: bye` + EOF); the
  browser's `EventSource` reconnects with `Last-Event-ID` after `retry`.
- Events are delivered only when `to` contains the stream's user id or is
  `*` (everyone in the tenant). `data` is the publisher's JSON, ≤ 16 KiB.
- On reconnect with `Last-Event-ID`, missed events inside the replay window
  (5 minutes) are replayed in order before live events; an id older than the
  window yields `event: reset` and the client refreshes its inbox
  (`GET /inbox/unread` + list).
- Limits: 5 open streams per person (the sixth gets `429 rate_limited`),
  2,000 per tenant per instance, 1 KiB write buffer — a client that stops
  reading is dropped and reconnects.
- Sign-out: the gateway stops relaying (the platform token is revoked); the
  module also ends a stream when its token expires (`event: bye`).

## Client (remote `stores/live.ts`)

One `EventSource('/api/notification/v1/stream', { withCredentials: true })`
per browser tab, created by the header component and shared through the
Pinia store with the inbox view; handlers: `inbox` → increment unread, prepend
to the badge menu; `inbox.revoked` → set unread; `reset` → refetch; any other
type → `window.dispatchEvent(new CustomEvent('freya:live', { detail: {type,
data} }))` so other remotes can listen without depending on this module.
