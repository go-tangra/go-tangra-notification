package notify

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-freya/freya/services/notification/internal/audit"
	"github.com/go-freya/freya/services/notification/internal/authz"
	"github.com/go-freya/freya/services/notification/internal/channel"
	"github.com/go-freya/freya/services/notification/internal/memstore"
	"github.com/go-freya/freya/services/notification/internal/sealed"
	"github.com/go-freya/freya/services/notification/internal/store"
)

const (
	tA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	tB = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c66"
	uA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c77"
	uB = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c88"
)

// fakeProvider records sends and can fail.
type fakeProvider struct {
	kind   string
	sent   []channel.Message
	fail   error
	secret []string
}

func (f *fakeProvider) Type() string { return f.kind }
func (f *fakeProvider) Validate(s sealed.Settings) error {
	if _, ok := s["host"]; !ok {
		return errors.New("host is required")
	}
	return nil
}
func (f *fakeProvider) Secret() []string { return f.secret }
func (f *fakeProvider) Send(_ context.Context, _ sealed.Settings, m channel.Message) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, m)
	return nil
}

type fakeLimiter struct {
	counts map[string]int
	err    error
}

func (l *fakeLimiter) Limited(_ context.Context, kind, subject string, limit int, _ time.Time) (bool, error) {
	if l.err != nil {
		return true, l.err
	}
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	l.counts[kind+":"+subject]++
	return limit > 0 && l.counts[kind+":"+subject] > limit, nil
}

type fx struct {
	ms      *memstore.Store
	aw      *audit.Writer
	az      *authz.Authz
	ch      *Channels
	tp      *Templates
	snd     *Sender
	email   *fakeProvider
	limiter *fakeLimiter
	now     time.Time
}

func newFx(t *testing.T) *fx {
	t.Helper()
	ms := memstore.New()
	aw := audit.NewWriter(ms, nil)
	t.Cleanup(aw.Close)
	az := authz.New(ms, aw)
	env, _ := sealed.NewEnvelope(bytes.Repeat([]byte{3}, 32))
	email := &fakeProvider{kind: "email", secret: []string{"password"}}
	reg := channel.NewRegistry(email, channel.Nop{Kind: "sms"}, channel.Nop{Kind: "slack"}, channel.Nop{Kind: "sse"})
	f := &fx{ms: ms, aw: aw, az: az, email: email, limiter: &fakeLimiter{}, now: time.Unix(1_700_000_000, 0)}
	f.ch = NewChannels(ms, env, reg, az, aw)
	f.tp = NewTemplates(ms, az, aw)
	f.snd = NewSender(ms, f.ch, f.tp, az, aw, f.limiter, Limits{PerTenant: 5, PerSender: 3})
	f.snd.SetClock(func() time.Time { return f.now })
	ms.Now = func() time.Time { return f.now }
	az.SetClock(func() time.Time { return f.now })
	return f
}

func admin() authz.Subjects {
	return authz.Subjects{TenantID: tA, UserID: uA, Roles: []string{"admin"}}
}
func member() authz.Subjects {
	return authz.Subjects{TenantID: tA, UserID: uB, Roles: []string{"member"}}
}

func emailSettings(pw string) sealed.Settings {
	return sealed.Settings{"host": "relay", "port": float64(587), "from": "noreply@example.org", "password": pw}
}

