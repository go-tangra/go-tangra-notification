// Package notifyclient is the Go client other platform services use to send
// notifications and publish live events through the notification module
// over the Freya channel (contracts/notification.v1.proto).
package notifyclient

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/grpc"

	notificationv1 "github.com/go-tangra/go-tangra-notification/v4/api/proto/notification/v1"
)

// Service is the discovery name of the notification module.
const Service = "notification"

// Client wraps the two gRPC services.
type Client struct {
	notifier notificationv1.NotifierClient
	events   notificationv1.EventsClient
	timeout  time.Duration
}

// New wraps a connection obtained from Freya.Client(ctx, Service).
func New(conn grpc.ClientConnInterface) *Client {
	return &Client{notifier: notificationv1.NewNotifierClient(conn), events: notificationv1.NewEventsClient(conn), timeout: 60 * time.Second}
}

// Send renders a template and delivers it for the tenant.
func (c *Client) Send(ctx context.Context, tenantID, templateID, recipient string, variables map[string]string) (*notificationv1.SendResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.notifier.Send(ctx, &notificationv1.SendRequest{TenantId: tenantID, TemplateId: templateID, Recipient: recipient, Variables: variables})
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
