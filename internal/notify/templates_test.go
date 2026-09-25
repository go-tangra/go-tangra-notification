package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
)

func systemTemplate(t *testing.T, f *fx, key string) TemplateView {
	t.Helper()
	ctx := context.Background()
	if _, err := f.tp.EnsureSystemTemplates(ctx, tP); err != nil {
		t.Fatal(err)
	}
	row, err := f.ms.TemplateByKey(ctx, tP, key)
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.tp.Get(ctx, platformAdmin(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestSystemTemplateGuards (US4, T055): only subject and body of a system
// template change; a save dropping a required variable is refused naming
// it; deletion is refused; restore returns to the built-in wording; the
// edited flag follows.
func TestSystemTemplateGuards(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	v := systemTemplate(t, f, "auth.invite")
	if v.SystemKey == nil || *v.SystemKey != "auth.invite" || v.Edited || v.Permissions.Delete || !v.Permissions.Write ||
		joined(v.RequiredVariables) != "link,valid_for" || joined(v.SecretVariables) != "link" || v.ChannelID != nil {
		t.Fatalf("view %+v", v)
	}
	edit := TemplateInput{Name: "auth.invite", Subject: "Welcome to Tangra", Body: "<p>Join us: {{.link}} (valid {{.valid_for}})</p>", Variables: v.Variables}
	up, err := f.tp.Update(ctx, platformAdmin(), v.ID, edit)
	if err != nil || up.Subject != "Welcome to Tangra" || !up.Edited || up.Name != "auth.invite" {
		t.Fatalf("edit %+v %v", up, err)
	}
	// Name and variables may be omitted (unchanged).
	if _, err := f.tp.Update(ctx, platformAdmin(), v.ID, TemplateInput{Subject: "Hi", Body: "{{.link}} {{.valid_for}}"}); err != nil {
		t.Fatalf("omitted fields: %v", err)
	}
	// Anything but subject and body is refused.
	for name, in := range map[string]TemplateInput{
		"rename":    {Name: "other", Subject: "s", Body: "{{.link}} {{.valid_for}}"},
		"channel":   {Name: "auth.invite", ChannelID: tA, Subject: "s", Body: "{{.link}} {{.valid_for}}"},
		"default":   {Name: "auth.invite", Subject: "s", Body: "{{.link}} {{.valid_for}}", IsDefault: true},
		"variables": {Name: "auth.invite", Subject: "s", Body: "{{.link}} {{.valid_for}}", Variables: []string{"link", "valid_for"}},
	} {
		if _, err := f.tp.Update(ctx, platformAdmin(), v.ID, in); !errors.Is(err, ErrSystemTemplateField) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The same variables in another order are fine.
	if _, err := f.tp.Update(ctx, platformAdmin(), v.ID, TemplateInput{Subject: "s", Body: "{{.link}} {{.valid_for}}", Variables: []string{"tenant", "valid_for", "link"}}); err != nil {
		t.Fatalf("reordered variables: %v", err)
	}
	// Dropping the link is refused and names it; so are syntax errors and undeclared variables.
	var mr *MissingRequiredError
	if _, err := f.tp.Update(ctx, platformAdmin(), v.ID, TemplateInput{Subject: "s", Body: "<p>no link, {{.valid_for}}</p>"}); !errors.As(err, &mr) || mr.Variable != "link" || !strings.Contains(mr.Error(), "link") {
		t.Fatalf("missing link: %v", err)
	}
	var ve *ValidationError
	if _, err := f.tp.Update(ctx, platformAdmin(), v.ID, TemplateInput{Subject: "s", Body: "{{.link"}); !errors.As(err, &ve) {
		t.Fatalf("syntax: %v", err)
	}
	if _, err := f.tp.Update(ctx, platformAdmin(), v.ID, TemplateInput{Subject: "{{.other}}", Body: "{{.link}} {{.valid_for}}"}); !errors.As(err, &ve) {
		t.Fatalf("undeclared: %v", err)
	}
	// Members of the platform tenant cannot write it.
	pm := authz.Subjects{TenantID: tP, UserID: uB, Roles: []string{"member"}}
	if _, err := f.tp.Update(ctx, pm, v.ID, edit); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member: %v", err)
	}
	if _, err := f.tp.Restore(ctx, pm, v.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member restore: %v", err)
	}
	// Delete refused; restore brings the built-in wording back.
	if err := f.tp.Delete(ctx, platformAdmin(), v.ID); !errors.Is(err, ErrSystemTemplate) {
		t.Fatalf("delete: %v", err)
	}
	r, err := f.tp.Restore(ctx, platformAdmin(), v.ID)
	if err != nil || r.Edited || r.Subject != SystemTemplates[0].Subject || r.Body != SystemTemplates[0].Body {
		t.Fatalf("restore %+v %v", r, err)
	}
	// Restore of an ordinary template is refused.
	c := f.channel(t, "relay", true)
	plain := f.template(t, "welcome", c.ID)
	if plain.SystemKey != nil || plain.Edited || plain.RequiredVariables == nil || plain.SecretVariables == nil {
		t.Fatalf("plain view %+v", plain)
	}
	if _, err := f.tp.Restore(ctx, admin(), plain.ID); !errors.Is(err, ErrNotSystemTemplate) {
		t.Fatalf("restore ordinary: %v", err)
	}
	if _, err := f.tp.Restore(ctx, admin(), tP); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("restore unknown: %v", err)
	}
	// Store failures surface.
	f.ms.FailOn("UpdateTemplate", errors.New("db"))
	if _, err := f.tp.Restore(ctx, platformAdmin(), v.ID); err == nil {
		t.Fatal("restore store error swallowed")
	}
	if _, err := f.tp.Update(ctx, platformAdmin(), v.ID, edit); err == nil {
		t.Fatal("update store error swallowed")
	}
	f.ms.FailOn("UpdateTemplate", nil)
	f.ms.FailOn("GetTemplate", errors.New("db"))
	if _, err := f.tp.Restore(ctx, platformAdmin(), v.ID); err == nil {
		t.Fatal("restore lookup error swallowed")
	}
	f.ms.FailOn("GetTemplate", nil)
	f.aw.Flush()
	var restored int
	for _, e := range f.ms.AuditEvents(tP, "template_updated") {
		if strings.Contains(string(e.Details), `"restored":true`) {
			restored++
		}
	}
	if restored != 1 {
		t.Fatalf("restored events %d", restored)
	}
}
