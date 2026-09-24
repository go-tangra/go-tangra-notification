package notify

import (
	"context"
	"errors"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/render"
	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Templates manages notification templates.
type Templates struct {
	st    repo.Store
	az    *authz.Authz
	audit *audit.Writer
}

// NewTemplates wires the service.
func NewTemplates(st repo.Store, az *authz.Authz, aw *audit.Writer) *Templates {
	return &Templates{st: st, az: az, audit: aw}
}

// TemplateInput is a create/update request.
type TemplateInput struct {
	Name      string
	ChannelID string
	Subject   string
	Body      string
	Variables []string
	IsDefault bool
}

// TemplateView is a template as returned to clients.
type TemplateView struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ChannelID   *string           `json:"channel_id"`
	ChannelName string            `json:"channel_name"`
	ChannelType string            `json:"channel_type"`
	Subject     string            `json:"subject"`
	Body        string            `json:"body"`
	Variables   []string          `json:"variables"`
	IsDefault   bool              `json:"is_default"`
	CreatedBy   string            `json:"created_by"`
	UpdatedBy   string            `json:"updated_by"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Permissions authz.Permissions `json:"permissions"`
}

func templateView(row store.Template, p authz.Permissions) TemplateView {
	vars := row.Variables
	if vars == nil {
		vars = []string{}
	}
	return TemplateView{ID: row.ID, Name: row.Name, ChannelID: row.ChannelID, ChannelName: row.ChannelName, ChannelType: row.ChannelType, Subject: row.Subject, Body: row.Body,
		Variables: vars, IsDefault: row.IsDefault, CreatedBy: strp(row.CreatedBy), UpdatedBy: strp(row.UpdatedBy), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Permissions: p}
}

// KindFor maps a channel type to the body rendering kind.
func KindFor(channelType string) render.Kind {
	if channelType == channel.TypeEmail {
		return render.KindHTML
	}
	return render.KindText
}

// validationError maps render failures to client-safe details.
func validationError(err error) error {
	var se *render.SyntaxError
	var ue *render.UndeclaredError
	var ee *render.ExecError
	switch {
	case errors.As(err, &se):
		return invalid("template syntax error", map[string]any{"where": se.Where, "position": se.Position, "message": se.Msg})
	case errors.As(err, &ue):
		return invalid("template references an undeclared variable", map[string]any{"variable": ue.Variable})
	case errors.As(err, &ee):
		return invalid("template failed to render", map[string]any{"message": ee.Msg})
	case errors.Is(err, render.ErrVariable), errors.Is(err, render.ErrTooLarge), errors.Is(err, render.ErrOutputSize), errors.Is(err, render.ErrTimeout):
		return invalid(err.Error(), map[string]any{"message": err.Error()})
	}
	return err
}

// validate parses and checks the template against its declared variables.
func (t *Templates) validate(in TemplateInput, channelType string) error {
	if len(in.Name) == 0 || len(in.Name) > 100 {
		return invalid("name must be 1-100 characters", map[string]any{"field": "name"})
	}
	if len(in.Variables) > 50 {
		return invalid("at most 50 variables", map[string]any{"field": "variables"})
	}
	c, err := render.Parse(in.Subject, in.Body, KindFor(channelType))
	if err != nil {
		return validationError(err)
	}
	if err := c.Validate(in.Variables); err != nil {
		return validationError(err)
	}
	return nil
}

// Create stores a template bound to a channel the caller may read; the
// creator becomes owner; a default template replaces the channel's previous default.
func (t *Templates) Create(ctx context.Context, s authz.Subjects, in TemplateInput) (TemplateView, error) {
	if in.ChannelID == "" {
		return TemplateView{}, invalid("channel_id is required", map[string]any{"field": "channel_id"})
	}
	if _, err := t.az.Require(ctx, s, authz.Channel, in.ChannelID, authz.Read); err != nil {
		return TemplateView{}, err
	}
	ch, err := t.st.GetChannel(ctx, s.TenantID, in.ChannelID)
	if err != nil {
		return TemplateView{}, err
	}
	if err := t.validate(in, ch.Type); err != nil {
		return TemplateView{}, err
	}
	id := store.NewID()
	cid := in.ChannelID
	row := store.Template{ID: id, TenantID: s.TenantID, Name: in.Name, ChannelID: &cid, ChannelType: ch.Type, Subject: in.Subject, Body: in.Body, Variables: in.Variables, IsDefault: in.IsDefault, CreatedBy: userPtr(s), UpdatedBy: userPtr(s)}
	err = t.st.Atomic(ctx, s.TenantID, func(tx repo.Store) error {
		if in.IsDefault {
			if err := tx.ClearDefaultTemplate(ctx, s.TenantID, cid, id); err != nil {
				return err
			}
		}
		if err := tx.InsertTemplate(ctx, row); err != nil {
			return err
		}
		return authz.New(tx, t.audit).GrantOwner(ctx, s.TenantID, authz.Template, id, s.UserID)
	})
	if err != nil {
		return TemplateView{}, err
	}
	t.emit(audit.Event{Type: audit.TemplateCreated, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "template", SubjectID: id, Outcome: "ok",
		Details: map[string]any{"name": in.Name, "channel_id": in.ChannelID, "is_default": in.IsDefault, "variables": len(in.Variables)}})
	return t.Get(ctx, s, id)
}

// Get returns a template the caller may read.
func (t *Templates) Get(ctx context.Context, s authz.Subjects, id string) (TemplateView, error) {
	d, err := t.az.Require(ctx, s, authz.Template, id, authz.Read)
	if err != nil {
		return TemplateView{}, err
	}
	row, err := t.st.GetTemplate(ctx, s.TenantID, id)
	if err != nil {
		return TemplateView{}, err
	}
	return templateView(row, d.Permissions), nil
}

// List returns the templates the caller may read, paged by name.
func (t *Templates) List(ctx context.Context, s authz.Subjects, channelID *string, q, cursor string, limit int) ([]TemplateView, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ids, all, err := t.az.ReadableIDs(ctx, s, authz.Template)
	if err != nil {
		return nil, "", err
	}
	out := []TemplateView{}
	after := cursor
	for len(out) < limit {
		rows, err := t.st.ListTemplates(ctx, s.TenantID, channelID, q, after, limit)
		if err != nil {
			return nil, "", err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			after = row.Name
			if !all && !ids[row.ID] {
				continue
			}
			p, err := t.az.PermissionsOn(ctx, s, authz.Template, row.ID)
			if err != nil {
				return nil, "", err
			}
			out = append(out, templateView(row, p))
			if len(out) == limit {
				break
			}
		}
		if len(rows) < limit {
			break
		}
	}
	next := ""
	if len(out) == limit {
		next = out[len(out)-1].Name
	}
	return out, next, nil
}

// Update rewrites a template the caller may write; re-binding needs read on
// the new channel.
func (t *Templates) Update(ctx context.Context, s authz.Subjects, id string, in TemplateInput) (TemplateView, error) {
	if _, err := t.az.Require(ctx, s, authz.Template, id, authz.Write); err != nil {
		return TemplateView{}, err
	}
	row, err := t.st.GetTemplate(ctx, s.TenantID, id)
	if err != nil {
		return TemplateView{}, err
	}
	if in.ChannelID == "" {
		return TemplateView{}, invalid("channel_id is required", map[string]any{"field": "channel_id"})
	}
	if in.ChannelID != strp(row.ChannelID) {
		if _, err := t.az.Require(ctx, s, authz.Channel, in.ChannelID, authz.Read); err != nil {
			return TemplateView{}, err
		}
	}
	ch, err := t.st.GetChannel(ctx, s.TenantID, in.ChannelID)
	if err != nil {
		return TemplateView{}, err
	}
	if err := t.validate(in, ch.Type); err != nil {
		return TemplateView{}, err
	}
	cid := in.ChannelID
	row.Name, row.ChannelID, row.ChannelType, row.Subject, row.Body, row.Variables, row.IsDefault, row.UpdatedBy = in.Name, &cid, ch.Type, in.Subject, in.Body, in.Variables, in.IsDefault, userPtr(s)
	err = t.st.Atomic(ctx, s.TenantID, func(tx repo.Store) error {
		if in.IsDefault {
			if err := tx.ClearDefaultTemplate(ctx, s.TenantID, cid, id); err != nil {
				return err
			}
		}
		return tx.UpdateTemplate(ctx, row)
	})
	if err != nil {
		return TemplateView{}, err
	}
	t.emit(audit.Event{Type: audit.TemplateUpdated, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "template", SubjectID: id, Outcome: "ok",
		Details: map[string]any{"name": in.Name, "channel_id": in.ChannelID, "is_default": in.IsDefault}})
	return t.Get(ctx, s, id)
}

// Delete removes a template the caller may delete with its grants.
func (t *Templates) Delete(ctx context.Context, s authz.Subjects, id string) error {
	if _, err := t.az.Require(ctx, s, authz.Template, id, authz.Delete); err != nil {
		return err
	}
	row, err := t.st.GetTemplate(ctx, s.TenantID, id)
	if err != nil {
		return err
	}
	err = t.st.Atomic(ctx, s.TenantID, func(tx repo.Store) error {
		if err := tx.DeleteTemplate(ctx, s.TenantID, id); err != nil {
			return err
		}
		return authz.New(tx, t.audit).DropResource(ctx, s.TenantID, authz.Template, id)
	})
	if err != nil {
		return err
	}
	t.emit(audit.Event{Type: audit.TemplateDeleted, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "template", SubjectID: id, Outcome: "ok", Details: map[string]any{"name": row.Name}})
	return nil
}

// PreviewInput is a preview request: a saved template (read required) or an
// unsaved draft; subject/body override the saved ones when given.
type PreviewInput struct {
	TemplateID  string
	ChannelType string
	Subject     string
	Body        string
	Variables   []string
	Values      map[string]string
}

// Preview renders without delivering or logging (audited as a preview).
func (t *Templates) Preview(ctx context.Context, s authz.Subjects, in PreviewInput) (subject, body string, err error) {
	kind := KindFor(in.ChannelType)
	if in.TemplateID != "" {
		if _, err := t.az.Require(ctx, s, authz.Template, in.TemplateID, authz.Read); err != nil {
			return "", "", err
		}
		row, err := t.st.GetTemplate(ctx, s.TenantID, in.TemplateID)
		if err != nil {
			return "", "", err
		}
		kind = KindFor(row.ChannelType)
		if in.Subject == "" && in.Body == "" {
			in.Subject, in.Body = row.Subject, row.Body
		}
		if in.Variables == nil {
			in.Variables = row.Variables
		}
	}
	if len(in.Variables) > 50 {
		return "", "", invalid("at most 50 variables", map[string]any{"field": "variables"})
	}
	values := in.Values
	if values == nil {
		values = map[string]string{}
	}
	for _, v := range in.Variables {
		if _, ok := values[v]; !ok {
			values[v] = ""
		}
	}
	subject, body, err = render.Preview(ctx, in.Subject, in.Body, kind, in.Variables, values)
	if err != nil {
		return "", "", validationError(err)
	}
	t.emit(audit.Event{Type: audit.TemplatePreviewed, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "template", SubjectID: in.TemplateID, Outcome: "ok"})
	return subject, body, nil
}

func (t *Templates) emit(e audit.Event) {
	if t.audit != nil {
		_ = t.audit.Emit(e)
	}
}
