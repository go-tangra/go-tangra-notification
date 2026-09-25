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
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

const authSVID = "spiffe://example.org/svc/auth"

// keyFx is the notify fixture with the system templates seeded and the
// platform channel configured in tP.
func keyFx(t *testing.T) *fx {
	t.Helper()
	f := newFx(t)
	ctx := context.Background()
	f.snd.SetPlatformTenant(tP)
	f.snd.limits.System = 100
	if _, err := f.tp.EnsureSystemTemplates(ctx, tP); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err != nil {
		t.Fatal(err)
	}
	return f
}

func authFor(tenant string) authz.Subjects { return authz.ServiceSubjects(tenant, authSVID) }

func invite(link string) KeyInput {
	return KeyInput{Key: "auth.invite", Recipient: "ann@example.org", Variables: map[string]string{"link": link, "valid_for": "72 hours", "tenant": "Acme"}, CorrelationID: "corr-1"}
}

// TestSendKeyChannelResolution (T022, research D5): the tenant's enabled
// default email channel, else the platform channel, else
// email_not_configured; no grant on template or channel is needed.
func TestSendKeyChannelResolution(t *testing.T) {
	f := keyFx(t)
	ctx := context.Background()
	platform, _ := f.ms.ManagedChannel(ctx, tP)
	tpl, _ := f.ms.TemplateByKey(ctx, tP, "auth.invite")

	// Tenant A has no email channel: the platform channel delivers, the entry lives in tenant A.
	v, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p.example/accept?token=NOTIF-TOKEN-1"))
	if err != nil || v.Status != "sent" || v.ChannelID != platform.ID || v.TemplateKey == nil || *v.TemplateKey != "auth.invite" || v.TemplateID == nil || *v.TemplateID != tpl.ID ||
		v.SenderKind != "service" || v.SenderID != authSVID {
		t.Fatalf("platform fallback %+v %v", v, err)
	}
	if row, err := f.ms.GetLog(ctx, tA, v.ID); err != nil || row.TemplateKey == nil || strings.Contains(row.RenderedBody, "NOTIF-TOKEN") || !strings.Contains(row.RenderedBody, "[redacted]") {
		t.Fatalf("log row %+v %v", row, err)
	}
	sent := f.email.sent[len(f.email.sent)-1]
	if !strings.Contains(sent.HTMLBody, "NOTIF-TOKEN-1") || sent.To != "ann@example.org" || !strings.Contains(sent.Subject, "Acme") {
		t.Fatalf("delivered %+v", sent)
	}
	// Optional variables may be omitted.
	in := invite("https://p.example/accept?token=NOTIF-TOKEN-2")
	delete(in.Variables, "tenant")
	if v, err := f.snd.SendKey(ctx, authFor(tA), "auth", in); err != nil || v.Status != "sent" {
		t.Fatalf("optional omitted %+v %v", v, err)
	}
	// The tenant's own default email channel wins.
	own := f.channel(t, "acme relay", true)
	if v, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p.example/x")); err != nil || v.ChannelID != own.ID {
		t.Fatalf("tenant channel %+v %v", v, err)
	}
	// Disabled: back to the platform channel.
	if _, err := f.ch.Update(ctx, admin(), own.ID, ChannelInput{Name: own.Name, Settings: emailSettings("x"), Enabled: false, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if v, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p.example/x")); err != nil || v.ChannelID != platform.ID {
		t.Fatalf("disabled tenant channel %+v %v", v, err)
	}
	// The platform tenant itself uses its default: the platform channel.
	if v, err := f.snd.SendKey(ctx, authFor(tP), "auth", invite("https://p.example/x")); err != nil || v.ChannelID != platform.ID {
		t.Fatalf("platform tenant %+v %v", v, err)
	}
	// No platform channel either: email_not_configured (permanent).
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p.example/x")); !errors.Is(err, ErrEmailNotConfigured) {
		t.Fatalf("not configured: %v", err)
	}
	f.ms.FailOn("DefaultEmailChannel", errors.New("db"))
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p.example/x")); err == nil || errors.Is(err, ErrEmailNotConfigured) {
		t.Fatalf("lookup error: %v", err)
	}
	f.ms.FailOn("DefaultEmailChannel", nil)
	f.ms.FailOn("ManagedChannel", errors.New("db"))
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p.example/x")); err == nil || errors.Is(err, ErrEmailNotConfigured) {
		t.Fatalf("managed lookup error: %v", err)
	}
	f.ms.FailOn("ManagedChannel", nil)
	// No grants were ever given to the service.
	for _, g := range f.ms.Grants {
		if g.SubjectType == authz.SubjectUser && g.SubjectID == authSVID {
			t.Fatal("grant for the service")
		}
	}
}

