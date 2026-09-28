package notify

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

const schedulerSVID = "spiffe://example.org/svc/scheduler"

func schedFor(tenant string) authz.Subjects { return authz.ServiceSubjects(tenant, schedulerSVID) }

func custom(rcpt string) CustomInput {
	return CustomInput{Recipient: rcpt, CorrelationID: "exec-42"}
}

// TestSendCustomChannelChoice (T053): the tenant's default email channel or
// the chosen one; no platform-channel fallback; no grant is needed.
func TestSendCustomChannelChoice(t *testing.T) {
	f := keyFx(t) // the platform channel exists but must never be used
	ctx := context.Background()
	f.snd.limits.PerTenant, f.snd.limits.PerSender = 100, 100
	f.now = f.now.UTC()

	// No tenant email channel: email_not_configured, even with a platform channel.
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), custom("ann@example.org")); !errors.Is(err, ErrEmailNotConfigured) {
		t.Fatalf("no channel: %v", err)
	}
	def := f.channel(t, "default relay", true)
	other := f.channel(t, "other relay", false)

	v, err := f.snd.SendCustom(ctx, schedFor(tA), custom("ann@example.org"))
	if err != nil || v.Status != "sent" || v.ChannelID != def.ID || v.SenderKind != "service" || v.SenderID != schedulerSVID || v.TemplateID != nil || v.TemplateKey != nil {
		t.Fatalf("default channel %+v %v", v, err)
	}
	sent := f.email.sent[len(f.email.sent)-1]
	if sent.To != "ann@example.org" || sent.Subject != DefaultCustomSubject || !strings.Contains(sent.HTMLBody, "exec-42") ||
		!strings.Contains(sent.HTMLBody, f.now.Format("2006-01-02T15:04:05Z")) || !strings.HasPrefix(sent.HTMLBody, `<div style="white-space: pre-wrap">`) {
		t.Fatalf("default message %+v", sent)
	}
	in := custom("bob@example.org")
	in.Subject = strings.Repeat("é", MaxCustomSubject) // bounds count characters
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), in); err != nil {
		t.Fatalf("200 characters: %v", err)
	}
	in.ChannelID = other.ID
	in.Subject = "Weekly check"
	in.Body = "Hello <b>Bob</b> & co\nline 2"
	v, err = f.snd.SendCustom(ctx, schedFor(tA), in)
	if err != nil || v.ChannelID != other.ID || v.RenderedSubject != "Weekly check" {
		t.Fatalf("explicit channel %+v %v", v, err)
	}
	sent = f.email.sent[len(f.email.sent)-1]
	want := `<div style="white-space: pre-wrap">Hello &lt;b&gt;Bob&lt;/b&gt; &amp; co` + "\nline 2</div>"
	if sent.HTMLBody != want || sent.Subject != "Weekly check" {
		t.Fatalf("custom message %q", sent.HTMLBody)
	}
	row, err := f.ms.GetLog(ctx, tA, v.ID)
	if err != nil || row.Status != "sent" || row.RenderedBody != want || row.Recipient != "bob@example.org" || row.SenderID != schedulerSVID {
		t.Fatalf("log row %+v %v", row, err)
	}
	f.aw.Flush()
	events := f.ms.AuditEvents(tA, string(audit.NotificationSent))
	last := events[len(events)-1]
	if len(events) != 3 || last.ActorKind != "service" || last.ActorID != schedulerSVID || last.CorrelationID != "exec-42" || last.SubjectID != v.ID {
		t.Fatalf("audit %+v", events)
	}
	for _, e := range events {
		d := string(e.Details)
		if strings.Contains(d, "example.org") || strings.Contains(d, "Bob") {
			t.Fatalf("recipient or body in the audit details: %s", d)
		}
	}
	// The channel of another tenant is not found.
	in.ChannelID = other.ID
	if _, err := f.snd.SendCustom(ctx, schedFor(tB), in); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign channel: %v", err)
	}
	// A channel id that is not a uuid is not found either.
	in.ChannelID = "nope"
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), in); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("bad channel id: %v", err)
	}
	// No grants were given to the scheduler.
	for _, g := range f.ms.Grants {
		if g.SubjectID == schedulerSVID {
			t.Fatal("grant for the scheduler")
		}
	}
}

