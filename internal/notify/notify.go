// Package notify holds the notification services: channels with sealed
// provider settings, templates validated at save time, and the send
// pipeline that renders, delivers and logs (research R8).
package notify

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Errors.
var (
	ErrChannelDisabled = errors.New("notify: channel disabled")
	ErrRateLimited     = errors.New("notify: rate limited")
	ErrTypeMismatch    = errors.New("notify: channel type does not match the template")
	// System template sends (feature 017).
	ErrKeyNamespace       = errors.New("notify: template key outside the caller's namespace")
	ErrUnknownKey         = errors.New("notify: unknown template key")
	ErrEmailNotConfigured = errors.New("notify: email not configured")
)

// ValidationError carries a client-safe detail object (positions, names).
type ValidationError struct {
	Msg    string
	Detail map[string]any
}

func (e *ValidationError) Error() string { return "notify: " + e.Msg }

func invalid(msg string, detail map[string]any) error {
	return &ValidationError{Msg: msg, Detail: detail}
}

// InUseError refuses a deletion while dependants exist.
type InUseError struct {
	What  string
	Count int
}

func (e *InUseError) Error() string {
	return fmt.Sprintf("notify: %d %s reference it", e.Count, e.What)
}

// Limits bound sends per minute (0 = unlimited).
type Limits struct {
	PerTenant int
	PerSender int
	System    int // system template sends per calling service
}

func strp(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func userPtr(s authz.Subjects) *string {
	if s.UserID == "" {
		return nil
	}
	u := s.UserID
	return &u
}

// LogView is a log entry as returned to clients (body only on single reads).
type LogView struct {
	ID              string     `json:"id"`
	ChannelID       string     `json:"channel_id"`
	ChannelType     string     `json:"channel_type"`
	TemplateID      *string    `json:"template_id"`
	TemplateKey     *string    `json:"template_key"`
	Recipient       string     `json:"recipient"`
	RenderedSubject string     `json:"rendered_subject"`
	RenderedBody    string     `json:"rendered_body,omitempty"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty"`
	SenderKind      string     `json:"sender_kind"`
	SenderID        string     `json:"sender_id"`
	Test            bool       `json:"test"`
	CreatedAt       time.Time  `json:"created_at"`
	SentAt          *time.Time `json:"sent_at"`
	// Retryable reports, for a failed delivery, whether a later attempt may
	// succeed (service callers decide on retries; not part of the API body).
	Retryable bool `json:"-"`
}

func logView(l store.LogRow) LogView {
	return LogView{ID: l.ID, ChannelID: l.ChannelID, ChannelType: l.ChannelType, TemplateID: l.TemplateID, TemplateKey: l.TemplateKey, Recipient: l.Recipient, RenderedSubject: l.RenderedSubject,
		RenderedBody: l.RenderedBody, Status: l.Status, Error: l.Error, SenderKind: l.SenderKind, SenderID: l.SenderID, Test: l.Test, CreatedAt: l.CreatedAt, SentAt: l.SentAt}
}