// TestSendKeyRefusals: malformed or foreign keys, unknown keys, missing
// required variables and bad recipients are refused before anything is
// logged; the namespace refusal is audited.
func TestSendKeyRefusals(t *testing.T) {
	f := keyFx(t)
	ctx := context.Background()
	var ve *ValidationError
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", KeyInput{Key: "Auth.Invite", Recipient: "a@b.c"}); !errors.As(err, &ve) {
		t.Fatalf("malformed key: %v", err)
	}
	// warden may not send auth's templates (and auth not warden's).
	if _, err := f.snd.SendKey(ctx, authz.ServiceSubjects(tA, "spiffe://example.org/svc/warden"), "warden", invite("https://p/x")); !errors.Is(err, ErrKeyNamespace) {
		t.Fatalf("warden → auth.invite: %v", err)
	}
	share := KeyInput{Key: "warden.share", Recipient: "a@b.c", Variables: map[string]string{"link": "https://w/x", "secret_name": "db", "expires": "tomorrow", "openings": "1"}}
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", share); !errors.Is(err, ErrKeyNamespace) {
		t.Fatalf("auth → warden.share: %v", err)
	}
	if _, err := f.snd.SendKey(ctx, authFor(tA), "", invite("https://p/x")); !errors.Is(err, ErrKeyNamespace) {
		t.Fatalf("no service: %v", err)
	}
	f.aw.Flush()
	refused := f.ms.AuditEvents(tA, string(audit.AccessRefused))
	if len(refused) != 3 || refused[0].Reason != "key_namespace" || refused[0].ActorKind != "service" {
		t.Fatalf("audit %+v", refused)
	}
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", KeyInput{Key: "auth.nope", Recipient: "a@b.c"}); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown key: %v", err)
	}
	in := invite("https://p/x")
	delete(in.Variables, "link")
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", in); !errors.As(err, &ve) || ve.Msg != "missing_variable:link" {
		t.Fatalf("missing link: %v", err)
	}
	in = invite("https://p/x")
	in.Recipient = "not an address"
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", in); !errors.As(err, &ve) {
		t.Fatalf("recipient: %v", err)
	}
	in = invite("https://p/x")
	in.Variables["bad name"] = "x"
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", in); !errors.As(err, &ve) {
		t.Fatalf("variable name: %v", err)
	}
	// An operator-broken template (edited in the database) is refused as invalid.
	tpl, _ := f.ms.TemplateByKey(ctx, tP, "auth.invite")
	tpl.Body = "{{.link"
	f.ms.Templates[tpl.ID] = tpl
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p/x")); !errors.As(err, &ve) {
		t.Fatalf("broken template: %v", err)
	}
	f.ms.FailOn("TemplateByKey", errors.New("db"))
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p/x")); err == nil || errors.Is(err, ErrUnknownKey) {
		t.Fatalf("store error: %v", err)
	}
	if len(f.ms.Logs) != 0 {
		t.Fatalf("refusals logged %d entries", len(f.ms.Logs))
	}
}

// TestSendKeyRateLimit (SR-005): key sends count per calling service under
// system:<service>, not against the tenant; throttled is ErrRateLimited.
func TestSendKeyRateLimit(t *testing.T) {
	f := keyFx(t)
	ctx := context.Background()
	f.snd.limits.System = 2
	for i := 0; i < 2; i++ {
		if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p/x")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.snd.SendKey(ctx, authFor(tB), "auth", invite("https://p/x")); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("throttled: %v", err)
	}
	if f.limiter.counts["system:auth"] != 3 || f.limiter.counts["tenant:"+tA] != 0 {
		t.Fatalf("counters %v", f.limiter.counts)
	}
	f.limiter.err = errors.New("valkey down")
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite("https://p/x")); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("counter down: %v", err)
	}
}

// TestSendKeyOutcome: delivery failures come back as failed entries with
// retryable set by classification (research D7) and a reason scrubbed of
// the relay password and the secret values.
func TestSendKeyOutcome(t *testing.T) {
	f := keyFx(t)
	ctx := context.Background()
	link := "https://p.example/accept?token=NOTIF-TOKEN-fail"
	for _, tc := range []struct {
		err       error
		retryable bool
	}{
		{fmt.Errorf("email: recipient: %w", &textproto.Error{Code: 451, Msg: "greylisted " + link}), true},
		{fmt.Errorf("email: recipient: %w", &textproto.Error{Code: 550, Msg: "no such user NOTIF-MARKER-PW-relay"}), false},
		{errors.New("email: dial: connection refused"), true},
	} {
		f.email.fail = tc.err
		v, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite(link))
		if err != nil || v.Status != "failed" || v.Retryable != tc.retryable || strings.Contains(v.Error, "NOTIF-TOKEN") || strings.Contains(v.Error, "NOTIF-MARKER") {
			t.Fatalf("%v: %+v %v", tc.err, v, err)
		}
		row, _ := f.ms.GetLog(ctx, tA, v.ID)
		if row.Status != "failed" || strings.Contains(row.Error, "NOTIF-TOKEN") || strings.Contains(row.RenderedBody, "NOTIF-TOKEN") {
			t.Fatalf("row %+v", row)
		}
	}
	f.email.fail = nil
	// Store failures while logging surface as errors (the caller retries).
	f.ms.FailOn("InsertLog", errors.New("db"))
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite(link)); err == nil {
		t.Fatal("insert error swallowed")
	}
	f.ms.FailOn("InsertLog", nil)
	f.ms.FailOn("GetChannel", errors.New("db"))
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite(link)); err == nil {
		t.Fatal("resolve error swallowed")
	}
	f.ms.FailOn("GetChannel", nil)
	if _, err := f.snd.SendKey(ctx, authFor(tA), "auth", invite(link)); err != nil {
		t.Fatal(err)
	}
	_ = store.ErrNotFound
}
