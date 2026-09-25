// Package notifyclient is the Go client other platform services use to send
// notifications and publish live events through the notification module
// over the Freya channel (contracts/notification.v1.proto).
package notifyclient

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
)

// Service is the discovery name of the notification module.
const Service = "notification"

// DefaultTimeout bounds one Send call (rendering plus one relay session).
const DefaultTimeout = 60 * time.Second

// Client wraps the two gRPC services.
type Client struct {
	notifier notificationv1.NotifierClient
	events   notificationv1.EventsClient
	timeout  time.Duration
}

// New wraps a connection obtained from Freya.Client(ctx, Service).
func New(conn grpc.ClientConnInterface) *Client {
	return &Client{notifier: notificationv1.NewNotifierClient(conn), events: notificationv1.NewEventsClient(conn), timeout: DefaultTimeout}
}

// Send renders a template and delivers it for the tenant.
func (c *Client) Send(ctx context.Context, tenantID, templateID, recipient string, variables map[string]string) (*notificationv1.SendResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.notifier.Send(ctx, &notificationv1.SendRequest{TenantId: tenantID, TemplateId: templateID, Recipient: recipient, Variables: variables})
}

// Result is the outcome of SendKey.
type Result struct {
	LogID     string // notification log entry, when one was written
	Sent      bool
	Retryable bool   // !Sent only: a later attempt may succeed
	Reason    string // scrubbed failure reason (no secret variable values)
}

// SendKey sends a system template (key such as "auth.invite") for the
// tenant. The key must belong to the calling service's namespace. Outcomes a
// later retry can fix (notification unavailable, deadline, throttled, relay
// temporarily failing) come back as Result{Retryable: true} with a nil error;
// permanent refusals (unknown key, foreign namespace, missing variable, email
// not configured) are returned as the gRPC error.
func (c *Client) SendKey(ctx context.Context, tenantID, key, recipient string, variables map[string]string, correlationID string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	res, err := c.notifier.Send(ctx, &notificationv1.SendRequest{
		TenantId: tenantID, TemplateKey: key, Recipient: recipient, Variables: variables, CorrelationId: correlationID,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
			return Result{Retryable: true, Reason: status.Code(err).String()}, nil
		}
		return Result{}, err
	}
	out := Result{LogID: res.GetLogId(), Reason: res.GetError()}
	switch res.GetStatus() {
	case notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT:
		out.Sent = true
	case notificationv1.DeliveryStatus_DELIVERY_STATUS_FAILED:
		out.Retryable = res.GetRetryable()
	default: // pending or unspecified: not confirmed, try again later
		out.Retryable = true
	}
	return out, nil
}

// SendTest delivers the built-in test message through a channel.
func (c *Client) SendTest(ctx context.Context, tenantID, channelID, recipient string) (*notificationv1.SendResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.notifier.SendTest(ctx, &notificationv1.SendTestRequest{TenantId: tenantID, ChannelId: channelID, Recipient: recipient})
}

// Publish pushes a live event (JSON-serialisable data) to users of the tenant.
func (c *Client) Publish(ctx context.Context, tenantID string, userIDs []string, all bool, eventType string, data any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := c.events.Publish(ctx, &notificationv1.PublishRequest{TenantId: tenantID, UserIds: userIDs, All: all, Type: eventType, Data: raw})
	if err != nil {
		return "", err
	}
	return res.GetEventId(), nil
}
