package channel

import "context"

// Message represents a rendered notification ready to send.
type Message struct {
	Recipient string
	Subject   string
	Body      string
}

// Sender is the interface all notification channels must implement.
type Sender interface {
	// Send delivers a message to the recipient.
	Send(ctx context.Context, msg *Message) error

	// Type returns the channel type identifier.
	Type() string
}
