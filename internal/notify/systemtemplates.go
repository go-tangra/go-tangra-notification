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
	{
		// lcm's scheduled digest (feature 026): certificates is a plain-text
		// list pre-rendered by lcm; it is escaped and kept preformatted.
		Key:     "lcm.certificates_expiring",
		Subject: `{{.count}} certificate(s) expire within {{.days}} days`,
		Body: `<p>Hello,</p>
<p>{{.count}} certificate(s){{if .tenant}} of <strong>{{.tenant}}</strong>{{end}} expire within the next {{.days}} days. Renew or replace them before they expire:</p>
<div style="white-space: pre-wrap">{{.certificates}}</div>
<p>This digest is sent by a scheduled task of the certificate lifecycle module.</p>`,
		Variables: []string{"days", "count", "certificates", "tenant"},
		Required:  []string{"days", "count", "certificates"},
	},
	{
		// signing (feature 027): variables are names, links and reasons only —
		// never field values. Links open the portal (sign-in required).
		Key:     "signing.invitation",
		Subject: `Please sign: {{.document}}`,
		Body: `<p>Hello {{.signer}},</p>
<p>{{if .sender}}{{.sender}} asks you{{else}}You are asked{{end}} to sign <strong>{{.document}}</strong>.</p>
<p><a href="{{.link}}">Open the document</a></p>
` + linkHint + `
<p lang="bg">Моля, подпишете документа „{{.document}}“.</p>`,
		Variables: []string{"document", "sender", "signer", "link"},
		Required:  []string{"document", "signer", "link"},
	},
	{
		Key:     "signing.next_signer",
		Subject: `Your turn to sign: {{.document}}`,
		Body: `<p>Hello {{.signer}},</p>
<p>The previous signers have signed <strong>{{.document}}</strong>{{if .sender}} (sent by {{.sender}}){{end}}. It is your turn now.</p>
<p><a href="{{.link}}">Open the document</a></p>
` + linkHint + `
<p lang="bg">Ваш ред е да подпишете „{{.document}}“.</p>`,
		Variables: []string{"document", "sender", "signer", "link"},
		Required:  []string{"document", "signer", "link"},
	},
	{
		Key:     "signing.certificate_setup",
		Subject: `Set up your signing certificate`,
		Body: `<p>Hello {{.signer}},</p>
<p>To sign documents you need a personal signing certificate protected by a PIN that only you know. Setting it up takes a minute.</p>
<p><a href="{{.link}}">Set up my certificate</a></p>
` + linkHint,
		Variables: []string{"signer", "link"},
		Required:  []string{"signer", "link"},
	},
	{
		Key:     "signing.reminder",
		Subject: `Reminder: please sign {{.document}}`,
		Body: `<p>Hello {{.signer}},</p>
<p>This is reminder {{.reminder_no}}: <strong>{{.document}}</strong>{{if .sender}} from {{.sender}}{{end}} still waits for your signature.</p>
<p><a href="{{.link}}">Open the document</a></p>
` + linkHint + `
<p lang="bg">Напомняне: документът „{{.document}}“ очаква вашия подпис.</p>`,
		Variables: []string{"document", "sender", "signer", "link", "reminder_no"},
		Required:  []string{"document", "signer", "link", "reminder_no"},
	},
	{
		Key:     "signing.completed",
		Subject: `Signed by everyone: {{.document}}`,
		Body: `<p>Hello{{if .recipient}} {{.recipient}}{{end}},</p>
<p>Everyone has signed <strong>{{.document}}</strong>. The signed document and its audit trail are available.</p>
<p><a href="{{.link}}">Open the document</a></p>
` + linkHint + `
<p lang="bg">Документът „{{.document}}“ е подписан от всички страни.</p>`,
		Variables: []string{"document", "recipient", "link"},
		Required:  []string{"document", "link"},
	},
	{
		Key:     "signing.declined",
		Subject: `Declined: {{.document}}`,
		Body: `<p>Hello{{if .recipient}} {{.recipient}}{{end}},</p>
<p>{{.signer}} declined to sign <strong>{{.document}}</strong>, so the signing was cancelled.</p>
<p>Reason given:</p>
<blockquote>{{.reason}}</blockquote>
<p><a href="{{.link}}">Open the submission</a></p>
` + linkHint,
		Variables: []string{"document", "signer", "reason", "recipient", "link"},
		Required:  []string{"document", "signer", "reason", "link"},
	},
	{
		Key:     "signing.cancelled",
		Subject: `Cancelled: {{.document}}`,
		Body: `<p>Hello{{if .recipient}} {{.recipient}}{{end}},</p>
<p>The signing of <strong>{{.document}}</strong> was cancelled. You no longer need to sign it.</p>
<p>Reason given:</p>
<blockquote>{{.reason}}</blockquote>
<p><a href="{{.link}}">Open the submission</a></p>
` + linkHint,
		Variables: []string{"document", "reason", "recipient", "link"},
		Required:  []string{"document", "reason", "link"},
	},
	{
		Key:     "signing.expired",
		Subject: `Expired: {{.document}}`,
		Body: `<p>Hello{{if .recipient}} {{.recipient}}{{end}},</p>
<p>The signing of <strong>{{.document}}</strong> expired before everyone signed. It can no longer be signed.</p>
<p><a href="{{.link}}">Open the submission</a></p>
` + linkHint,
		Variables: []string{"document", "recipient", "link"},
		Required:  []string{"document", "link"},
	},
	{
		Key:     "signing.certificate_locked",
		Subject: `Your signing certificate is locked`,
		Body: `<p>Hello {{.signer}},</p>
<p>Your signing certificate was locked after too many wrong PINs. You can sign again after {{.locked_until}}.</p>
<p>If you did not try to sign, tell your administrator.</p>
<p><a href="{{.link}}">My signing certificate</a></p>
` + linkHint,
		Variables: []string{"signer", "locked_until", "link"},
		Required:  []string{"signer", "locked_until", "link"},
	},
	{
		// hr (feature 028): names, dates, day counts and the review notes the
		// people involved wrote; links open the portal (sign-in required).
		// Reasons and notes never appear in subjects.
		Key:     "hr.request_submitted",
		Subject: `Leave request: {{.EmployeeName}}, {{.StartDate}} – {{.EndDate}}`,
		Body: `<p>Hello{{if .ApproverName}} {{.ApproverName}}{{end}},</p>
<p>{{.EmployeeName}} asks for <strong>{{.AbsenceType}}</strong> from {{.StartDate}} to {{.EndDate}} ({{.Days}} days).</p>
{{if .Reason}}<p>Reason given:</p>
<blockquote>{{.Reason}}</blockquote>
{{end}}<p><a href="{{.ReviewURL}}">Review leave requests</a></p>
<p>If the link does not open, copy this address into your browser:<br>{{.ReviewURL}}</p>
<p lang="bg">{{.EmployeeName}} подаде заявка за отпуск от {{.StartDate}} до {{.EndDate}} ({{.Days}} дни).</p>`,
		Variables: []string{"ApproverName", "EmployeeName", "AbsenceType", "StartDate", "EndDate", "Days", "Reason", "ReviewURL"},
		Required:  []string{"EmployeeName", "AbsenceType", "StartDate", "EndDate", "Days", "ReviewURL"},
	},
	{
		Key:     "hr.request_approved",
		Subject: `Leave approved: {{.StartDate}} – {{.EndDate}}`,
		Body: `<p>Hello{{if .EmployeeName}} {{.EmployeeName}}{{end}},</p>
<p>Your <strong>{{.AbsenceType}}</strong> from {{.StartDate}} to {{.EndDate}} ({{.Days}} days) was approved{{if .ReviewerName}} by {{.ReviewerName}}{{end}}.</p>
<p><a href="{{.RequestURL}}">Open the request</a></p>
<p>If the link does not open, copy this address into your browser:<br>{{.RequestURL}}</p>
<p lang="bg">Вашият отпуск от {{.StartDate}} до {{.EndDate}} е одобрен.</p>`,
		Variables: []string{"EmployeeName", "AbsenceType", "StartDate", "EndDate", "Days", "ReviewerName", "RequestURL"},
		Required:  []string{"AbsenceType", "StartDate", "EndDate", "Days", "RequestURL"},
	},
	{
		Key:     "hr.request_rejected",
		Subject: `Leave rejected: {{.StartDate}} – {{.EndDate}}`,
		Body: `<p>Hello{{if .EmployeeName}} {{.EmployeeName}}{{end}},</p>
<p>Your <strong>{{.AbsenceType}}</strong> from {{.StartDate}} to {{.EndDate}} ({{.Days}} days) was rejected{{if .ReviewerName}} by {{.ReviewerName}}{{end}}.</p>
{{if .ReviewNotes}}<p>Notes:</p>
<blockquote>{{.ReviewNotes}}</blockquote>
{{end}}<p><a href="{{.RequestURL}}">Open the request</a></p>
<p>If the link does not open, copy this address into your browser:<br>{{.RequestURL}}</p>
<p lang="bg">Вашата заявка за отпуск от {{.StartDate}} до {{.EndDate}} е отхвърлена.</p>`,
		Variables: []string{"EmployeeName", "AbsenceType", "StartDate", "EndDate", "Days", "ReviewerName", "ReviewNotes", "RequestURL"},
		Required:  []string{"AbsenceType", "StartDate", "EndDate", "Days", "RequestURL"},
	},
	{
		Key:     "hr.request_revoked",
		Subject: `Leave revoked: {{.StartDate}} – {{.EndDate}}`,
		Body: `<p>Hello{{if .EmployeeName}} {{.EmployeeName}}{{end}},</p>
<p>Your approved <strong>{{.AbsenceType}}</strong> from {{.StartDate}} to {{.EndDate}} ({{.Days}} days) was revoked{{if .ReviewerName}} by {{.ReviewerName}}{{end}}. The days are back in your allowance.</p>
{{if .Reason}}<p>Reason given:</p>
<blockquote>{{.Reason}}</blockquote>
{{end}}<p><a href="{{.RequestURL}}">Open the request</a></p>
<p>If the link does not open, copy this address into your browser:<br>{{.RequestURL}}</p>
<p lang="bg">Одобреният ви отпуск от {{.StartDate}} до {{.EndDate}} е отменен.</p>`,
		Variables: []string{"EmployeeName", "AbsenceType", "StartDate", "EndDate", "Days", "ReviewerName", "Reason", "RequestURL"},
		Required:  []string{"AbsenceType", "StartDate", "EndDate", "Days", "RequestURL"},
	},
	{
		Key:     "hr.signing_failed",
		Subject: `Leave form not signed: {{.EmployeeName}}, {{.StartDate}} – {{.EndDate}}`,
		Body: `<p>Hello{{if .RecipientName}} {{.RecipientName}}{{end}},</p>
<p>The leave form for {{.EmployeeName}}'s <strong>{{.AbsenceType}}</strong> from {{.StartDate}} to {{.EndDate}} was not signed ({{.Outcome}}). The request is pending again and needs a new review.</p>
<p><a href="{{.RequestURL}}">Open the request</a></p>
<p>If the link does not open, copy this address into your browser:<br>{{.RequestURL}}</p>
<p lang="bg">Формулярът за отпуск не е подписан; заявката отново очаква преглед.</p>`,
		Variables: []string{"RecipientName", "EmployeeName", "AbsenceType", "StartDate", "EndDate", "Outcome", "RequestURL"},
		Required:  []string{"EmployeeName", "AbsenceType", "StartDate", "EndDate", "Outcome", "RequestURL"},
	},
	{
		Key:     "hr.allowance_overdrawn",
		Subject: `Allowance overdrawn: {{.EmployeeName}}, {{.Year}}`,
		Body: `<p>Hello,</p>
<p>An approved <strong>{{.AbsenceType}}</strong> left {{.EmployeeName}} with {{.Remaining}} days in {{.Year}}. Check the allowance.</p>
<p><a href="{{.RequestURL}}">Open the request</a></p>
<p>If the link does not open, copy this address into your browser:<br>{{.RequestURL}}</p>
<p lang="bg">Полагаемият отпуск на {{.EmployeeName}} за {{.Year}} е надвишен.</p>`,
		Variables: []string{"EmployeeName", "AbsenceType", "Year", "Remaining", "RequestURL"},
		Required:  []string{"EmployeeName", "AbsenceType", "Year", "Remaining", "RequestURL"},
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
