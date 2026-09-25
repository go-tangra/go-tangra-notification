# Contract: notification.v1 service-to-service changes (sdk/v4.2.0)

Module: `github.com/go-tangra/go-tangra-notification/sdk/v4`
(`api/proto/notification/v1`, `pkg/notifyclient`). Backwards compatible:
new fields only; existing callers keep working.

```proto
message SendRequest {
  string tenant_id = 1;                 // uuid
  string template_id = 2;               // uuid; exactly one of template_id / template_key
  string channel_id = 3;                // uuid, optional override (id sends only)
  string recipient = 4;
  map<string, string> variables = 5;
  string correlation_id = 6;
  string template_key = 7;              // NEW: "^[a-z][a-z0-9]*\.[a-z][a-z0-9_]{0,62}$"
}

message SendResponse {
  string log_id = 1;
  DeliveryStatus status = 2;
  string error = 3;                     // scrubbed; secret variable values removed
  google.protobuf.Timestamp sent_at = 4;
  bool retryable = 5;                   // NEW: meaningful when status = FAILED
}
```

## Rules for `template_key` sends

| Check | Failure |
|---|---|
| exactly one of `template_id`, `template_key` | `InvalidArgument template_ref` |
| `channel_id` must be empty | `InvalidArgument channel_override` |
| key prefix == caller service (`spiffe://<td>/svc/<name>` → `<name>.`) | `PermissionDenied key_namespace` (+ `access_refused` audit) |
| key exists in the platform tenant | `NotFound template_key` |
| every `required_variables` entry present | `InvalidArgument missing_variable:<name>` |
| channel: tenant default email channel (enabled) → platform managed channel | `FailedPrecondition email_not_configured` |
| `system:<service>` rate limit | `ResourceExhausted throttled` (retryable) |

No per-tenant `use` grant is checked for key sends.

Delivery failures are returned as `status=FAILED` with `retryable` set by
the classification in research D7; transport errors of the call itself
(`Unavailable`, `DeadlineExceeded`, `ResourceExhausted`) are retryable for the
caller.

## Client (`pkg/notifyclient`)

```go
func (c *Client) SendKey(ctx context.Context, tenantID, key, recipient string,
    vars map[string]string, correlationID string) (Result, error)

type Result struct {
    LogID     string
    Sent      bool
    Retryable bool   // for !Sent
    Reason    string // scrubbed
}
```

`SendKey` maps gRPC status codes: `Unavailable`, `DeadlineExceeded`,
`ResourceExhausted`, `Aborted` → `Result{Retryable: true}` with nil error;
other codes → error (permanent).

## Policy (`deploy/policy.yaml`, unchanged rule set)

`modules-send` already allows `svc/auth` and `svc/warden` on
`/notification.v1.Notifier/Send`; the key namespace check is enforced in
the handler, independent of policy.
