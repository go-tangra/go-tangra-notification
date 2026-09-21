package notify

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-freya/freya/services/notification/internal/audit"
	"github.com/go-freya/freya/services/notification/internal/authz"
	"github.com/go-freya/freya/services/notification/internal/channel"
	"github.com/go-freya/freya/services/notification/internal/render"
	"github.com/go-freya/freya/services/notification/internal/repo"
	"github.com/go-freya/freya/services/notification/internal/sealed"
	"github.com/go-freya/freya/services/notification/internal/store"
)

// Limiter counts sends per subject and minute (stream.Limiter).
type Limiter interface {
	Limited(ctx context.Context, kind, subject string, limit int, at time.Time) (bool, error)
}

// Sender is the send pipeline.
type Sender struct {
	st        repo.Store
	channels  *Channels
	templates *Templates
	az        *authz.Authz
	audit     *audit.Writer
	limiter   Limiter
	limits    Limits
	now       func() time.Time
}

// NewSender wires the pipeline.
func NewSender(st repo.Store, ch *Channels, tp *Templates, az *authz.Authz, aw *audit.Writer, l Limiter, limits Limits) *Sender {
	return &Sender{st: st, channels: ch, templates: tp, az: az, audit: aw, limiter: l, limits: limits, now: time.Now}
}

// SetClock injects the clock (tests).
func (s *Sender) SetClock(now func() time.Time) { s.now = now }

// SendInput is a send request.
type SendInput struct {
	TemplateID    string
	ChannelID     string // optional override
	Recipient     string
	Variables     map[string]string
	CorrelationID string
}

// MaxVariablesBytes bounds the serialised variables.
const MaxVariablesBytes = 64 << 10

// Send renders the template and delivers it (research R8). Delivery
// failures are reported in the returned entry, not as an error.
func (s *Sender) Send(ctx context.Context, subj authz.Subjects, in SendInput) (LogView, error) {
	if in.TemplateID == "" {
		return LogView{}, invalid("template_id is required", map[string]any{"field": "template_id"})
	}
	if err := checkVariables(in.Variables); err != nil {
		return LogView{}, err
	}
	if _, err := s.az.Require(ctx, subj, authz.Template, in.TemplateID, authz.Use); err != nil {
		return LogView{}, err
	}
	tpl, err := s.st.GetTemplate(ctx, subj.TenantID, in.TemplateID)
	if err != nil {
		return LogView{}, err
	}
	channelID := in.ChannelID
	if channelID == "" {
		channelID = strp(tpl.ChannelID)
	}
	if channelID == "" {
		return LogView{}, invalid("the template has no channel", map[string]any{"field": "channel_id"})
	}
	if _, err := s.az.Require(ctx, subj, authz.Channel, channelID, authz.Use); err != nil {
		return LogView{}, err
	}
	ch, settings, provider, err := s.channels.Resolve(ctx, subj.TenantID, channelID)
	if err != nil {
		return LogView{}, err
	}
	if ch.Type != tpl.ChannelType {
		return LogView{}, ErrTypeMismatch
	}
	if !ch.Enabled {
		return LogView{}, ErrChannelDisabled
	}
	if err := channel.ValidateRecipient(ch.Type, in.Recipient); err != nil {
		return LogView{}, invalid("recipient is not valid for the channel type", map[string]any{"field": "recipient"})
	}
	compiled, err := render.Parse(tpl.Subject, tpl.Body, KindFor(ch.Type))
	if err != nil {
		return LogView{}, validationError(err)
	}
	for _, r := range compiled.Refs {
		if _, ok := in.Variables[r]; !ok {
			return LogView{}, invalid("missing variable", map[string]any{"variable": r})
		}
	}
	if err := s.limit(ctx, subj); err != nil {
		return LogView{}, err
	}
	tid := tpl.ID
	return s.deliver(ctx, subj, ch, settings, provider, &tid, in.Recipient, false, in.CorrelationID, func(ctx context.Context) (string, string, error) {
		return compiled.Render(ctx, in.Variables)
	})
}