// TestSendCustomRefusals: wrong type, disabled, invalid recipient, CR/LF in
// the subject, oversize fields, missing tenant, rate limit and store errors
// are refused before anything is delivered.
func TestSendCustomRefusals(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	def := f.channel(t, "relay", true)
	sms, err := f.ch.Create(ctx, admin(), ChannelInput{Name: "texts", Type: "sms", Settings: sealed.Settings{"host": "x"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var ve *ValidationError
	in := custom("ann@example.org")
	in.ChannelID = sms.ID
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), in); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("sms channel: %v", err)
	}
	for _, bad := range []CustomInput{
		{Recipient: "not an address"},
		{Recipient: ""},
		{Recipient: "a@example.org", Subject: "Hi\r\nBcc: x@example.org"},
		{Recipient: "a@example.org", Subject: "line\nbreak"},
		{Recipient: "a@example.org", Subject: strings.Repeat("s", MaxCustomSubject+1)},
		{Recipient: "a@example.org", Body: strings.Repeat("b", MaxCustomBody+1)},
	} {
		if _, err := f.snd.SendCustom(ctx, schedFor(tA), bad); !errors.As(err, &ve) {
			t.Errorf("%.40q: %v", bad.Subject+bad.Recipient, err)
		}
	}
	if _, err := f.snd.SendCustom(ctx, schedFor(""), custom("a@example.org")); !errors.As(err, &ve) {
		t.Fatalf("no tenant: %v", err)
	}
	if _, err := f.snd.SendCustom(ctx, schedFor("x"), custom("a@example.org")); !errors.As(err, &ve) {
		t.Fatalf("bad tenant: %v", err)
	}
	f.limiter.err = errors.New("valkey down")
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), custom("a@example.org")); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("limited: %v", err)
	}
	f.limiter.err = nil
	f.ms.FailOn("DefaultEmailChannel", errors.New("db"))
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), custom("a@example.org")); err == nil || errors.Is(err, ErrEmailNotConfigured) {
		t.Fatalf("lookup error: %v", err)
	}
	f.ms.FailOn("DefaultEmailChannel", nil)
	f.ms.FailOn("GetChannel", errors.New("db"))
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), custom("a@example.org")); err == nil {
		t.Fatal("resolve error swallowed")
	}
	f.ms.FailOn("GetChannel", nil)
	if len(f.email.sent) != 0 {
		t.Fatalf("%d messages delivered on refusals", len(f.email.sent))
	}
	// Disabled default channel: refused, not skipped.
	if _, err := f.ch.Update(ctx, admin(), def.ID, ChannelInput{Name: def.Name, Settings: emailSettings("x"), Enabled: false, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), custom("a@example.org")); !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("disabled default: %v", err)
	}
	in = custom("a@example.org")
	in.ChannelID = def.ID
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), in); !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("disabled explicit: %v", err)
	}
}

// TestSendCustomOutcome: delivery failures come back in the entry with
// Retryable, are logged as failed and audited.
func TestSendCustomOutcome(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	f.snd.limits.PerTenant, f.snd.limits.PerSender = 100, 100
	f.channel(t, "relay", true)
	for _, tc := range []struct {
		err       error
		retryable bool
	}{
		{fmt.Errorf("email: %w", &textproto.Error{Code: 451, Msg: "try later"}), true},
		{fmt.Errorf("email: %w", &textproto.Error{Code: 550, Msg: "no such user"}), false},
		{errors.New("email: dial: connection refused"), true},
	} {
		f.email.fail = tc.err
		v, err := f.snd.SendCustom(ctx, schedFor(tA), custom("ann@example.org"))
		if err != nil || v.Status != "failed" || v.Retryable != tc.retryable {
			t.Fatalf("%v: %+v %v", tc.err, v, err)
		}
		if row, _ := f.ms.GetLog(ctx, tA, v.ID); row.Status != "failed" {
			t.Fatalf("row %+v", row)
		}
	}
	f.email.fail = nil
	f.aw.Flush()
	if n := len(f.ms.AuditEvents(tA, string(audit.NotificationFailed))); n != 3 {
		t.Fatalf("%d failure events", n)
	}
	f.ms.FailOn("InsertLog", errors.New("db"))
	if _, err := f.snd.SendCustom(ctx, schedFor(tA), custom("ann@example.org")); err == nil {
		t.Fatal("insert error swallowed")
	}
	f.ms.FailOn("InsertLog", nil)
}
