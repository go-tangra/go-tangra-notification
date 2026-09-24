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
}

func logView(l store.LogRow) LogView {
	return LogView{ID: l.ID, ChannelID: l.ChannelID, ChannelType: l.ChannelType, TemplateID: l.TemplateID, Recipient: l.Recipient, RenderedSubject: l.RenderedSubject,
		RenderedBody: l.RenderedBody, Status: l.Status, Error: l.Error, SenderKind: l.SenderKind, SenderID: l.SenderID, Test: l.Test, CreatedAt: l.CreatedAt, SentAt: l.SentAt}
}
