package notify

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel/email"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// tP is the platform tenant of these tests.
const tP = "00000000-0000-0000-0000-000000000001"

func platformAdmin() authz.Subjects {
	return authz.Subjects{TenantID: tP, UserID: uA, Roles: []string{"admin"}}
}

func relay() *PlatformEmail {
	return &PlatformEmail{Host: "mx01.example.net", Port: 587, TLS: "starttls", Username: "tangra@example.net", Password: "NOTIF-MARKER-PW-relay", From: "tangra@example.net"}
}

// TestEnsurePlatformChannel covers the start-up upsert (T013): created once,
// updated when the configuration changes, untouched when equal, disabled
// (not deleted) when the block is removed; the password is sealed and
// never shows in views, public settings or audit.
func TestEnsurePlatformChannel(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	platform := &fakeProvider{kind: "email", secret: []string{"password"}}
	f.ch.SetPlatformProvider(platform)

	// No block, no channel: nothing to do.
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, nil); err != nil || r != PlatformAbsent {
		t.Fatalf("absent %q %v", r, err)
	}
	// A previous default email channel of the platform tenant gives up the flag.
	prev, err := f.ch.Create(ctx, platformAdmin(), ChannelInput{Name: "old relay", Type: "email", Settings: emailSettings("x"), Enabled: true, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.ch.EnsurePlatformChannel(ctx, tP, relay())
	if err != nil || r != PlatformCreated {
		t.Fatalf("create %q %v", r, err)
	}
	row, err := f.ms.ManagedChannel(ctx, tP)
	if err != nil || row.Name != PlatformChannelName || row.Type != "email" || !row.Enabled || !row.IsDefault || !row.Managed {
		t.Fatalf("row %v %+v", err, row)
	}
	if bytes.Contains(row.SettingsSealed, []byte("NOTIF-MARKER")) || bytes.Contains(row.SettingsPublic, []byte("NOTIF-MARKER")) {
		t.Fatal("password stored in clear")
	}
	_, settings, provider, err := f.ch.Resolve(ctx, tP, row.ID)
	if err != nil || settings["password"] != "NOTIF-MARKER-PW-relay" || settings["host"] != "mx01.example.net" || settings["port"] != float64(587) || settings["tls"] != "starttls" || provider != platform {
		t.Fatalf("resolve %v %v %v", err, settings, provider)
	}
	if old, _ := f.ch.Get(ctx, platformAdmin(), prev.ID); old.IsDefault {
		t.Fatal("previous default kept the flag")
	}
	// Members of the platform tenant (and modules) may use it: tenant-wide use.
	pm := authz.Subjects{TenantID: tP, UserID: uB, Roles: []string{"member"}}
	if d, _ := f.az.Check(ctx, pm, authz.Channel, row.ID, authz.Use); !d.Allowed {
		t.Fatal("platform channel not usable tenant-wide")
	}
	v, err := f.ch.Get(ctx, platformAdmin(), row.ID)
	if err != nil || !v.Managed || v.Settings["password"] != sealed.Marker || v.Permissions.Write || v.Permissions.Delete || v.Permissions.Share || !v.Permissions.Read || !v.Permissions.Use {
		t.Fatalf("view %+v %v", v, err)
	}
	// Equal configuration: unchanged, nothing written.
	before := f.ms.Channels[row.ID]
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err != nil || r != PlatformUnchanged {
		t.Fatalf("unchanged %q %v", r, err)
	}
	if !bytes.Equal(before.SettingsSealed, f.ms.Channels[row.ID].SettingsSealed) {
		t.Fatal("unchanged configuration rewrote the channel")
	}
	// A changed relay updates the same channel.
	changed := relay()
	changed.Port, changed.From, changed.ReplyTo = 465, "Tangra <noreply@example.net>", "ops@example.net"
	changed.TLS = "implicit"
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, changed); err != nil || r != PlatformUpdated {
		t.Fatalf("update %q %v", r, err)
	}
	_, settings, _, _ = f.ch.Resolve(ctx, tP, row.ID)
	if settings["port"] != float64(465) || settings["tls"] != "implicit" || settings["reply_to"] != "ops@example.net" || settings["from"] != "Tangra <noreply@example.net>" {
		t.Fatalf("updated settings %v", settings)
	}
	// Credentials dropped from the configuration are dropped from the channel.
	anon := relay()
	anon.Username, anon.Password = "", ""
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, anon); err != nil || r != PlatformUpdated {
		t.Fatalf("anon %q %v", r, err)
	}
	if _, settings, _, _ = f.ch.Resolve(ctx, tP, row.ID); settings["password"] != nil || settings["username"] != nil {
		t.Fatalf("credentials kept %v", settings)
	}
	// An operator re-enabling another default does not survive a restart: the platform channel takes the flag back.
	if _, err := f.ch.Update(ctx, platformAdmin(), prev.ID, ChannelInput{Name: "old relay", Settings: emailSettings("x"), Enabled: true, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, anon); err != nil || r != PlatformUpdated {
		t.Fatalf("retake default %q %v", r, err)
	}
	if got, _ := f.ms.ManagedChannel(ctx, tP); !got.IsDefault {
		t.Fatal("default not taken back")
	}
	// Removing the block disables the channel (log rows may reference it); a second pass is a no-op.
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, nil); err != nil || r != PlatformDisabled {
		t.Fatalf("disable %q %v", r, err)
	}
	if got, _ := f.ms.ManagedChannel(ctx, tP); got.Enabled || !got.Managed {
		t.Fatalf("disabled %+v", got)
	}
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, nil); err != nil || r != PlatformAbsent {
		t.Fatalf("second disable %q %v", r, err)
	}
	// Configuring it again re-enables it.
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err != nil || r != PlatformUpdated {
		t.Fatalf("re-enable %q %v", r, err)
	}
	// Audit: system actor, no credential anywhere.
	f.aw.Flush()
	if n := len(f.ms.AuditEvents(tP, string(audit.ChannelCreated))); n != 2 { // "old relay" + platform
		t.Fatalf("created events %d", n)
	}
	for _, e := range f.ms.Audit {
		if strings.Contains(string(e.Details), "NOTIF-MARKER") || strings.Contains(e.Reason, "NOTIF-MARKER") {
			t.Fatalf("credential in audit %+v", e)
		}
	}
	upd := f.ms.AuditEvents(tP, string(audit.ChannelUpdated))
	if len(upd) == 0 || upd[len(upd)-1].ActorKind != "system" {
		t.Fatalf("updated events %+v", upd)
	}
}

