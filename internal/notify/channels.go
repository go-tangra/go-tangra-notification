package notify

import (
	"context"
	"errors"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Channels manages notification channels.
type Channels struct {
	st    repo.Store
	env   *sealed.Envelope
	reg   *channel.Registry
	az    *authz.Authz
	audit *audit.Writer
	now   func() time.Time
	// platform delivers through the managed channel (nil = the registry's email provider).
	platform channel.Provider
}

// NewChannels wires the service.
func NewChannels(st repo.Store, env *sealed.Envelope, reg *channel.Registry, az *authz.Authz, aw *audit.Writer) *Channels {
	return &Channels{st: st, env: env, reg: reg, az: az, audit: aw, now: time.Now}
}

// ChannelInput is a create/update request.
type ChannelInput struct {
	Name      string
	Type      string
	Settings  sealed.Settings
	Enabled   bool
	IsDefault bool
}

// ChannelView is a channel as returned to clients (settings redacted).
type ChannelView struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Settings      sealed.Settings   `json:"settings"`
	Enabled       bool              `json:"enabled"`
	IsDefault     bool              `json:"is_default"`
	Managed       bool              `json:"managed"` // created from configuration: read-only
	TemplateCount int               `json:"template_count"`
	CreatedBy     string            `json:"created_by"`
	UpdatedBy     string            `json:"updated_by"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Permissions   authz.Permissions `json:"permissions"`
}

func (c *Channels) view(row store.Channel, public sealed.Settings, p authz.Permissions) ChannelView {
	if public == nil {
		public = sealed.Settings{}
	}
	if row.Managed { // configuration owns it: only read, use (and test) remain
		p.Write, p.Delete, p.Share = false, false, false
	}
	return ChannelView{ID: row.ID, Name: row.Name, Type: row.Type, Settings: public, Enabled: row.Enabled, IsDefault: row.IsDefault, Managed: row.Managed, TemplateCount: row.TemplateCount,
		CreatedBy: strp(row.CreatedBy), UpdatedBy: strp(row.UpdatedBy), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Permissions: p}
}

// redacted decodes the public settings of a row and marks credentials as set.
func (c *Channels) redacted(row store.Channel) sealed.Settings {
	public, err := sealed.Decode(row.SettingsPublic)
	if err != nil {
		public = sealed.Settings{}
	}
	return public
}

func (c *Channels) validate(in ChannelInput, allowDefaultChange bool) error {
	if len(in.Name) == 0 || len(in.Name) > 100 {
		return invalid("name must be 1-100 characters", map[string]any{"field": "name"})
	}
	if !channel.ValidType(in.Type) {
		return invalid("unknown channel type", map[string]any{"field": "type"})
	}
	if in.Settings == nil {
		return invalid("settings are required", map[string]any{"field": "settings"})
	}
	p, err := c.reg.Get(in.Type)
	if err != nil {
		return invalid("unknown channel type", map[string]any{"field": "type"})
	}
	if err := p.Validate(in.Settings); err != nil {
		return invalid(err.Error(), map[string]any{"field": "settings"})
	}
	_ = allowDefaultChange
	return nil
}

// seal encodes and encrypts the clear settings, returning the sealed blob and
// the public projection.
func (c *Channels) seal(id, typ string, s sealed.Settings) (blob, public []byte, err error) {
	clear, err := sealed.Encode(s)
	if err != nil {
		return nil, nil, invalid("settings exceed 8 KiB", map[string]any{"field": "settings"})
	}
	if blob, err = c.env.Seal(clear, sealed.AD(id)); err != nil {
		return nil, nil, err
	}
	// The redacted projection keeps "__set__" markers so listings show what is configured.
	if public, err = sealed.Encode(sealed.Redact(s, c.reg.SecretFields(typ))); err != nil {
		return nil, nil, err
	}
	return blob, public, nil
}

// Create stores a channel; the creator becomes its owner. A default channel
// takes the flag from the previous default of the type and carries the
// tenant-wide use grant (research R7).
func (c *Channels) Create(ctx context.Context, s authz.Subjects, in ChannelInput) (ChannelView, error) {
	if err := c.validate(in, true); err != nil {
		return ChannelView{}, err
	}
	id := store.NewID()
	blob, public, err := c.seal(id, in.Type, in.Settings)
	if err != nil {
		return ChannelView{}, err
	}
	row := store.Channel{ID: id, TenantID: s.TenantID, Name: in.Name, Type: in.Type, SettingsSealed: blob, SettingsPublic: public, Enabled: in.Enabled, IsDefault: in.IsDefault, CreatedBy: userPtr(s), UpdatedBy: userPtr(s)}
	err = c.st.Atomic(ctx, s.TenantID, func(tx repo.Store) error {
		if in.IsDefault {
			if err := c.moveDefault(ctx, tx, s.TenantID, in.Type, id); err != nil {
				return err
			}
		}
		if err := tx.InsertChannel(ctx, row); err != nil {
			return err
		}
		if err := authz.New(tx, c.audit).GrantOwner(ctx, s.TenantID, authz.Channel, id, s.UserID); err != nil {
			return err
		}
		if in.IsDefault {
			return authz.New(tx, c.audit).SetTenantUse(ctx, s.TenantID, id, true)
		}
		return nil
	})
	if err != nil {
		return ChannelView{}, err
	}
	c.emit(audit.Event{Type: audit.ChannelCreated, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "channel", SubjectID: id, Outcome: "ok",
		Details: map[string]any{"name": in.Name, "type": in.Type, "enabled": in.Enabled, "is_default": in.IsDefault}})
	return c.Get(ctx, s, id)
}

// moveDefault clears the default flag (and the tenant use grant) of the
// previous default channel of the type.
func (c *Channels) moveDefault(ctx context.Context, tx repo.Store, tenantID, typ, newID string) error {
	prev, err := tx.ListChannels(ctx, tenantID, typ, "", 1000)
	if err != nil {
		return err
	}
	for _, p := range prev {
		if p.IsDefault && p.ID != newID {
			if err := authz.New(tx, c.audit).SetTenantUse(ctx, tenantID, p.ID, false); err != nil {
				return err
			}
		}
	}
	return tx.ClearDefaultChannel(ctx, tenantID, typ, newID)
}

// Get returns a channel the caller may read.
func (c *Channels) Get(ctx context.Context, s authz.Subjects, id string) (ChannelView, error) {
	d, err := c.az.Require(ctx, s, authz.Channel, id, authz.Read)
	if err != nil {
		return ChannelView{}, err
	}
	row, err := c.st.GetChannel(ctx, s.TenantID, id)
	if err != nil {
		return ChannelView{}, err
	}
	return c.view(row, c.redacted(row), d.Permissions), nil
}

// List returns the channels the caller may read, paged by name.
func (c *Channels) List(ctx context.Context, s authz.Subjects, typ, cursor string, limit int) ([]ChannelView, string, error) {
	if typ != "" && !channel.ValidType(typ) {
		return nil, "", invalid("unknown channel type", map[string]any{"field": "type"})
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ids, all, err := c.az.ReadableIDs(ctx, s, authz.Channel)
	if err != nil {
		return nil, "", err
	}
	out := []ChannelView{}
	after := cursor
	for len(out) < limit {
		rows, err := c.st.ListChannels(ctx, s.TenantID, typ, after, limit)
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
			p, err := c.az.PermissionsOn(ctx, s, authz.Channel, row.ID)
			if err != nil {
				return nil, "", err
			}
			out = append(out, c.view(row, c.redacted(row), p))
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

// Update rewrites a channel the caller may write; credential fields sent
// as the marker (or omitted) keep their stored value; the type is fixed.
func (c *Channels) Update(ctx context.Context, s authz.Subjects, id string, in ChannelInput) (ChannelView, error) {
	if _, err := c.az.Require(ctx, s, authz.Channel, id, authz.Write); err != nil {
		return ChannelView{}, err
	}
	row, err := c.st.GetChannel(ctx, s.TenantID, id)
	if err != nil {
		return ChannelView{}, err
	}
	if row.Managed {
		return ChannelView{}, ErrManagedChannel
	}
	if in.Type != "" && in.Type != row.Type {
		return ChannelView{}, invalid("the channel type cannot change", map[string]any{"field": "type"})
	}
	in.Type = row.Type
	stored, err := c.open(row)
	if err != nil {
		return ChannelView{}, err
	}
	if in.Settings == nil {
		in.Settings = sealed.Settings{}
	}
	in.Settings = sealed.Merge(stored, in.Settings, c.reg.SecretFields(row.Type))
	if err := c.validate(in, true); err != nil {
		return ChannelView{}, err
	}
	blob, public, err := c.seal(id, row.Type, in.Settings)
	if err != nil {
		return ChannelView{}, err
	}
	wasDefault := row.IsDefault
	row.Name, row.SettingsSealed, row.SettingsPublic, row.Enabled, row.IsDefault, row.UpdatedBy = in.Name, blob, public, in.Enabled, in.IsDefault, userPtr(s)
	err = c.st.Atomic(ctx, s.TenantID, func(tx repo.Store) error {
		if in.IsDefault && !wasDefault {
			if err := c.moveDefault(ctx, tx, s.TenantID, row.Type, id); err != nil {
				return err
			}
		}
		if err := tx.UpdateChannel(ctx, row); err != nil {
			return err
		}
		if in.IsDefault != wasDefault {
			return authz.New(tx, c.audit).SetTenantUse(ctx, s.TenantID, id, in.IsDefault)
		}
		return nil
	})
	if err != nil {
		return ChannelView{}, err
	}
	c.emit(audit.Event{Type: audit.ChannelUpdated, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "channel", SubjectID: id, Outcome: "ok",
		Details: map[string]any{"name": in.Name, "enabled": in.Enabled, "is_default": in.IsDefault}})
	return c.Get(ctx, s, id)
}

// Delete removes a channel the caller may delete; templates referencing it
// refuse the deletion with their count.
func (c *Channels) Delete(ctx context.Context, s authz.Subjects, id string) error {
	if _, err := c.az.Require(ctx, s, authz.Channel, id, authz.Delete); err != nil {
		return err
	}
	row, err := c.st.GetChannel(ctx, s.TenantID, id)
	if err != nil {
		return err
	}
	if row.Managed {
		return ErrManagedChannel
	}
	if row.TemplateCount > 0 {
		return &InUseError{What: "templates", Count: row.TemplateCount}
	}
	err = c.st.Atomic(ctx, s.TenantID, func(tx repo.Store) error {
		if err := tx.DeleteChannel(ctx, s.TenantID, id); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return &InUseError{What: "templates", Count: 1}
			}
			return err
		}
		return authz.New(tx, c.audit).DropResource(ctx, s.TenantID, authz.Channel, id)
	})
	if err != nil {
		return err
	}
	c.emit(audit.Event{Type: audit.ChannelDeleted, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "channel", SubjectID: id, Outcome: "ok", Details: map[string]any{"name": row.Name}})
	return nil
}

// open decrypts a channel's settings (never leaves the service).
func (c *Channels) open(row store.Channel) (sealed.Settings, error) {
	clear, err := c.env.Open(row.SettingsSealed, sealed.AD(row.ID))
	if err != nil {
		return nil, err
	}
	return sealed.Decode(clear)
}

// Resolve loads a channel for sending: the row, its clear settings and the
// provider. The caller checks permissions.
func (c *Channels) Resolve(ctx context.Context, tenantID, id string) (store.Channel, sealed.Settings, channel.Provider, error) {
	row, err := c.st.GetChannel(ctx, tenantID, id)
	if err != nil {
		return store.Channel{}, nil, nil, err
	}
	settings, err := c.open(row)
	if err != nil {
		return store.Channel{}, nil, nil, err
	}
	p, err := c.provider(row)
	if err != nil {
		return store.Channel{}, nil, nil, err
	}
	return row, settings, p, nil
}

// SecretFields of a channel type (error scrubbing).
func (c *Channels) SecretFields(typ string) []string { return c.reg.SecretFields(typ) }

func (c *Channels) emit(e audit.Event) {
	if c.audit != nil {
		_ = c.audit.Emit(e)
	}
}