// testSubject / testBody are the built-in test message.
const (
	testSubject = "Freya notification test"
	testBody    = "<p>This is a test message from the Freya notification service. If you can read it, the channel works.</p>"
)

// SendTest delivers the built-in test message through a channel the caller
// may write (administrators checking their configuration).
func (s *Sender) SendTest(ctx context.Context, subj authz.Subjects, channelID, recipient, correlationID string) (LogView, error) {
	if _, err := s.az.Require(ctx, subj, authz.Channel, channelID, authz.Write); err != nil {
		return LogView{}, err
	}
	ch, settings, provider, err := s.channels.Resolve(ctx, subj.TenantID, channelID)
	if err != nil {
		return LogView{}, err
	}
	if err := channel.ValidateRecipient(ch.Type, recipient); err != nil {
		return LogView{}, invalid("recipient is not valid for the channel type", map[string]any{"field": "recipient"})
	}
	if err := s.limit(ctx, subj); err != nil {
		return LogView{}, err
	}
	return s.deliver(ctx, subj, ch, settings, provider, nil, recipient, true, correlationID, func(context.Context) (string, string, error) {
		return testSubject, testBody, nil
	})
}

func checkVariables(vars map[string]string) error {
	if len(vars) > 50 {
		return invalid("at most 50 variables", map[string]any{"field": "variables"})
	}
	total := 0
	for k, v := range vars {
		if !render.ValidName(k) {
			return invalid("variable name", map[string]any{"variable": k})
		}
		total += len(k) + len(v)
	}
	if total > MaxVariablesBytes {
		return invalid("variables exceed 64 KiB", map[string]any{"field": "variables"})
	}
	return nil
}

func (s *Sender) limit(ctx context.Context, subj authz.Subjects) error {
	if s.limiter == nil {
		return nil
	}
	now := s.now()
	if limited, err := s.limiter.Limited(ctx, "tenant", subj.TenantID, s.limits.PerTenant, now); err != nil || limited {
		return ErrRateLimited
	}
	if limited, err := s.limiter.Limited(ctx, "sender", subj.ActorID(), s.limits.PerSender, now); err != nil || limited {
		return ErrRateLimited
	}
	return nil
}

// deliver writes the pending entry, renders, sends and finalises the entry.
func (s *Sender) deliver(ctx context.Context, subj authz.Subjects, ch store.Channel, settings sealed.Settings, provider channel.Provider, templateID *string, recipient string, test bool, correlationID string, render func(context.Context) (string, string, error)) (LogView, error) {
	now := s.now()
	row := store.LogRow{ID: store.NewID(), TenantID: subj.TenantID, CreatedAt: now, ChannelID: ch.ID, ChannelType: ch.Type, TemplateID: templateID, Recipient: recipient,
		Status: "pending", SenderKind: subj.ActorKind(), SenderID: subj.ActorID(), Test: test}
	if err := s.st.InsertLog(ctx, row); err != nil {
		return LogView{}, err
	}
	subject, body, err := render(ctx)
	if err == nil {
		msg := channel.Message{To: recipient, Subject: subject}
		if ch.Type == channel.TypeEmail {
			msg.HTMLBody = body
		} else {
			msg.TextBody = body
		}
		err = provider.Send(ctx, settings, msg)
	}
	fields := s.channels.SecretFields(ch.Type)
	if err != nil {
		reason := sealed.Scrub(err.Error(), settings, fields)
		if errors.Is(err, channel.ErrNoProvider) {
			reason = "no_provider"
		}
		if e := s.st.SetLogOutcome(ctx, subj.TenantID, row.ID, "failed", reason, subject, body, nil); e != nil {
			return LogView{}, e
		}
		row.Status, row.Error, row.RenderedSubject, row.RenderedBody = "failed", reason, subject, body
		s.emit(audit.Event{Type: audit.NotificationFailed, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "notification", SubjectID: row.ID,
			Outcome: "failed", Reason: shortReason(reason), CorrelationID: correlationID, Details: map[string]any{"channel_id": ch.ID, "template_id": strp(templateID), "test": test}})
		return logView(row), nil
	}
	sent := s.now()
	if e := s.st.SetLogOutcome(ctx, subj.TenantID, row.ID, "sent", "", subject, body, &sent); e != nil {
		return LogView{}, e
	}
	row.Status, row.RenderedSubject, row.RenderedBody, row.SentAt = "sent", subject, body, &sent
	s.emit(audit.Event{Type: audit.NotificationSent, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "notification", SubjectID: row.ID,
		Outcome: "ok", CorrelationID: correlationID, Details: map[string]any{"channel_id": ch.ID, "template_id": strp(templateID), "test": test}})
	return logView(row), nil
}