func TestEnsurePlatformChannelRefusals(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	// Settings the email provider refuses are refused: tls none without the
	// platform opt-out, credentials over plaintext, a missing host.
	f.ch.SetPlatformProvider(&email.Provider{})
	for _, pe := range []*PlatformEmail{{Port: 25, From: "a@b.c"}, {Host: "h", Port: 25, TLS: "none", From: "a@b.c"}} {
		if _, err := f.ch.EnsurePlatformChannel(ctx, tP, pe); err == nil {
			t.Fatalf("invalid settings accepted: %+v", pe)
		}
	}
	f.ch.SetPlatformProvider(&email.Provider{AllowPlaintext: true})
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, &PlatformEmail{Host: "h", Port: 25, TLS: "none", Username: "u", Password: "p", From: "a@b.c"}); err == nil {
		t.Fatal("credentials over plaintext accepted")
	}
	f.ch.SetPlatformProvider(nil)
	// Store failures surface.
	f.ms.FailOn("ManagedChannel", errors.New("db"))
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err == nil {
		t.Fatal("lookup error swallowed")
	}
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, nil); err == nil {
		t.Fatal("lookup error swallowed (absent)")
	}
	f.ms.FailOn("ManagedChannel", nil)
	f.ms.FailOn("InsertChannel", errors.New("db"))
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err == nil {
		t.Fatal("insert error swallowed")
	}
	f.ms.FailOn("InsertChannel", nil)
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err != nil {
		t.Fatal(err)
	}
	changed := relay()
	changed.Port = 2525
	f.ms.FailOn("UpdateChannel", errors.New("db"))
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, changed); err == nil {
		t.Fatal("update error swallowed")
	}
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, nil); err == nil {
		t.Fatal("disable error swallowed")
	}
	f.ms.FailOn("UpdateChannel", nil)
	// A blob that no longer opens (KEK changed) is re-sealed from the configuration.
	row, _ := f.ms.ManagedChannel(ctx, tP)
	other, _ := sealed.NewEnvelope(bytes.Repeat([]byte{9}, 32))
	row.SettingsSealed, _ = other.Seal([]byte(`{"host":"h"}`), sealed.AD(row.ID))
	f.ms.Channels[row.ID] = row
	if r, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err != nil || r != PlatformUpdated {
		t.Fatalf("reseal %q %v", r, err)
	}
	if _, s, _, err := f.ch.Resolve(ctx, tP, row.ID); err != nil || s["host"] != "mx01.example.net" {
		t.Fatalf("after reseal %v %v", s, err)
	}
}

// TestManagedChannelGuards: the managed channel is read-only in the API
// (T014): update and delete refused with ErrManagedChannel, a test send
// through it is allowed.
func TestManagedChannelGuards(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	if _, err := f.ch.EnsurePlatformChannel(ctx, tP, relay()); err != nil {
		t.Fatal(err)
	}
	row, _ := f.ms.ManagedChannel(ctx, tP)
	if _, err := f.ch.Update(ctx, platformAdmin(), row.ID, ChannelInput{Name: "mine", Settings: emailSettings("x"), Enabled: true}); !errors.Is(err, ErrManagedChannel) {
		t.Fatalf("update: %v", err)
	}
	if err := f.ch.Delete(ctx, platformAdmin(), row.ID); !errors.Is(err, ErrManagedChannel) {
		t.Fatalf("delete: %v", err)
	}
	// Members still get forbidden first (no write).
	pm := authz.Subjects{TenantID: tP, UserID: uB, Roles: []string{"member"}}
	if err := f.ch.Delete(ctx, pm, row.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member delete: %v", err)
	}
	if got, err := f.ms.GetChannel(ctx, tP, row.ID); err != nil || got.Name != PlatformChannelName {
		t.Fatalf("channel changed %v %+v", err, got)
	}
	v, err := f.snd.SendTest(ctx, platformAdmin(), row.ID, "ops@example.org", "c1")
	if err != nil || v.Status != "sent" || !v.Test {
		t.Fatalf("test send %+v %v", v, err)
	}
	// The list view carries the managed flag and the read-only permissions.
	list, _, _ := f.ch.List(ctx, platformAdmin(), "email", "", 10)
	if len(list) != 1 || !list[0].Managed || list[0].Permissions.Write || list[0].Permissions.Delete {
		t.Fatalf("list %+v", list)
	}
	if _, err := f.ms.GetChannel(ctx, tA, row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("platform channel visible in another tenant")
	}
}
