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

// TestBuiltinTemplates: the five system templates (research D3) parse as
// email templates, declare what they reference, reference every required
// variable, and mark the links (and the legacy text) secret.
func TestBuiltinTemplates(t *testing.T) {
	want := map[string]struct{ vars, required, secret string }{
		"auth.invite":        {"link,tenant,valid_for", "link,valid_for", "link"},
		"auth.account_reset": {"link,valid_for", "link,valid_for", "link"},
		"auth.recovery":      {"link,valid_for", "link,valid_for", "link"},
		"auth.message":       {"subject,text", "subject,text", "text"},
		"warden.share":       {"expires,link,message,openings,secret_name", "expires,link,openings,secret_name", "link"},
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