func shortReason(r string) string {
	r = strings.TrimSpace(r)
	if len(r) > 120 {
		return r[:120]
	}
	return r
}

// ListLog pages the log newest first; callers without stats:read see their
// own sends only.
func (s *Sender) ListLog(ctx context.Context, subj authz.Subjects, f store.LogFilter, allSenders bool) ([]LogView, string, error) {
	if !allSenders {
		f.SenderID = subj.ActorID()
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	if f.Status != "" && f.Status != "pending" && f.Status != "sent" && f.Status != "failed" {
		return nil, "", invalid("unknown status", map[string]any{"field": "status"})
	}
	rows, err := s.st.LogPage(ctx, subj.TenantID, f)
	if err != nil {
		return nil, "", err
	}
	out := make([]LogView, 0, len(rows))
	for _, r := range rows {
		out = append(out, logView(r))
	}
	next := ""
	if len(rows) == f.Limit {
		last := rows[len(rows)-1]
		next = EncodeCursor(last.CreatedAt, last.ID)
	}
	return out, next, nil
}

// GetLog returns one entry with its body (own sends only without stats:read).
func (s *Sender) GetLog(ctx context.Context, subj authz.Subjects, id string, allSenders bool) (LogView, error) {
	row, err := s.st.GetLog(ctx, subj.TenantID, id)
	if err != nil {
		return LogView{}, err
	}
	if !allSenders && row.SenderID != subj.ActorID() {
		return LogView{}, store.ErrNotFound
	}
	return logView(row), nil
}

// ExpirePending marks entries stuck in pending for longer than age as failed.
func (s *Sender) ExpirePending(ctx context.Context, age time.Duration) (int64, error) {
	return s.st.ExpirePendingLogs(ctx, s.now().Add(-age))
}

// RunExpiry runs ExpirePending every interval until ctx ends.
func (s *Sender) RunExpiry(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = s.ExpirePending(ctx, 10*time.Minute)
		}
	}
}

// EncodeCursor / DecodeCursor format a (time, id) page cursor.
func EncodeCursor(ts time.Time, id string) string {
	return ts.UTC().Format(time.RFC3339Nano) + "|" + id
}

// DecodeCursor parses a cursor ("" → zero values).
func DecodeCursor(c string) (time.Time, string, error) {
	if c == "" {
		return time.Time{}, "", nil
	}
	i := strings.IndexByte(c, '|')
	if i <= 0 {
		return time.Time{}, "", invalid("bad cursor", map[string]any{"field": "cursor"})
	}
	ts, err := time.Parse(time.RFC3339Nano, c[:i])
	if err != nil {
		return time.Time{}, "", invalid("bad cursor", map[string]any{"field": "cursor"})
	}
	return ts, c[i+1:], nil
}

func (s *Sender) emit(e audit.Event) {
	if s.audit != nil {
		_ = s.audit.Emit(e)
	}
}
