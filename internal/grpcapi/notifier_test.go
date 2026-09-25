package grpcapi

import (
	"context"
	"errors"
	"net/textproto"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
)

const tP = "00000000-0000-0000-0000-000000000001"

// keyFx seeds the system templates and the platform channel in tP.
func keyFx(t *testing.T, spiffe string) *fx {
	t.Helper()
	f := newFx(t, spiffe)
	ctx := context.Background()
	f.snd.SetPlatformTenant(tP)
	if _, err := f.tp.EnsureSystemTemplates(ctx, tP); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, &notify.PlatformEmail{Host: "relay", Port: 587, From: "tangra@example.org"}); err != nil {
		t.Fatal(err)
	}
	return f
}

var inviteVars = map[string]string{"link": "https://p.example/accept?token=NOTIF-TOKEN-g1", "valid_for": "72 hours"}

// TestNotifierSendKey (T023, T031): auth sends its system template for any
// tenant without grants; the SDK maps the outcomes.
func TestNotifierSendKey(t *testing.T) {
	f := keyFx(t, "spiffe://example.org/svc/auth")
	ctx := context.Background()
	res, err := f.client.SendKey(ctx, tA, "auth.invite", "ann@example.org", inviteVars, "corr-9")
	if err != nil || !res.Sent || res.LogID == "" || len(f.email.sent) != 1 || !strings.Contains(f.email.sent[0].HTMLBody, "NOTIF-TOKEN-g1") {
		t.Fatalf("%+v %v", res, err)
	}
	row := f.ms.Logs[res.LogID]
	if row.TemplateKey == nil || *row.TemplateKey != "auth.invite" || strings.Contains(row.RenderedBody, "NOTIF-TOKEN") {
		t.Fatalf("log %+v", row)
	}
	// Retryable and permanent delivery failures.
	f.email.fail = &textproto.Error{Code: 451, Msg: "later"}
	raw := notificationv1.NewNotifierClient(f.raw)
	out, err := raw.Send(ctx, &notificationv1.SendRequest{TenantId: tA, TemplateKey: "auth.invite", Recipient: "ann@example.org", Variables: inviteVars})
	if err != nil || out.GetStatus() != notificationv1.DeliveryStatus_DELIVERY_STATUS_FAILED || !out.GetRetryable() {
		t.Fatalf("retryable %+v %v", out, err)
	}
	f.email.fail = &textproto.Error{Code: 550, Msg: "no such user"}
	if res, err := f.client.SendKey(ctx, tA, "auth.invite", "ann@example.org", inviteVars, ""); err != nil || res.Sent || res.Retryable || !strings.Contains(res.Reason, "550") {
		t.Fatalf("permanent %+v %v", res, err)
	}
	f.email.fail = nil
	// Refusals and their codes.
	for name, tc := range map[string]struct {
		key  string
		vars map[string]string
		code codes.Code
		msg  string
	}{
		"foreign":   {"warden.share", map[string]string{"link": "l", "secret_name": "s", "expires": "e", "openings": "1"}, codes.PermissionDenied, "key_namespace"},
		"unknown":   {"auth.nope", nil, codes.NotFound, "template_key"},
		"missing":   {"auth.invite", map[string]string{"valid_for": "1h"}, codes.InvalidArgument, "missing_variable:link"},
		"malformed": {"auth..x", nil, codes.InvalidArgument, "template_key"},
	} {
		_, err := raw.Send(ctx, &notificationv1.SendRequest{TenantId: tA, TemplateKey: tc.key, Recipient: "ann@example.org", Variables: tc.vars})
		if status.Code(err) != tc.code || status.Convert(err).Message() != tc.msg {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, req := range map[string]*notificationv1.SendRequest{
		"both":     {TenantId: tA, TemplateKey: "auth.invite", TemplateId: tA, Recipient: "a@b.c"},
		"override": {TenantId: tA, TemplateKey: "auth.invite", ChannelId: tA, Recipient: "a@b.c"},
		"tenant":   {TenantId: "x", TemplateKey: "auth.invite", Recipient: "a@b.c"},
		"rcpt":     {TenantId: tA, TemplateKey: "auth.invite", Recipient: ""},
	} {
		if _, err := raw.Send(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	f.aw.Flush()
	if refused := f.ms.AuditEvents(tA, "access_refused"); len(refused) != 1 || refused[0].Reason != "key_namespace" {
		t.Fatalf("audit %+v", refused)
	}
	// Email not configured: FailedPrecondition, a permanent error for the SDK.
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.SendKey(ctx, tA, "auth.invite", "ann@example.org", inviteVars, ""); status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "email_not_configured" {
		t.Fatalf("not configured: %v", err)
	}
	// Store outage: Unavailable, retryable for the SDK.
	f.ms.FailOn("TemplateByKey", errors.New("down"))
	if res, err := f.client.SendKey(ctx, tA, "auth.invite", "ann@example.org", inviteVars, ""); err != nil || !res.Retryable {
		t.Fatalf("outage %+v %v", res, err)
	}
}

// TestNotifierSendKeyNamespaces (SR-002): warden may send only warden.*;
// a caller whose identity is not a service has no namespace at all.
func TestNotifierSendKeyNamespaces(t *testing.T) {
	ctx := context.Background()
	w := keyFx(t, "spiffe://example.org/svc/warden")
	if _, err := w.client.SendKey(ctx, tA, "auth.invite", "a@example.org", inviteVars, ""); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("warden → auth.invite: %v", err)
	}
	share := map[string]string{"link": "https://w.example/warden/share#NOTIF-TOKEN-w", "secret_name": "db", "expires": "2026-10-01 12:00 UTC", "openings": "1"}
	if res, err := w.client.SendKey(ctx, tA, "warden.share", "a@example.org", share, ""); err != nil || !res.Sent {
		t.Fatalf("warden.share %+v %v", res, err)
	}
	u := keyFx(t, "spiffe://example.org/user/auth")
	if _, err := u.client.SendKey(ctx, tA, "auth.invite", "a@example.org", inviteVars, ""); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-service identity: %v", err)
	}
	// Throttled: ResourceExhausted "throttled", retryable for the SDK.
	th := keyFx(t, "spiffe://example.org/svc/auth")
	th.lim.counts = map[string]int{"system:auth": 100}
	raw := notificationv1.NewNotifierClient(th.raw)
	if _, err := raw.Send(ctx, &notificationv1.SendRequest{TenantId: tA, TemplateKey: "auth.invite", Recipient: "a@example.org", Variables: inviteVars}); status.Code(err) != codes.ResourceExhausted || status.Convert(err).Message() != "throttled" {
		t.Fatalf("throttled: %v", err)
	}
}