func (f *fx) channel(t *testing.T, name string, def bool) ChannelView {
	t.Helper()
	v, err := f.ch.Create(context.Background(), admin(), ChannelInput{Name: name, Type: "email", Settings: emailSettings("NOTIF-MARKER-PW-" + name), Enabled: true, IsDefault: def})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *fx) template(t *testing.T, name, channelID string) TemplateView {
	t.Helper()
	v, err := f.tp.Create(context.Background(), admin(), TemplateInput{Name: name, ChannelID: channelID, Subject: "Hello {{.Name}}", Body: "<p>Hi {{.Name}}, see {{.Link}}</p>", Variables: []string{"Name", "Link"}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestChannelsLifecycle(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	c := f.channel(t, "relay", true)
	if c.Settings["password"] != sealed.Marker || c.Settings["host"] != "relay" || !c.Permissions.Delete || !c.IsDefault {
		t.Fatalf("view %+v", c)
	}
	// Stored settings are sealed; the public projection never carries the password.
	row := f.ms.Channels[c.ID]
	if bytes.Contains(row.SettingsSealed, []byte("NOTIF-MARKER")) || bytes.Contains(row.SettingsPublic, []byte("NOTIF-MARKER")) {
		t.Fatal("credential in storage")
	}
	// The default carries tenant use (sharer: read of the redacted view, no write).
	if v, err := f.ch.Get(ctx, member(), c.ID); err != nil || v.Permissions.Write || v.Settings["password"] != sealed.Marker {
		t.Fatalf("member get: %+v %v", v, err)
	}
	if _, err := f.ch.Update(ctx, member(), c.ID, ChannelInput{Name: "x", Settings: emailSettings("x")}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member update: %v", err)
	}
	if d, _ := f.az.Check(ctx, member(), authz.Channel, c.ID, authz.Use); !d.Allowed {
		t.Fatal("default channel not usable by members")
	}
	// Validation refusals.
	for _, in := range []ChannelInput{
		{Name: "", Type: "email", Settings: emailSettings("x")},
		{Name: "x", Type: "pigeon", Settings: emailSettings("x")},
		{Name: "x", Type: "email"},
		{Name: "x", Type: "email", Settings: sealed.Settings{"port": float64(1)}},
		{Name: "x", Type: "email", Settings: sealed.Settings{"host": strings.Repeat("h", 9000)}},
	} {
		var ve *ValidationError
		if _, err := f.ch.Create(ctx, admin(), in); !errors.As(err, &ve) {
			t.Errorf("%+v: %v", in, err)
		}
	}
	if _, err := f.ch.Create(ctx, admin(), ChannelInput{Name: "RELAY", Type: "email", Settings: emailSettings("x")}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	// Second default of the type moves the flag and the tenant grant.
	c2 := f.channel(t, "relay2", true)
	c1, _ := f.ch.Get(ctx, admin(), c.ID)
	if c1.IsDefault || !c2.IsDefault {
		t.Fatal("default did not move")
	}
	if d, _ := f.az.Check(ctx, member(), authz.Channel, c.ID, authz.Use); d.Allowed {
		t.Fatal("old default still usable")
	}
	// Listing: admin sees both; member sees the default only (tenant grant); type filter and paging.
	list, next, err := f.ch.List(ctx, admin(), "", "", 1)
	if err != nil || len(list) != 1 || next == "" {
		t.Fatalf("page 1 %v %q %v", list, next, err)
	}
	list, next, _ = f.ch.List(ctx, admin(), "", next, 10)
	if len(list) != 1 || next != "" {
		t.Fatalf("page 2 %v %q", list, next)
	}
	if list, _, _ := f.ch.List(ctx, member(), "email", "", 10); len(list) != 1 || list[0].ID != c2.ID {
		t.Fatalf("member list %v", list)
	}
	if _, _, err := f.ch.List(ctx, admin(), "pigeon", "", 10); err == nil {
		t.Fatal("bad type filter")
	}
	// Update keeps the credential on marker/omit, clears on "", refuses a type change.
	up, err := f.ch.Update(ctx, admin(), c.ID, ChannelInput{Name: "relay-1", Settings: sealed.Settings{"host": "relay", "port": float64(25), "from": "a@b.c", "password": sealed.Marker}, Enabled: false})
	if err != nil || up.Name != "relay-1" || up.Settings["password"] != sealed.Marker || up.Enabled {
		t.Fatalf("update %+v %v", up, err)
	}
	_, settings, _, _ := f.ch.Resolve(ctx, tA, c.ID)
	if settings["password"] != "NOTIF-MARKER-PW-relay" || settings["port"] != float64(25) {
		t.Fatalf("stored after marker update %v", settings)
	}
	up, _ = f.ch.Update(ctx, admin(), c.ID, ChannelInput{Name: "relay-1", Settings: sealed.Settings{"host": "relay", "port": float64(25), "from": "a@b.c", "password": ""}, Enabled: true})
	if _, ok := up.Settings["password"]; ok {
		t.Fatal("password not cleared")
	}
	if _, err := f.ch.Update(ctx, admin(), c.ID, ChannelInput{Name: "relay-1", Type: "sms", Settings: emailSettings("x")}); err == nil {
		t.Fatal("type change accepted")
	}
	if _, err := f.ch.Update(ctx, admin(), c.ID, ChannelInput{Name: "", Settings: emailSettings("x")}); err == nil {
		t.Fatal("empty name accepted")
	}
	// Making c default again moves it back.
	if _, err := f.ch.Update(ctx, admin(), c.ID, ChannelInput{Name: "relay-1", Settings: emailSettings("x"), Enabled: true, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.ch.Get(ctx, admin(), c2.ID); v.IsDefault {
		t.Fatal("c2 still default")
	}
	// Unsetting the default removes the tenant grant.
	if _, err := f.ch.Update(ctx, admin(), c.ID, ChannelInput{Name: "relay-1", Settings: emailSettings("x"), Enabled: true, IsDefault: false}); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.az.Check(ctx, member(), authz.Channel, c.ID, authz.Use); d.Allowed {
		t.Fatal("tenant grant kept")
	}
	// Delete refused while templates reference it; then ok, grants dropped.
	tp := f.template(t, "welcome", c.ID)
	var iu *InUseError
	if err := f.ch.Delete(ctx, admin(), c.ID); !errors.As(err, &iu) || iu.Count != 1 {
		t.Fatalf("in use: %v", err)
	}
	if err := f.tp.Delete(ctx, admin(), tp.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.ch.Delete(ctx, admin(), c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ch.Get(ctx, admin(), c.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	for _, g := range f.ms.Grants {
		if g.ResourceID == c.ID {
			t.Fatal("grant left behind")
		}
	}
	if err := f.ch.Delete(ctx, member(), c2.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member delete: %v", err)
	}
	// Cross-tenant is not found.
	if _, err := f.ch.Get(ctx, authz.Subjects{TenantID: tB, UserID: uA, Roles: []string{"admin"}}, c2.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("cross tenant: %v", err)
	}
	f.aw.Flush()
	if n := len(f.ms.AuditEvents(tA, string(audit.ChannelCreated))); n != 2 {
		t.Fatalf("audit created %d", n)
	}
	// Store failures roll back.
	f.ms.FailOn("InsertChannel", errors.New("db"))
	if _, err := f.ch.Create(ctx, admin(), ChannelInput{Name: "x", Type: "email", Settings: emailSettings("x"), IsDefault: true}); err == nil {
		t.Fatal("db error swallowed")
	}
	f.ms.FailOn("InsertChannel", nil)
	f.ms.FailOn("UpdateChannel", errors.New("db"))
	if _, err := f.ch.Update(ctx, admin(), c2.ID, ChannelInput{Name: "z", Settings: emailSettings("x")}); err == nil {
		t.Fatal("update db error swallowed")
	}
	f.ms.FailOn("UpdateChannel", nil)
	f.ms.FailOn("ListChannels", errors.New("db"))
	if _, _, err := f.ch.List(ctx, admin(), "", "", 10); err == nil {
		t.Fatal("list db error swallowed")
	}
	if _, err := f.ch.Create(ctx, admin(), ChannelInput{Name: "y", Type: "email", Settings: emailSettings("x"), IsDefault: true}); err == nil {
		t.Fatal("moveDefault db error swallowed")
	}
	f.ms.FailOn("ListChannels", nil)
	f.ms.FailOn("DeleteChannel", store.ErrConflict)
	if err := f.ch.Delete(ctx, admin(), c2.ID); !errors.As(err, &iu) {
		t.Fatalf("delete conflict: %v", err)
	}
	f.ms.FailOn("DeleteChannel", errors.New("db"))
	if err := f.ch.Delete(ctx, admin(), c2.ID); err == nil {
		t.Fatal("delete db error swallowed")
	}
	f.ms.FailOn("DeleteChannel", nil)
	f.ms.FailOn("GetChannel", errors.New("db"))
	if _, _, _, err := f.ch.Resolve(ctx, tA, c2.ID); err == nil {
		t.Fatal("resolve db error swallowed")
	}
	f.ms.FailOn("GetChannel", nil)
	// A row sealed under another key cannot be opened.
	other, _ := sealed.NewEnvelope(bytes.Repeat([]byte{4}, 32))
	row = f.ms.Channels[c2.ID]
	row.SettingsSealed, _ = other.Seal([]byte(`{"host":"h"}`), sealed.AD(c2.ID))
	f.ms.Channels[c2.ID] = row
	if _, err := f.ch.Update(ctx, admin(), c2.ID, ChannelInput{Name: "z", Settings: emailSettings("x")}); err == nil {
		t.Fatal("tampered blob opened")
	}
	if _, _, _, err := f.ch.Resolve(ctx, tA, c2.ID); err == nil {
		t.Fatal("tampered blob resolved")
	}
	row.SettingsPublic = []byte("not json")
	f.ms.Channels[c2.ID] = row
	if v, err := f.ch.Get(ctx, admin(), c2.ID); err != nil || len(v.Settings) != 0 {
		t.Fatalf("bad public projection %v %v", v, err)
	}
	row.Type = "pigeon"
	row.SettingsSealed, _ = f.ch.env.Seal([]byte(`{"host":"h"}`), sealed.AD(c2.ID))
	f.ms.Channels[c2.ID] = row
	if _, _, _, err := f.ch.Resolve(ctx, tA, c2.ID); !errors.Is(err, channel.ErrType) {
		t.Fatalf("unknown type resolve: %v", err)
	}
}

func TestTemplatesLifecycle(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	c := f.channel(t, "relay", true)
	tp := f.template(t, "welcome", c.ID)
	if tp.ChannelName != "relay" || tp.ChannelType != "email" || !tp.Permissions.Use {
		t.Fatalf("view %+v", tp)
	}
	// Validation: syntax position, undeclared variable, bad name, too many, missing channel.
	var ve *ValidationError
	_, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "bad", ChannelID: c.ID, Subject: "s", Body: "line\n{{.X"})
	if !errors.As(err, &ve) || ve.Detail["where"] != "body" || !strings.HasPrefix(ve.Detail["position"].(string), "2") {
		t.Fatalf("syntax %v", err)
	}
	_, err = f.tp.Create(ctx, admin(), TemplateInput{Name: "bad", ChannelID: c.ID, Subject: "{{.Nope}}", Body: "b", Variables: []string{"Name"}})
	if !errors.As(err, &ve) || ve.Detail["variable"] != "Nope" {
		t.Fatalf("undeclared %v", err)
	}
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "bad", ChannelID: c.ID, Subject: "s", Body: "b", Variables: []string{"1x"}}); !errors.As(err, &ve) {
		t.Fatalf("bad variable name %v", err)
	}
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "bad", ChannelID: c.ID, Subject: "s", Body: "b", Variables: make([]string, 51)}); !errors.As(err, &ve) {
		t.Fatalf("too many variables %v", err)
	}
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "bad", Subject: "s", Body: "b"}); !errors.As(err, &ve) {
		t.Fatalf("missing channel %v", err)
	}
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "", ChannelID: c.ID, Subject: "s", Body: "b"}); !errors.As(err, &ve) {
		t.Fatalf("empty name %v", err)
	}
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "bad", ChannelID: store.NewID(), Subject: "s", Body: "b"}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("unknown channel %v", err)
	}
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "Welcome", ChannelID: c.ID, Subject: "s", Body: "b"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate %v", err)
	}
	// A member with tenant use on the channel (default) but no read on the template: refused.
	if _, err := f.tp.Get(ctx, member(), tp.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member get: %v", err)
	}
	// Default per channel moves.
	d1, _ := f.tp.Create(ctx, admin(), TemplateInput{Name: "d1", ChannelID: c.ID, Subject: "s", Body: "b", IsDefault: true})
	d2, _ := f.tp.Create(ctx, admin(), TemplateInput{Name: "d2", ChannelID: c.ID, Subject: "s", Body: "b", IsDefault: true})
	if v, _ := f.tp.Get(ctx, admin(), d1.ID); v.IsDefault || !d2.IsDefault {
		t.Fatal("default did not move")
	}
	// Listing with filters and paging.
	cid := c.ID
	list, next, err := f.tp.List(ctx, admin(), &cid, "", "", 2)
	if err != nil || len(list) != 2 || next == "" {
		t.Fatalf("page %v %q %v", list, next, err)
	}
	list, _, _ = f.tp.List(ctx, admin(), nil, "", next, 10)
	if len(list) != 1 {
		t.Fatalf("page 2 %v", list)
	}
	if list, _, _ := f.tp.List(ctx, admin(), nil, "wel", "", 10); len(list) != 1 || list[0].Name != "welcome" {
		t.Fatalf("search %v", list)
	}
	if list, _, _ := f.tp.List(ctx, member(), nil, "", "", 10); len(list) != 0 {
		t.Fatalf("member list %v", list)
	}
	// Update: rename, re-bind to a channel needing read, default move, refusals.
	c2 := f.channel(t, "relay2", false)
	up, err := f.tp.Update(ctx, admin(), tp.ID, TemplateInput{Name: "welcome2", ChannelID: c2.ID, Subject: "S {{.Name}}", Body: "B", Variables: []string{"Name"}, IsDefault: true})
	if err != nil || up.Name != "welcome2" || up.ChannelName != "relay2" || !up.IsDefault {
		t.Fatalf("update %+v %v", up, err)
	}
	if _, err := f.tp.Update(ctx, admin(), tp.ID, TemplateInput{Name: "welcome2", Subject: "s", Body: "b"}); !errors.As(err, &ve) {
		t.Fatalf("update without channel %v", err)
	}
	if _, err := f.tp.Update(ctx, admin(), tp.ID, TemplateInput{Name: "welcome2", ChannelID: store.NewID(), Subject: "s", Body: "b"}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("update unknown channel %v", err)
	}
	if _, err := f.tp.Update(ctx, admin(), tp.ID, TemplateInput{Name: "welcome2", ChannelID: c2.ID, Subject: "{{", Body: "b"}); !errors.As(err, &ve) {
		t.Fatalf("update syntax %v", err)
	}
	if _, err := f.tp.Update(ctx, member(), tp.ID, TemplateInput{Name: "x", ChannelID: c2.ID, Subject: "s", Body: "b"}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member update %v", err)
	}
	// Preview: saved template, draft, overrides, undeclared.
	s, b, err := f.tp.Preview(ctx, admin(), PreviewInput{TemplateID: tp.ID, Values: map[string]string{"Name": "Ann"}})
	if err != nil || s != "S Ann" || b != "B" {
		t.Fatalf("preview saved %q %q %v", s, b, err)
	}
	s, _, err = f.tp.Preview(ctx, admin(), PreviewInput{ChannelType: "email", Subject: "Hi {{.X}}", Body: "<i>{{.X}}</i>", Variables: []string{"X"}, Values: map[string]string{"X": "<b>"}})
	if err != nil || s != "Hi <b>" {
		t.Fatalf("preview draft %q %v", s, err)
	}
	if _, _, err := f.tp.Preview(ctx, admin(), PreviewInput{Subject: "{{.Y}}", Body: "b"}); !errors.As(err, &ve) {
		t.Fatalf("preview undeclared %v", err)
	}
	if _, _, err := f.tp.Preview(ctx, admin(), PreviewInput{Subject: "s", Body: "b", Variables: make([]string, 51)}); !errors.As(err, &ve) {
		t.Fatalf("preview too many %v", err)
	}
	if _, _, err := f.tp.Preview(ctx, member(), PreviewInput{TemplateID: tp.ID}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("preview forbidden %v", err)
	}
	if _, _, err := f.tp.Preview(ctx, admin(), PreviewInput{TemplateID: tp.ID, Subject: "{{.Missing}}", Body: "b"}); !errors.As(err, &ve) {
		t.Fatalf("preview override undeclared %v", err)
	}
	// Delete: forbidden for members, ok for the owner, grants dropped.
	if err := f.tp.Delete(ctx, member(), tp.ID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member delete %v", err)
	}
	if err := f.tp.Delete(ctx, admin(), tp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tp.Get(ctx, admin(), tp.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("deleted template readable")
	}
	// Store failures.
	f.ms.FailOn("InsertTemplate", errors.New("db"))
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "e", ChannelID: c.ID, Subject: "s", Body: "b", IsDefault: true}); err == nil {
		t.Fatal("insert db error swallowed")
	}
	f.ms.FailOn("InsertTemplate", nil)
	f.ms.FailOn("UpdateTemplate", errors.New("db"))
	if _, err := f.tp.Update(ctx, admin(), d1.ID, TemplateInput{Name: "d1", ChannelID: c.ID, Subject: "s", Body: "b", IsDefault: true}); err == nil {
		t.Fatal("update db error swallowed")
	}
	f.ms.FailOn("UpdateTemplate", nil)
	f.ms.FailOn("ListTemplates", errors.New("db"))
	if _, _, err := f.tp.List(ctx, admin(), nil, "", "", 10); err == nil {
		t.Fatal("list db error swallowed")
	}
	f.ms.FailOn("ListTemplates", nil)
	f.ms.FailOn("DeleteTemplate", errors.New("db"))
	if err := f.tp.Delete(ctx, admin(), d1.ID); err == nil {
		t.Fatal("delete db error swallowed")
	}
	f.ms.FailOn("DeleteTemplate", nil)
	f.ms.FailOn("GetTemplate", errors.New("db"))
	if _, _, err := f.tp.Preview(ctx, admin(), PreviewInput{TemplateID: d1.ID}); err == nil {
		t.Fatal("preview db error swallowed")
	}
	f.ms.FailOn("GetTemplate", nil)
	f.ms.FailOn("GetChannel", errors.New("db"))
	if _, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "e", ChannelID: c.ID, Subject: "s", Body: "b"}); err == nil {
		t.Fatal("create channel db error swallowed")
	}
	if _, err := f.tp.Update(ctx, admin(), d1.ID, TemplateInput{Name: "d1", ChannelID: c.ID, Subject: "s", Body: "b"}); err == nil {
		t.Fatal("update channel db error swallowed")
	}
	f.ms.FailOn("GetChannel", nil)
	if validationError(errors.New("other")) == nil {
		t.Fatal("validationError passthrough")
	}
	if (&ValidationError{Msg: "m"}).Error() == "" || (&InUseError{What: "x", Count: 1}).Error() == "" {
		t.Fatal("error strings")
	}
}

