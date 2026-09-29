package notify

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/render"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// TestBuiltinTemplates: the system templates (research D3; lcm's expiry
// digest from feature 026) parse as email templates, declare what they
// reference, reference every required variable, and mark the links (and
// the legacy text) secret.
func TestBuiltinTemplates(t *testing.T) {
	want := map[string]struct{ vars, required, secret string }{
		"auth.invite":                {"link,tenant,valid_for", "link,valid_for", "link"},
		"auth.account_reset":         {"link,valid_for", "link,valid_for", "link"},
		"auth.recovery":              {"link,valid_for", "link,valid_for", "link"},
		"auth.message":               {"subject,text", "subject,text", "text"},
		"warden.share":               {"expires,link,message,openings,secret_name", "expires,link,openings,secret_name", "link"},
		"lcm.certificates_expiring":  {"certificates,count,days,tenant", "certificates,count,days", ""},
		"signing.invitation":         {"document,link,sender,signer", "document,link,signer", ""},
		"signing.next_signer":        {"document,link,sender,signer", "document,link,signer", ""},
		"signing.certificate_setup":  {"link,signer", "link,signer", ""},
		"signing.reminder":           {"document,link,reminder_no,sender,signer", "document,link,reminder_no,signer", ""},
		"signing.completed":          {"document,link,recipient", "document,link", ""},
		"signing.declined":           {"document,link,reason,recipient,signer", "document,link,reason,signer", ""},
		"signing.cancelled":          {"document,link,reason,recipient", "document,link,reason", ""},
		"signing.expired":            {"document,link,recipient", "document,link", ""},
		"signing.certificate_locked": {"link,locked_until,signer", "link,locked_until,signer", ""},
		"hr.request_submitted":       {"AbsenceType,ApproverName,Days,EmployeeName,EndDate,Reason,ReviewURL,StartDate", "AbsenceType,Days,EmployeeName,EndDate,ReviewURL,StartDate", ""},
		"hr.request_approved":        {"AbsenceType,Days,EmployeeName,EndDate,RequestURL,ReviewerName,StartDate", "AbsenceType,Days,EndDate,RequestURL,StartDate", ""},
		"hr.request_rejected":        {"AbsenceType,Days,EmployeeName,EndDate,RequestURL,ReviewNotes,ReviewerName,StartDate", "AbsenceType,Days,EndDate,RequestURL,StartDate", ""},
		"hr.request_revoked":         {"AbsenceType,Days,EmployeeName,EndDate,Reason,RequestURL,ReviewerName,StartDate", "AbsenceType,Days,EndDate,RequestURL,StartDate", ""},
		"hr.signing_failed":          {"AbsenceType,EmployeeName,EndDate,Outcome,RecipientName,RequestURL,StartDate", "AbsenceType,EmployeeName,EndDate,Outcome,RequestURL,StartDate", ""},
		"hr.allowance_overdrawn":     {"AbsenceType,EmployeeName,Remaining,RequestURL,Year", "AbsenceType,EmployeeName,Remaining,RequestURL,Year", ""},
	}
	if len(SystemTemplates) != len(want) {
		t.Fatalf("%d system templates", len(SystemTemplates))
	}
	for _, st := range SystemTemplates {
		w, ok := want[st.Key]
		if !ok || !ValidKey(st.Key) {
			t.Fatalf("unexpected key %q", st.Key)
		}
		if joined(st.Variables) != w.vars || joined(st.Required) != w.required || joined(st.Secret) != w.secret {
			t.Errorf("%s: vars %v required %v secret %v", st.Key, st.Variables, st.Required, st.Secret)
		}
		c, err := render.Parse(st.Subject, st.Body, render.KindHTML)
		if err != nil {
			t.Fatalf("%s: %v", st.Key, err)
		}
		if err := c.Validate(st.Variables); err != nil {
			t.Fatalf("%s: %v", st.Key, err)
		}
		for _, r := range st.Required {
			if !contains(c.Refs, r) {
				t.Errorf("%s: required %s not referenced", st.Key, r)
			}
		}
		if strings.Contains(st.Subject, ".link") {
			t.Errorf("%s: link in the subject", st.Key)
		}
	}
}

func joined(s []string) string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return strings.Join(c, ",")
}

// TestEnsureSystemTemplates (T021): all keys seeded in the platform tenant
// on the first start; later starts insert nothing, keep operator edits and
// refresh only the built-in wording and variable sets.
func TestEnsureSystemTemplates(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	res, err := f.tp.EnsureSystemTemplates(ctx, tP)
	if err != nil || res.Created != len(SystemTemplates) || res.Refreshed != 0 {
		t.Fatalf("first %+v %v", res, err)
	}
	for _, st := range SystemTemplates {
		row, err := f.ms.TemplateByKey(ctx, tP, st.Key)
		if err != nil || row.Name != st.Key || row.ChannelID != nil || row.ChannelType != "email" || row.IsDefault ||
			row.Subject != st.Subject || row.Body != st.Body || row.BuiltinSubject != st.Subject || row.BuiltinBody != st.Body ||
			joined(row.SecretVariables) != joined(st.Secret) || joined(row.RequiredVariables) != joined(st.Required) {
			t.Fatalf("%s: %v %+v", st.Key, err, row)
		}
	}
	// Nothing in other tenants.
	if _, err := f.ms.TemplateByKey(ctx, tA, "auth.invite"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tenant A: %v", err)
	}
	// Second start: unchanged.
	if res, err := f.tp.EnsureSystemTemplates(ctx, tP); err != nil || res.Created != 0 || res.Refreshed != 0 {
		t.Fatalf("second %+v %v", res, err)
	}
	// An operator edit survives; an older built-in wording is refreshed without touching the edit.
	row, _ := f.ms.TemplateByKey(ctx, tP, "auth.invite")
	row.Subject, row.Body = "Welcome to Tangra", "<p>Join: {{.link}} ({{.valid_for}})</p>"
	row.BuiltinSubject, row.SecretVariables = "old wording", []string{}
	f.ms.Templates[row.ID] = row
	if res, err := f.tp.EnsureSystemTemplates(ctx, tP); err != nil || res.Created != 0 || res.Refreshed != 1 {
		t.Fatalf("refresh %+v %v", res, err)
	}
	got, _ := f.ms.TemplateByKey(ctx, tP, "auth.invite")
	if got.Subject != "Welcome to Tangra" || !strings.Contains(got.Body, "Join:") || got.BuiltinSubject != SystemTemplates[0].Subject || joined(got.SecretVariables) != "link" {
		t.Fatalf("after refresh %+v", got)
	}
	f.aw.Flush()
	if n := len(f.ms.AuditEvents(tP, "template_created")); n != len(SystemTemplates) {
		t.Fatalf("audit %d", n)
	}
}

