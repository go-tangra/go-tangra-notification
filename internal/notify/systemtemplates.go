package notify

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// SystemTemplate is a built-in email template addressed by key (research
// D3). Variables are what the owning service supplies; Required must stay
// referenced when an operator edits the wording; Secret values (one-time
// links) are redacted in the stored log.
type SystemTemplate struct {
	Key       string
	Subject   string
	Body      string // HTML (email bodies render with html/template)
	Variables []string
	Required  []string
	Secret    []string
}

// linkHint repeats a link as text for mail clients that do not open anchors.
const linkHint = `<p>If the link does not open, copy this address into your browser:<br>{{.link}}</p>`

// SystemTemplates is the built-in set; operators may edit subject and body.
var SystemTemplates = []SystemTemplate{
	{
		Key:     "auth.invite",
		Subject: `You are invited{{if .tenant}} to {{.tenant}}{{end}}`,
		Body: `<p>Hello,</p>
<p>You have been invited to {{if .tenant}}<strong>{{.tenant}}</strong> on {{end}}the platform. Open the link below to choose your password and activate your account.</p>
<p><a href="{{.link}}">Accept the invitation</a></p>
` + linkHint + `
<p>The link works once and expires in {{.valid_for}}. If you did not expect this invitation, you can ignore this message.</p>`,
		Variables: []string{"link", "valid_for", "tenant"},
		Required:  []string{"link", "valid_for"},
		Secret:    []string{"link"},
	},
	{
		Key:     "auth.account_reset",
		Subject: `Your account has been reset`,
		Body: `<p>Hello,</p>
<p>An administrator has reset your account. Open the link below to choose a new password.</p>
<p><a href="{{.link}}">Set a new password</a></p>
` + linkHint + `
<p>The link works once and expires in {{.valid_for}}. If you did not expect this, contact your administrator.</p>`,
		Variables: []string{"link", "valid_for"},
		Required:  []string{"link", "valid_for"},
		Secret:    []string{"link"},
	},
	{
		Key:     "auth.recovery",
		Subject: `Reset your password`,
		Body: `<p>Hello,</p>
<p>We received a request to reset the password of your account. Open the link below to choose a new one.</p>
<p><a href="{{.link}}">Reset your password</a></p>
` + linkHint + `
<p>The link works once and expires in {{.valid_for}}. If you did not ask for this, you can ignore this message: your password stays unchanged.</p>`,
		Variables: []string{"link", "valid_for"},
		Required:  []string{"link", "valid_for"},
		Secret:    []string{"link"},
	},
	{
		// Messages queued by auth 4.1 or older carry pre-rendered text only;
		// the text may hold a link, so all of it is secret.
		Key:       "auth.message",
		Subject:   `{{.subject}}`,
		Body:      `<div style="white-space: pre-wrap">{{.text}}</div>`,
		Variables: []string{"subject", "text"},
		Required:  []string{"subject", "text"},
		Secret:    []string{"text"},
	},
	{
		Key:     "warden.share",
		Subject: `A secret has been shared with you`,
		Body: `<p>Hello,</p>
<p>A secret, <strong>{{.secret_name}}</strong>, has been shared with you.</p>
{{if .message}}<p>Message from the sender:</p>
<blockquote>{{.message}}</blockquote>
{{end}}<p><a href="{{.link}}">Open the secret</a></p>
` + linkHint + `
<p>The link is valid until {{.expires}}. Number of times it can be opened: {{.openings}}.</p>
<p>Do not forward this message: anyone with the link can open the secret.</p>`,
		Variables: []string{"link", "secret_name", "expires", "openings", "message"},
		Required:  []string{"link", "secret_name", "expires", "openings"},
		Secret:    []string{"link"},
	},
}

// SeedResult counts what EnsureSystemTemplates did.
type SeedResult struct {
	Created   int
	Refreshed int
}

// EnsureSystemTemplates inserts the system templates missing from the
// platform tenant and refreshes the built-in wording and variable sets of
// existing ones; subject and body are never overwritten (operator edits
// survive restarts and upgrades). A key that cannot be seeded (a template
// of that name already exists) does not stop the others; every failure is
// returned.
func (t *Templates) EnsureSystemTemplates(ctx context.Context, tenantID string) (SeedResult, error) {
	var res SeedResult
	var errs []error
	for _, st := range SystemTemplates {
		row, err := t.st.TemplateByKey(ctx, tenantID, st.Key)
		switch {
		case errors.Is(err, store.ErrNotFound):
			key := st.Key
			row = store.Template{ID: store.NewID(), TenantID: tenantID, Name: st.Key, ChannelType: channel.TypeEmail, Subject: st.Subject, Body: st.Body,
				Variables: st.Variables, SystemKey: &key, BuiltinSubject: st.Subject, BuiltinBody: st.Body, RequiredVariables: st.Required, SecretVariables: st.Secret}
			if err := t.st.InsertTemplate(ctx, row); err != nil {
				errs = append(errs, fmt.Errorf("notify: system template %s: %w", st.Key, err))
				continue
			}
			res.Created++
			t.emit(audit.Event{Type: audit.TemplateCreated, TenantID: tenantID, ActorKind: "system", ActorID: "system_templates", SubjectKind: "template", SubjectID: row.ID, Outcome: "ok",
				Details: map[string]any{"name": st.Key, "system_key": st.Key}})
		case err != nil:
			errs = append(errs, fmt.Errorf("notify: system template %s: %w", st.Key, err))
		case row.BuiltinSubject != st.Subject || row.BuiltinBody != st.Body || !slices.Equal(row.Variables, st.Variables) ||
			!slices.Equal(row.RequiredVariables, st.Required) || !slices.Equal(row.SecretVariables, st.Secret):
			row.BuiltinSubject, row.BuiltinBody, row.Variables, row.RequiredVariables, row.SecretVariables = st.Subject, st.Body, st.Variables, st.Required, st.Secret
			if err := t.st.SetTemplateBuiltin(ctx, row); err != nil {
				errs = append(errs, fmt.Errorf("notify: system template %s: %w", st.Key, err))
				continue
			}
			res.Refreshed++
		}
	}
	return res, errors.Join(errs...)
}

func contains(list []string, v string) bool { return slices.Contains(list, v) }
