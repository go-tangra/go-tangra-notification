package notify

import (
	"context"
	"html"
	"net/url"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// secretValue is a link with characters every escaper rewrites.
const secretValue = `https://p.example/accept?token=NOTIF-TOKEN-zz9&next=/a b"c'<d>`

// TestSystemSendRedaction (SR-001, SC-003): for every system template,
// the secret values never appear in the stored log entry (subject, body,
// error), the returned view, or any audit row, delivered or failed; the
// recipient gets the real value.
func TestSystemSendRedaction(t *testing.T) {
	forms := []string{"NOTIF-TOKEN-zz9", secretValue, html.EscapeString(secretValue), url.QueryEscape(secretValue)}
	f := keyFx(t)
	ctx := context.Background()
	check := func(where, text string) {
		t.Helper()
		for _, s := range forms {
			if strings.Contains(text, s) {
				t.Fatalf("%s leaks %q: %q", where, s, text)
			}
		}
	}
	for _, st := range SystemTemplates {
		service := KeyService(st.Key)
		vars := map[string]string{}
		for _, v := range st.Variables {
			vars[v] = "value of " + v
		}
		for _, s := range st.Secret {
			vars[s] = secretValue
		}
		for _, fail := range []bool{false, true} {
			f.email.fail = nil
			if fail {
				f.email.fail = &relayError{"relay refused " + secretValue}
			}
			subj := authFor(tA)
			subj.Service = "spiffe://example.org/svc/" + service
			v, err := f.snd.SendKey(ctx, subj, service, KeyInput{Key: st.Key, Recipient: "ann@example.org", Variables: vars, CorrelationID: "c"})
			if err != nil {
				t.Fatalf("%s: %v", st.Key, err)
			}
			row, _ := f.ms.GetLog(ctx, tA, v.ID)
			check(st.Key+" view", v.RenderedSubject+v.RenderedBody+v.Error)
			check(st.Key+" stored", row.RenderedSubject+row.RenderedBody+row.Error)
			if !strings.Contains(row.RenderedBody+row.RenderedSubject, "[redacted]") {
				t.Fatalf("%s: no marker in %q", st.Key, row.RenderedBody)
			}
			if !fail {
				sent := f.email.sent[len(f.email.sent)-1]
				if !strings.Contains(sent.HTMLBody+sent.Subject, "NOTIF-TOKEN-zz9") {
					t.Fatalf("%s: recipient did not get the secret", st.Key)
				}
			}
		}
	}
	f.aw.Flush()
	for _, e := range f.ms.Audit {
		check("audit "+e.EventType, string(e.Details)+e.Reason)
	}
	if n := len(f.ms.AuditEvents(tA, string(audit.NotificationSent))); n != len(SystemTemplates) {
		t.Fatalf("sent events %d", n)
	}
	for _, e := range f.ms.AuditEvents(tA, string(audit.NotificationSent)) {
		if !strings.Contains(string(e.Details), `"template_key"`) || e.CorrelationID != "c" {
			t.Fatalf("audit details %s", e.Details)
		}
	}
	_ = store.ErrNotFound
}

type relayError struct{ msg string }

func (e *relayError) Error() string { return e.msg }

// TestOrdinarySendRedactsSystemTemplate: a platform administrator sending a
// system template by id (with a channel override) still gets the stored
// copy redacted.
func TestOrdinarySendRedactsSystemTemplate(t *testing.T) {
	f := keyFx(t)
	ctx := context.Background()
	tpl, _ := f.ms.TemplateByKey(ctx, tP, "auth.recovery")
	platform, _ := f.ms.ManagedChannel(ctx, tP)
	v, err := f.snd.Send(ctx, platformAdmin(), SendInput{TemplateID: tpl.ID, ChannelID: platform.ID, Recipient: "ann@example.org", Variables: map[string]string{"link": secretValue, "valid_for": "30 minutes"}})
	if err != nil || v.Status != "sent" {
		t.Fatalf("%+v %v", v, err)
	}
	row, _ := f.ms.GetLog(ctx, tP, v.ID)
	if strings.Contains(row.RenderedBody, "NOTIF-TOKEN") || !strings.Contains(row.RenderedBody, "[redacted]") {
		t.Fatalf("stored %q", row.RenderedBody)
	}
}