func TestEnsureSystemTemplatesFailures(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	// An ordinary template already named like a key blocks that key only.
	if err := f.ms.InsertTemplate(ctx, store.Template{ID: store.NewID(), TenantID: tP, Name: "auth.recovery", ChannelType: "email"}); err != nil {
		t.Fatal(err)
	}
	res, err := f.tp.EnsureSystemTemplates(ctx, tP)
	if err == nil || !strings.Contains(err.Error(), "auth.recovery") || res.Created != len(SystemTemplates)-1 {
		t.Fatalf("name clash %+v %v", res, err)
	}
	for _, op := range []string{"TemplateByKey", "SetTemplateBuiltin"} {
		f := newFx(t)
		if _, err := f.tp.EnsureSystemTemplates(ctx, tP); err != nil {
			t.Fatal(err)
		}
		for id, row := range f.ms.Templates {
			row.BuiltinBody = "old"
			f.ms.Templates[id] = row
		}
		f.ms.FailOn(op, errors.New("db"))
		if _, err := f.tp.EnsureSystemTemplates(ctx, tP); err == nil {
			t.Fatalf("%s error swallowed", op)
		}
	}
}

// TestCertificatesExpiringTemplate (feature 026): lcm's digest renders the
// pre-rendered list preformatted and escaped, with the counts in the subject.
func TestCertificatesExpiringTemplate(t *testing.T) {
	var st SystemTemplate
	for _, s := range SystemTemplates {
		if s.Key == "lcm.certificates_expiring" {
			st = s
		}
	}
	c, err := render.Parse(st.Subject, st.Body, render.KindHTML)
	if err != nil {
		t.Fatal(err)
	}
	sent, _, err := c.RenderRedacted(context.Background(), map[string]string{"days": "7", "count": "2", "certificates": "a.example.org — 2026-10-01\n<b>b</b>", "tenant": "Acme"}, st.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Subject != "2 certificate(s) expire within 7 days" || !strings.Contains(sent.Body, `<div style="white-space: pre-wrap">`) ||
		!strings.Contains(sent.Body, "&lt;b&gt;b&lt;/b&gt;") || !strings.Contains(sent.Body, "a.example.org") || !strings.Contains(sent.Body, "Acme") {
		t.Fatalf("rendered %+v", sent)
	}
}

// TestHRTemplates (feature 028): the leave e-mails render with the variables
// hr sends, escape what people typed, keep reasons and notes out of the
// subjects and render optional parts only when given.
func TestHRTemplates(t *testing.T) {
	base := map[string]string{"EmployeeName": "Maria <i>", "AbsenceType": "Paid leave", "StartDate": "06.07.2026", "EndDate": "10.07.2026",
		"Days": "5", "RequestURL": "https://portal.example.org/hr/requests/r1", "ReviewURL": "https://portal.example.org/hr/review",
		"ApproverName": "Petar", "ReviewerName": "Petar", "Reason": "<script>x</script>", "ReviewNotes": "team offsite", "RecipientName": "Petar",
		"Outcome": "declined", "Year": "2026", "Remaining": "-2"}
	for _, st := range SystemTemplates {
		if !strings.HasPrefix(st.Key, "hr.") {
			continue
		}
		for _, v := range []string{".Reason", ".ReviewNotes", "URL"} {
			if strings.Contains(st.Subject, v) {
				t.Errorf("%s: %s in the subject", st.Key, v)
			}
		}
		c, err := render.Parse(st.Subject, st.Body, render.KindHTML)
		if err != nil {
			t.Fatal(err)
		}
		sent, _, err := c.RenderRedacted(context.Background(), base, st.Secret)
		if err != nil {
			t.Fatalf("%s: %v", st.Key, err)
		}
		if strings.Contains(sent.Body, "<script>") || strings.Contains(sent.Body, "<i>") || !strings.Contains(sent.Body, "Paid leave") || !strings.Contains(sent.Body, `lang="bg"`) {
			t.Errorf("%s rendered %q", st.Key, sent.Body)
		}
	}
	var sub SystemTemplate
	for _, st := range SystemTemplates {
		if st.Key == "hr.request_submitted" {
			sub = st
		}
	}
	c, _ := render.Parse(sub.Subject, sub.Body, render.KindHTML)
	vars := map[string]string{}
	for _, v := range sub.Variables {
		vars[v] = base[v]
	}
	vars["Reason"] = ""
	sent, _, err := c.RenderRedacted(context.Background(), vars, nil)
	if err != nil || strings.Contains(sent.Body, "Reason given") || !strings.HasPrefix(sent.Subject, "Leave request: Maria") {
		t.Fatalf("no reason: %v %+v", err, sent)
	}
}
