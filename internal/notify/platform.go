package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// PlatformChannelName names the configuration-managed channel.
const PlatformChannelName = "Platform email"

// ErrManagedChannel refuses API changes to the configuration-managed channel.
var ErrManagedChannel = errors.New("notify: the channel is managed by configuration")

// PlatformEmail is the relay setting behind the platform channel (config
// platform_email with the password already read from its secret file).
type PlatformEmail struct {
	Host     string
	Port     int
	TLS      string // implicit | starttls | none ("" = starttls)
	Username string
	Password string
	From     string
	ReplyTo  string
}

// settings is the channel settings object of the relay.
func (p *PlatformEmail) settings() sealed.Settings {
	s := sealed.Settings{"host": p.Host, "port": float64(p.Port), "tls": p.TLS, "from": p.From}
	if p.TLS == "" {
		s["tls"] = "starttls"
	}
	if p.Username != "" {
		s["username"], s["password"] = p.Username, p.Password
	}
	if p.ReplyTo != "" {
		s["reply_to"] = p.ReplyTo
	}
	return s
}

// PlatformResult reports what EnsurePlatformChannel did.
type PlatformResult string

// Results.
const (
	PlatformCreated   PlatformResult = "created"
	PlatformUpdated   PlatformResult = "updated"
	PlatformUnchanged PlatformResult = "unchanged"
	PlatformDisabled  PlatformResult = "disabled"
	PlatformAbsent    PlatformResult = "absent" // no configuration and no enabled channel
)

// SetPlatformProvider sets the email provider of the managed channel: it
// carries the platform_email plaintext opt-out, which tenant channels never
// get (their provider follows smtp.allow_plaintext).
func (c *Channels) SetPlatformProvider(p channel.Provider) { c.platform = p }

// provider returns the provider of a row.
func (c *Channels) provider(row store.Channel) (channel.Provider, error) {
	if row.Managed && row.Type == channel.TypeEmail && c.platform != nil {
		return c.platform, nil
	}
	return c.reg.Get(row.Type)
}

// EnsurePlatformChannel creates or updates the managed email channel of the
// platform tenant from the configuration (research D1): enabled, the
// tenant's default email channel with the tenant-wide use grant, password
// sealed like every channel credential. Without configuration an existing
// managed channel is disabled, never deleted (log entries reference it).
// Runs at start as the system, before anything is served.
func (c *Channels) EnsurePlatformChannel(ctx context.Context, tenantID string, pe *PlatformEmail) (PlatformResult, error) {
	row, err := c.st.ManagedChannel(ctx, tenantID)
	found := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if pe == nil {
		if !found || !row.Enabled {
			return PlatformAbsent, nil
		}
		row.Enabled = false
		if err := c.st.UpdateChannel(ctx, row); err != nil {
			return "", err
		}
		c.emitSystem(audit.ChannelUpdated, tenantID, row.ID, map[string]any{"name": row.Name, "enabled": false, "managed": true})
		return PlatformDisabled, nil
	}
	want := pe.settings()
	p, err := c.provider(store.Channel{Type: channel.TypeEmail, Managed: true})
	if err != nil {
		return "", err
	}
	if err := p.Validate(want); err != nil {
		return "", fmt.Errorf("notify: platform_email: %w", err)
	}
	if !found {
		id := store.NewID()
		blob, public, err := c.seal(id, channel.TypeEmail, want)
		if err != nil {
			return "", err
		}
		row = store.Channel{ID: id, TenantID: tenantID, Name: PlatformChannelName, Type: channel.TypeEmail, SettingsSealed: blob, SettingsPublic: public, Enabled: true, IsDefault: true, Managed: true}
		err = c.st.Atomic(ctx, tenantID, func(tx repo.Store) error {
			if err := c.moveDefault(ctx, tx, tenantID, channel.TypeEmail, id); err != nil {
				return err
			}
			if err := tx.InsertChannel(ctx, row); err != nil {
				return err
			}
			return authz.New(tx, c.audit).SetTenantUse(ctx, tenantID, id, true)
		})
		if err != nil {
			return "", err
		}
		c.emitSystem(audit.ChannelCreated, tenantID, id, map[string]any{"name": row.Name, "type": row.Type, "enabled": true, "is_default": true, "managed": true})
		return PlatformCreated, nil
	}
	if c.sameSettings(row, want) && row.Enabled && row.IsDefault && row.Name == PlatformChannelName {
		return PlatformUnchanged, nil
	}
	blob, public, err := c.seal(row.ID, channel.TypeEmail, want)
	if err != nil {
		return "", err
	}
	wasDefault := row.IsDefault
	row.Name, row.SettingsSealed, row.SettingsPublic, row.Enabled, row.IsDefault, row.UpdatedBy = PlatformChannelName, blob, public, true, true, nil
	err = c.st.Atomic(ctx, tenantID, func(tx repo.Store) error {
		if !wasDefault {
			if err := c.moveDefault(ctx, tx, tenantID, channel.TypeEmail, row.ID); err != nil {
				return err
			}
		}
		if err := tx.UpdateChannel(ctx, row); err != nil {
			return err
		}
		if !wasDefault {
			return authz.New(tx, c.audit).SetTenantUse(ctx, tenantID, row.ID, true)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	c.emitSystem(audit.ChannelUpdated, tenantID, row.ID, map[string]any{"name": row.Name, "enabled": true, "is_default": true, "managed": true})
	return PlatformUpdated, nil
}

// sameSettings compares the stored (sealed) settings with the wanted ones;
// a blob that no longer opens counts as different and is re-sealed.
func (c *Channels) sameSettings(row store.Channel, want sealed.Settings) bool {
	stored, err := c.open(row)
	if err != nil {
		return false
	}
	a, errA := sealed.Encode(stored)
	b, errB := sealed.Encode(want)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

func (c *Channels) emitSystem(t audit.EventType, tenantID, id string, details map[string]any) {
	c.emit(audit.Event{Type: t, TenantID: tenantID, ActorKind: "system", ActorID: "platform_email", SubjectKind: "channel", SubjectID: id, Outcome: "ok", Details: details})
}