func TestSendPipeline(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	c := f.channel(t, "relay", true)
	tp := f.template(t, "welcome", c.ID)
	vars := map[string]string{"Name": "Ann", "Link": "https://x.y"}
	// Admin send: rendered, delivered, logged sent, audited.
	v, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "ann@example.org", Variables: vars, CorrelationID: "c1"})
	if err != nil || v.Status != "sent" || v.RenderedSubject != "Hello Ann" || !strings.Contains(v.RenderedBody, "https://x.y") || v.SentAt == nil || v.TemplateID == nil {
		t.Fatalf("send %+v %v", v, err)
	}
	if len(f.email.sent) != 1 || f.email.sent[0].To != "ann@example.org" || f.email.sent[0].HTMLBody == "" {
		t.Fatalf("delivered %+v", f.email.sent)
	}
	// Member: template use missing → forbidden; grant sharer on the template → allowed via tenant use on the channel.
	if _, err := f.snd.Send(ctx, member(), SendInput{TemplateID: tp.ID, Recipient: "ann@example.org", Variables: vars}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member without use: %v", err)
	}
	if _, err := f.az.Grant(ctx, admin(), authz.GrantInput{ResourceType: authz.Template, ResourceID: tp.ID, SubjectType: authz.SubjectUser, SubjectID: uB, Relation: authz.Sharer}); err != nil {
		t.Fatal(err)
	}
	if v, err := f.snd.Send(ctx, member(), SendInput{TemplateID: tp.ID, Recipient: "ann@example.org", Variables: vars}); err != nil || v.SenderID != uB {
		t.Fatalf("member send %+v %v", v, err)
	}
	// Refusals: missing variable, bad recipient, disabled channel, type mismatch, override without use, unknown template.
	var ve *ValidationError
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "ann@example.org", Variables: map[string]string{"Name": "x"}}); !errors.As(err, &ve) || ve.Detail["variable"] != "Link" {
		t.Fatalf("missing variable %v", err)
	}
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "not an address", Variables: vars}); !errors.As(err, &ve) {
		t.Fatalf("bad recipient %v", err)
	}
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: "", Recipient: "a@b.c"}); !errors.As(err, &ve) {
		t.Fatalf("no template %v", err)
	}
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: store.NewID(), Recipient: "a@b.c", Variables: vars}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("unknown template %v", err)
	}
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: map[string]string{"bad name": "x"}}); !errors.As(err, &ve) {
		t.Fatalf("bad variable name %v", err)
	}
	big := map[string]string{"Name": strings.Repeat("x", MaxVariablesBytes), "Link": "l"}
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: big}); !errors.As(err, &ve) {
		t.Fatalf("oversize variables %v", err)
	}
	many := map[string]string{}
	for i := 0; i < 51; i++ {
		many["V"+strings.Repeat("x", i)] = "1"
	}
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: many}); !errors.As(err, &ve) {
		t.Fatalf("too many variables %v", err)
	}
	sms, _ := f.ch.Create(ctx, admin(), ChannelInput{Name: "sms", Type: "sms", Settings: sealed.Settings{"api_key": "k"}, Enabled: true})
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, ChannelID: sms.ID, Recipient: "a@b.c", Variables: vars}); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("type mismatch %v", err)
	}
	disabled, _ := f.ch.Create(ctx, admin(), ChannelInput{Name: "off", Type: "email", Settings: emailSettings("x"), Enabled: false})
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, ChannelID: disabled.ID, Recipient: "a@b.c", Variables: vars}); !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("disabled %v", err)
	}
	if _, err := f.snd.Send(ctx, member(), SendInput{TemplateID: tp.ID, ChannelID: disabled.ID, Recipient: "a@b.c", Variables: vars}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("override without use %v", err)
	}
	// Provider failure: logged failed with a scrubbed reason; no password anywhere.
	f.email.fail = errors.New("535 auth failed for NOTIF-MARKER-PW-relay\nsecond line")
	v, err = f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars})
	if err != nil || v.Status != "failed" || strings.Contains(v.Error, "NOTIF-MARKER") || strings.Contains(v.Error, "second") {
		t.Fatalf("failed send %+v %v", v, err)
	}
	f.email.fail = nil
	// No provider type: failed entry with no_provider.
	nopTpl, err := f.tp.Create(ctx, admin(), TemplateInput{Name: "sms-tpl", ChannelID: sms.ID, Subject: "s", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: nopTpl.ID, Recipient: "+15551234567"}); err != nil || v.Status != "failed" || v.Error != "no_provider" {
		t.Fatalf("no provider %+v %v", v, err)
	}
	// Test send: write on the channel required; test flag; recipient checked.
	f.limiter.counts = nil
	tv, err := f.snd.SendTest(ctx, admin(), c.ID, "ops@example.org", "c2")
	if err != nil || !tv.Test || tv.Status != "sent" || tv.TemplateID != nil || tv.RenderedSubject != testSubject {
		t.Fatalf("test send %+v %v", tv, err)
	}
	if _, err := f.snd.SendTest(ctx, member(), c.ID, "ops@example.org", ""); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("member test send %v", err)
	}
	if _, err := f.snd.SendTest(ctx, admin(), c.ID, "nope", ""); !errors.As(err, &ve) {
		t.Fatalf("test bad recipient %v", err)
	}
	if _, err := f.snd.SendTest(ctx, admin(), store.NewID(), "a@b.c", ""); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("test unknown channel %v", err)
	}
	// Rate limits: the sender limit (3) trips before the tenant limit (5).
	f.limiter.counts = nil
	for i := 0; i < 3; i++ {
		if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("sender limit %v", err)
	}
	f.limiter.counts = map[string]int{"tenant:" + tA: 5}
	if _, err := f.snd.SendTest(ctx, admin(), c.ID, "a@b.c", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("tenant limit %v", err)
	}
	f.limiter.counts = nil
	f.limiter.err = errors.New("valkey down")
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("limiter down must limit: %v", err)
	}
	f.limiter.err = nil
	// Log listing: admins see all, members their own; filters; paging; single read with body.
	all, _, err := f.snd.ListLog(ctx, admin(), store.LogFilter{}, true)
	if err != nil || len(all) < 6 || all[0].RenderedBody != "" {
		t.Fatalf("list all %d %v", len(all), err)
	}
	mine, _, _ := f.snd.ListLog(ctx, member(), store.LogFilter{}, false)
	if len(mine) != 1 || mine[0].SenderID != uB {
		t.Fatalf("member list %v", mine)
	}
	failed, _, _ := f.snd.ListLog(ctx, admin(), store.LogFilter{Status: "failed"}, true)
	if len(failed) != 2 {
		t.Fatalf("failed filter %d", len(failed))
	}
	if _, _, err := f.snd.ListLog(ctx, admin(), store.LogFilter{Status: "lost"}, true); !errors.As(err, &ve) {
		t.Fatal("bad status accepted")
	}
	page1, next, _ := f.snd.ListLog(ctx, admin(), store.LogFilter{Limit: 2}, true)
	if len(page1) != 2 || next == "" {
		t.Fatalf("page %v %q", page1, next)
	}
	ts, id, err := DecodeCursor(next)
	if err != nil || id == "" || ts.IsZero() {
		t.Fatalf("cursor %v", err)
	}
	page2, _, _ := f.snd.ListLog(ctx, admin(), store.LogFilter{Limit: 2, CursorTS: ts, CursorID: id}, true)
	if len(page2) != 2 || page2[0].ID == page1[0].ID {
		t.Fatalf("page 2 %v", page2)
	}
	if _, _, err := DecodeCursor("garbage"); err == nil {
		t.Fatal("bad cursor accepted")
	}
	if _, _, err := DecodeCursor("notatime|x"); err == nil {
		t.Fatal("bad cursor time accepted")
	}
	one, err := f.snd.GetLog(ctx, admin(), v.ID, true)
	if err != nil || one.RenderedBody == "" {
		t.Fatalf("get %+v %v", one, err)
	}
	if _, err := f.snd.GetLog(ctx, member(), v.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("member reads another sender's entry: %v", err)
	}
	// Expiry of stuck pending rows.
	f.ms.Logs["stuck"] = store.LogRow{ID: "stuck", TenantID: tA, CreatedAt: f.now.Add(-time.Hour), Status: "pending", ChannelID: c.ID, ChannelType: "email", SenderKind: "user", SenderID: uA}
	if n, err := f.snd.ExpirePending(ctx, 10*time.Minute); err != nil || n != 1 || f.ms.Logs["stuck"].Error != "interrupted" {
		t.Fatalf("expire %d %v", n, err)
	}
	rctx, cancel := context.WithCancel(ctx)
	cancel()
	f.snd.RunExpiry(rctx, time.Millisecond)
	// Audit: sends, failures and refusals present; no credential.
	f.aw.Flush()
	if len(f.ms.AuditEvents(tA, string(audit.NotificationSent))) < 4 || len(f.ms.AuditEvents(tA, string(audit.NotificationFailed))) != 2 {
		t.Fatal("audit counts")
	}
	for _, e := range f.ms.Audit {
		if strings.Contains(string(e.Details), "NOTIF-MARKER") {
			t.Fatal("credential in audit")
		}
	}
	// Store failures around the log.
	f.ms.FailOn("InsertLog", errors.New("db"))
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); err == nil {
		t.Fatal("insert log db error swallowed")
	}
	f.ms.FailOn("InsertLog", nil)
	f.ms.FailOn("SetLogOutcome", errors.New("db"))
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); err == nil {
		t.Fatal("outcome db error swallowed")
	}
	f.email.fail = errors.New("x")
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); err == nil {
		t.Fatal("failed outcome db error swallowed")
	}
	f.email.fail = nil
	f.ms.FailOn("SetLogOutcome", nil)
	f.ms.FailOn("LogPage", errors.New("db"))
	if _, _, err := f.snd.ListLog(ctx, admin(), store.LogFilter{}, true); err == nil {
		t.Fatal("list db error swallowed")
	}
	f.ms.FailOn("LogPage", nil)
	f.ms.FailOn("GetTemplate", errors.New("db"))
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); err == nil {
		t.Fatal("get template db error swallowed")
	}
	f.ms.FailOn("GetTemplate", nil)
	f.ms.FailOn("GetChannel", errors.New("db"))
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); err == nil {
		t.Fatal("get channel db error swallowed")
	}
	if _, err := f.snd.SendTest(ctx, admin(), c.ID, "a@b.c", ""); err == nil {
		t.Fatal("test get channel db error swallowed")
	}
	f.ms.FailOn("GetChannel", nil)
	// A template with a broken body stored (bypassing validation) fails at send with a validation error.
	row := f.ms.Templates[tp.ID]
	row.Body = "{{"
	f.ms.Templates[tp.ID] = row
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); !errors.As(err, &ve) {
		t.Fatalf("broken stored template %v", err)
	}
	row.Body, row.ChannelID = "b", nil
	f.ms.Templates[tp.ID] = row
	if _, err := f.snd.Send(ctx, admin(), SendInput{TemplateID: tp.ID, Recipient: "a@b.c", Variables: vars}); !errors.As(err, &ve) {
		t.Fatalf("template without channel %v", err)
	}
	// A sender without a limiter never limits.
	nl := NewSender(f.ms, f.ch, f.tp, f.az, f.aw, nil, Limits{})
	if err := nl.limit(ctx, admin()); err != nil {
		t.Fatal(err)
	}
	if shortReason(strings.Repeat("x", 200)) != strings.Repeat("x", 120) {
		t.Fatal("short reason")
	}
}
