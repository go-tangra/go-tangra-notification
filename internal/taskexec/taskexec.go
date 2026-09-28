// Package taskexec executes the notification module's scheduled task types
// for the scheduler module (feature 026, research D3/D11):
//
//   - notification:send-test-email — a plain-text test email through the
//     tenant's default (or chosen) email channel.
//
// The scheduler-v4 SDK server (pkg/taskexec) admits only the scheduler's
// verified SPIFFE identity and a UUID tenant; the handlers here decode the
// payload strictly, validate it again and scope every send to that tenant
// with the scheduler as the actor. Payload values are never logged.
package taskexec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/go-tangra/go-tangra-scheduler/sdk/v4/pkg/schedulerclient"
	sched "github.com/go-tangra/go-tangra-scheduler/sdk/v4/pkg/taskexec"

	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
	"github.com/go-tangra/go-tangra/v4/authn"
	"github.com/go-tangra/go-tangra/v4/identity"
)

// TypeSendTestEmail is the task type of the scheduled test email.
const TypeSendTestEmail = "notification:send-test-email"

// Payload bounds (mirrored in PayloadSchema).
const (
	maxSubject   = notify.MaxCustomSubject
	maxBody      = notify.MaxCustomBody
	maxRecipient = 320
)

// PayloadSchema is the JSON Schema of the send-test-email payload.
const PayloadSchema = `{"type":"object","additionalProperties":false,"required":["recipient"],"properties":{` +
	`"recipient":{"type":"string","format":"email","maxLength":320,"description":"Email address to deliver the test message to"},` +
	`"subject":{"type":"string","maxLength":200,"pattern":"^[^\\r\\n]*$","description":"Optional subject line (default: [GoTangra] Scheduled test email)"},` +
	`"body":{"type":"string","maxLength":10000,"description":"Optional plain-text body (default: a short text naming the execution and the time)"},` +
	`"channelId":{"type":"string","format":"uuid","description":"Optional email channel id; empty = the tenant's default email channel"}}}`

// Descriptors are the task types this module registers with the scheduler.
func Descriptors() []schedulerclient.Descriptor {
	return []schedulerclient.Descriptor{{
		Type:        TypeSendTestEmail,
		DisplayName: "Send test email",
		Description: "Send a test email through the notification module to verify that the tenant's email channel " +
			"(the default one, or the chosen channel) delivers end to end.",
		PayloadSchema:   PayloadSchema,
		DefaultCron:     "",
		DefaultMaxRetry: 1,
	}}
}

// Sender is the part of notify.Sender the executor uses.
type Sender interface {
	SendCustom(ctx context.Context, subj authz.Subjects, in notify.CustomInput) (notify.LogView, error)
}

// Executor runs the module's task types.
type Executor struct {
	sender    Sender
	scheduler string // service name of the scheduler
	actor     string // SPIFFE id of the scheduler (the audited actor)
}

// New builds the executor; trustDomain and schedulerService name the
// scheduler's identity.
func New(s Sender, trustDomain, schedulerService string) (*Executor, error) {
	if s == nil {
		return nil, errors.New("taskexec: sender is required")
	}
	id, err := identity.NewSPIFFEID(trustDomain, schedulerService)
	if err != nil {
		return nil, fmt.Errorf("taskexec: scheduler identity: %w", err)
	}
	return &Executor{sender: s, scheduler: schedulerService, actor: id.String()}, nil
}

// Handlers maps each task type to its handler.
func (e *Executor) Handlers() map[string]sched.Handler {
	return map[string]sched.Handler{TypeSendTestEmail: e.sendTestEmail}
}

// Server is the scheduler.v1.TaskExecutor server over the handlers.
func (e *Executor) Server(caller func(context.Context) (string, bool), log *slog.Logger) *sched.Server {
	return sched.NewServer(e.Handlers(), sched.Options{Caller: caller, Scheduler: e.scheduler, Log: log})
}

// Caller resolves the verified peer's service name, for peers of the own
// trust domain only.
func Caller(trustDomain string) func(context.Context) (string, bool) {
	return func(ctx context.Context) (string, bool) {
		p, ok := authn.FromContext(ctx)
		if !ok || p.ID.TrustDomain() != trustDomain {
			return "", false
		}
		return p.ID.ServiceName(), true
	}
}

// testEmail is the send-test-email payload.
type testEmail struct {
	Recipient string `json:"recipient"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	ChannelID string `json:"channelId"`
}

func (e *Executor) sendTestEmail(ctx context.Context, req sched.Request) sched.Result {
	if !sched.ValidTenant(req.TenantID) {
		return sched.Permanent("tenant is required")
	}
	var p testEmail
	if err := sched.DecodeStrict(req.Payload, &p); err != nil {
		return sched.Permanent(err.Error())
	}
	recipient, msg := validate(p)
	if msg != "" {
		return sched.Permanent("invalid payload: " + msg)
	}
	v, err := e.sender.SendCustom(ctx, authz.ServiceSubjects(req.TenantID, e.actor), notify.CustomInput{
		ChannelID: p.ChannelID, Recipient: recipient, Subject: p.Subject, Body: p.Body, CorrelationID: req.ExecutionID,
	})
	if err != nil {
		return outcome(err)
	}
	if v.Status != "sent" {
		m := "delivery via channel " + v.ChannelID + " failed: " + v.Error
		if v.Retryable {
			return sched.Retry(m)
		}
		return sched.Permanent(m)
	}
	r := sched.OK("Test email sent to " + recipient + " via channel " + v.ChannelID)
	r.Data = map[string]string{"log_id": v.ID, "channel_id": v.ChannelID}
	return r
}

// validate checks the payload and returns the bare recipient address, or a
// message naming the problem (never a value).
func validate(p testEmail) (string, string) {
	if p.Recipient == "" {
		return "", "recipient is required"
	}
	a, err := mail.ParseAddress(p.Recipient)
	if err != nil || len(p.Recipient) > maxRecipient {
		return "", "recipient must be one email address"
	}
	if p.ChannelID != "" && !sched.ValidTenant(p.ChannelID) {
		return "", "channelId must be a UUID"
	}
	if utf8.RuneCountInString(p.Subject) > maxSubject || strings.ContainsAny(p.Subject, "\r\n") {
		return "", "subject must be one line of at most 200 characters"
	}
	if utf8.RuneCountInString(p.Body) > maxBody {
		return "", "body must be at most 10000 characters"
	}
	return a.Address, ""
}

// outcome maps a refused send: configuration and input problems are
// permanent, everything else is retried.
func outcome(err error) sched.Result {
	var ve *notify.ValidationError
	switch {
	case errors.As(err, &ve):
		return sched.Permanent("invalid request: " + ve.Msg)
	case errors.Is(err, store.ErrNotFound):
		return sched.Permanent("channel not found in the tenant")
	case errors.Is(err, notify.ErrTypeMismatch):
		return sched.Permanent("the channel is not an email channel")
	case errors.Is(err, notify.ErrChannelDisabled):
		return sched.Permanent("the email channel is disabled")
	case errors.Is(err, notify.ErrEmailNotConfigured):
		return sched.Permanent("the tenant has no default email channel; create one in the notification module first")
	case errors.Is(err, notify.ErrRateLimited):
		return sched.Retry("send rate limit reached; retrying later")
	}
	return sched.Retry("notification is temporarily unavailable")
}
