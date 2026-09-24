// Package channel defines delivery providers by channel type. Only email has
// a provider today; sms, slack and sse are accepted for configuration and
// report ErrNoProvider when a send is attempted (spec assumptions).
package channel

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/channel/email"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
)

// Channel types.
const (
	TypeEmail = "email"
	TypeSMS   = "sms"
	TypeSlack = "slack"
	TypeSSE   = "sse"
)

// Types lists every accepted type.
var Types = []string{TypeEmail, TypeSMS, TypeSlack, TypeSSE}

// ValidType reports whether t is an accepted channel type.
func ValidType(t string) bool {
	for _, x := range Types {
		if x == t {
			return true
		}
	}
	return false
}

// Errors.
var (
	ErrNoProvider = errors.New("channel: no provider for this type")
	ErrRecipient  = errors.New("channel: invalid recipient")
	ErrType       = errors.New("channel: unknown type")
)

// Message is one rendered message to deliver.
type Message = email.Message

// Provider delivers messages of one channel type.
type Provider interface {
	Type() string
	// Validate checks settings for the type (called at save time).
	Validate(settings sealed.Settings) error
	// Secret lists the credential fields of the settings.
	Secret() []string
	// Send delivers; the error may be shown to administrators after Scrub.
	Send(ctx context.Context, settings sealed.Settings, msg Message) error
}

// Options configure the built-in providers.
type Options struct {
	AllowPlaintext bool
	DialTimeout    time.Duration
}

// Registry maps types to providers.
type Registry struct{ providers map[string]Provider }

// NewRegistry builds a registry from providers.
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{providers: map[string]Provider{}}
	for _, p := range ps {
		r.providers[p.Type()] = p
	}
	return r
}

// Builtin returns the email provider plus placeholders for the other types.
func Builtin(o Options) *Registry {
	return NewRegistry(&email.Provider{AllowPlaintext: o.AllowPlaintext, DialTimeout: o.DialTimeout}, Nop{Kind: TypeSMS}, Nop{Kind: TypeSlack}, Nop{Kind: TypeSSE})
}

// Get returns the provider of a type.
func (r *Registry) Get(typ string) (Provider, error) {
	p, ok := r.providers[typ]
	if !ok {
		return nil, ErrType
	}
	return p, nil
}

// SecretFields lists the credential fields of a type ("" when unknown).
func (r *Registry) SecretFields(typ string) []string {
	if p, ok := r.providers[typ]; ok {
		return p.Secret()
	}
	return nil
}

// ValidateRecipient checks a recipient for the type: a single address for
// email, otherwise a bounded printable string. Line breaks are never allowed.
func ValidateRecipient(typ, recipient string) error {
	if recipient == "" || len(recipient) > 512 || strings.ContainsAny(recipient, "\r\n\x00") {
		return ErrRecipient
	}
	if typ == TypeEmail {
		if _, err := mail.ParseAddress(recipient); err != nil {
			return ErrRecipient
		}
	}
	return nil
}

// Nop is a configurable type without a provider.
type Nop struct{ Kind string }

// Type implements Provider.
func (n Nop) Type() string { return n.Kind }

// Validate accepts any object of at most 8 KiB (checked by sealed).
func (n Nop) Validate(sealed.Settings) error { return nil }

// Secret lists the credential fields these providers will carry.
func (n Nop) Secret() []string { return []string{"api_key", "token", "password"} }

// Send reports that no provider exists.
func (n Nop) Send(context.Context, sealed.Settings, Message) error {
	return fmt.Errorf("%w: %s", ErrNoProvider, n.Kind)
}
