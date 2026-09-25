package notify

import (
	"context"
	"errors"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/render"
	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
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
	// platformTenant owns the system templates and the platform channel.
	platformTenant string
}

// NewSender wires the pipeline.
func NewSender(st repo.Store, ch *Channels, tp *Templates, az *authz.Authz, aw *audit.Writer, l Limiter, limits Limits) *Sender {
	return &Sender{st: st, channels: ch, templates: tp, az: az, audit: aw, limiter: l, limits: limits, now: time.Now}
}

// SetClock injects the clock (tests).
func (s *Sender) SetClock(now func() time.Time) { s.now = now }

// SetPlatformTenant names the tenant holding the system templates and the
// platform channel (config platform_tenant_id).
func (s *Sender) SetPlatformTenant(id string) { s.platformTenant = id }

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
	// A system template sent by id keeps its secret variables out of the log too.
	return s.deliver(ctx, subj, delivery{ch: ch, settings: settings, provider: provider, templateID: &tid, recipient: in.Recipient, correlationID: in.CorrelationID,
		secrets: secretValues(tpl.SecretVariables, in.Variables),
		render: func(ctx context.Context) (render.Output, render.Output, error) {
			return compiled.RenderRedacted(ctx, in.Variables, tpl.SecretVariables)
		}})
}

// KeyInput is a system template send (feature 017).
type KeyInput struct {
	Key           string // "<service>.<name>"
	Recipient     string
	Variables     map[string]string
	CorrelationID string
}

// SendKey renders a system template of the platform tenant and delivers it
// for the tenant on behalf of a service (contracts notification-grpc.md):
// the key must lie in the caller's namespace (service = the name from its
// verified mesh identity), no per-tenant grant is needed, the channel is
// the tenant's enabled default email channel or else the platform channel,
// secret variables are redacted in the log, and sends count against the
// service's system rate limit. Delivery failures are reported in the
// returned entry with Retryable set, not as an error.
func (s *Sender) SendKey(ctx context.Context, subj authz.Subjects, service string, in KeyInput) (LogView, error) {
	if !ValidKey(in.Key) {
		return LogView{}, invalid("template_key", map[string]any{"field": "template_key"})
	}
	if service == "" || KeyService(in.Key) != service {
		s.emit(audit.Event{Type: audit.AccessRefused, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "template", SubjectID: in.Key,
			Outcome: "refused", Reason: "key_namespace", CorrelationID: in.CorrelationID, Details: map[string]any{"template_key": in.Key}})
		return LogView{}, ErrKeyNamespace
	}
	if err := checkVariables(in.Variables); err != nil {
		return LogView{}, err
	}
	tpl, err := s.st.TemplateByKey(ctx, s.platformTenant, in.Key)
	if errors.Is(err, store.ErrNotFound) {
		return LogView{}, ErrUnknownKey
	}
	if err != nil {
		return LogView{}, err
	}
	for _, r := range tpl.RequiredVariables {
		if _, ok := in.Variables[r]; !ok {
			return LogView{}, invalid("missing_variable:"+r, map[string]any{"variable": r})
		}
	}
	if err := channel.ValidateRecipient(channel.TypeEmail, in.Recipient); err != nil {
		return LogView{}, invalid("recipient is not valid for the channel type", map[string]any{"field": "recipient"})
	}
	compiled, err := render.Parse(tpl.Subject, tpl.Body, KindFor(tpl.ChannelType))
	if err != nil {
		return LogView{}, validationError(err)
	}
	ch, settings, provider, err := s.systemChannel(ctx, subj.TenantID)
	if err != nil {
		return LogView{}, err
	}
	// Optional declared variables the caller left out render empty.
	values := make(map[string]string, len(tpl.Variables))
	for _, v := range tpl.Variables {
		values[v] = ""
	}
	for k, v := range in.Variables {
		values[k] = v
	}
	if s.limiter != nil {
		if limited, err := s.limiter.Limited(ctx, "system", service, s.limits.System, s.now()); err != nil || limited {
			return LogView{}, ErrRateLimited
		}
	}
	tid, key := tpl.ID, in.Key
	return s.deliver(ctx, subj, delivery{ch: ch, settings: settings, provider: provider, templateID: &tid, templateKey: &key, recipient: in.Recipient, correlationID: in.CorrelationID,
		secrets: secretValues(tpl.SecretVariables, values),
		render: func(ctx context.Context) (render.Output, render.Output, error) {
			return compiled.RenderRedacted(ctx, values, tpl.SecretVariables)
		}})
}

// systemChannel resolves the channel of a system send (research D5): the
// tenant's enabled default email channel, else the enabled platform
// channel, else ErrEmailNotConfigured.
func (s *Sender) systemChannel(ctx context.Context, tenantID string) (store.Channel, sealed.Settings, channel.Provider, error) {
	own, err := s.st.DefaultEmailChannel(ctx, tenantID)
	switch {
	case err == nil && own.Enabled:
		return s.channels.Resolve(ctx, tenantID, own.ID)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return store.Channel{}, nil, nil, err
	}
	platform, err := s.st.ManagedChannel(ctx, s.platformTenant)
	switch {
	case err == nil && platform.Enabled:
		return s.channels.Resolve(ctx, s.platformTenant, platform.ID)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return store.Channel{}, nil, nil, err
	}
	return store.Channel{}, nil, nil, ErrEmailNotConfigured
}

// secretValues lists the non-empty values of the secret variables.
func secretValues(names []string, values map[string]string) []string {
	var out []string
	for _, n := range names {
		if v := values[n]; v != "" {
			out = append(out, v)
		}
	}
	return out
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
	return s.deliver(ctx, subj, delivery{ch: ch, settings: settings, provider: provider, recipient: recipient, test: true, correlationID: correlationID,
		render: func(context.Context) (render.Output, render.Output, error) {
			out := render.Output{Subject: testSubject, Body: testBody}
			return out, out, nil
		}})
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

// delivery is one send in flight.
type delivery struct {
	ch            store.Channel
	settings      sealed.Settings
	provider      channel.Provider
	templateID    *string
	templateKey   *string
	recipient     string
	test          bool
	correlationID string
	secrets       []string // secret variable values: never in the failure reason
	// render returns what is delivered and what the log stores (secrets redacted).
	render func(context.Context) (sent, stored render.Output, err error)
}

// deliver writes the pending entry, renders, sends and finalises the entry
// with the stored (redacted) render.
func (s *Sender) deliver(ctx context.Context, subj authz.Subjects, d delivery) (LogView, error) {
	now := s.now()
	row := store.LogRow{ID: store.NewID(), TenantID: subj.TenantID, CreatedAt: now, ChannelID: d.ch.ID, ChannelType: d.ch.Type, TemplateID: d.templateID, TemplateKey: d.templateKey,
		Recipient: d.recipient, Status: "pending", SenderKind: subj.ActorKind(), SenderID: subj.ActorID(), Test: d.test}
	if err := s.st.InsertLog(ctx, row); err != nil {
		return LogView{}, err
	}
	sent, stored, err := d.render(ctx)
	if err == nil {
		msg := channel.Message{To: d.recipient, Subject: sent.Subject}
		if d.ch.Type == channel.TypeEmail {
			msg.HTMLBody = sent.Body
		} else {
			msg.TextBody = sent.Body
		}
		err = d.provider.Send(ctx, d.settings, msg)
	}
	details := map[string]any{"channel_id": d.ch.ID, "template_id": strp(d.templateID), "test": d.test}
	if d.templateKey != nil {
		details["template_key"] = *d.templateKey
	}
	if err != nil {
		reason := scrubSecrets(sealed.Scrub(err.Error(), d.settings, s.channels.SecretFields(d.ch.Type)), d.secrets)
		if errors.Is(err, channel.ErrNoProvider) {
			reason = "no_provider"
		}
		if e := s.st.SetLogOutcome(ctx, subj.TenantID, row.ID, "failed", reason, stored.Subject, stored.Body, nil); e != nil {
			return LogView{}, e
		}
		row.Status, row.Error, row.RenderedSubject, row.RenderedBody = "failed", reason, stored.Subject, stored.Body
		s.emit(audit.Event{Type: audit.NotificationFailed, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "notification", SubjectID: row.ID,
			Outcome: "failed", Reason: shortReason(reason), CorrelationID: d.correlationID, Details: details})
		v := logView(row)
		v.Retryable = channel.Retryable(err)
		return v, nil
	}
	at := s.now()
	if e := s.st.SetLogOutcome(ctx, subj.TenantID, row.ID, "sent", "", stored.Subject, stored.Body, &at); e != nil {
		return LogView{}, e
	}
	row.Status, row.RenderedSubject, row.RenderedBody, row.SentAt = "sent", stored.Subject, stored.Body, &at
	s.emit(audit.Event{Type: audit.NotificationSent, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "notification", SubjectID: row.ID,
		Outcome: "ok", CorrelationID: d.correlationID, Details: details})
	return logView(row), nil
}

// scrubSecrets removes secret variable values from a failure reason in the
// forms a relay may echo them (raw, HTML-escaped, URL-escaped).
func scrubSecrets(reason string, secrets []string) string {
	for _, v := range secrets {
		for _, f := range []string{v, html.EscapeString(v), url.QueryEscape(v)} {
			reason = strings.ReplaceAll(reason, f, render.Redacted)
		}
	}
	return reason
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
