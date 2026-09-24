package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/memstore"
	"github.com/go-tangra/go-tangra-notification/v4/internal/messages"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
)

const (
	tA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	tB = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c66"
	uA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c77"
)

type fakeProvider struct{}

func (fakeProvider) Type() string { return "email" }
func (fakeProvider) Validate(s sealed.Settings) error {
	if _, ok := s["host"]; !ok {
		return errors.New("host is required")
	}
	return nil
}
func (fakeProvider) Secret() []string                                             { return []string{"password"} }
func (fakeProvider) Send(context.Context, sealed.Settings, channel.Message) error { return nil }

type fx struct {
	ms  *memstore.Store
	aw  *audit.Writer
	ch  *notify.Channels
	tp  *notify.Templates
	svc *Service
}

func newFx(t *testing.T) *fx {
	t.Helper()
	ms := memstore.New()
	aw := audit.NewWriter(ms, nil)
	t.Cleanup(aw.Close)
	az := authz.New(ms, aw)
	env, _ := sealed.NewEnvelope(bytes.Repeat([]byte{3}, 32))
	reg := channel.NewRegistry(fakeProvider{}, channel.Nop{Kind: "sms"})
	ch := notify.NewChannels(ms, env, reg, az, aw)
	tp := notify.NewTemplates(ms, az, aw)
	msgs := messages.New(ms, aw, messages.StaticDirectory{}, nil)
	svc := New(ms, ch, tp, msgs, aw)
	svc.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return &fx{ms: ms, aw: aw, ch: ch, tp: tp, svc: svc}
}

func subj(tenant string) authz.Subjects {
	return authz.Subjects{TenantID: tenant, UserID: uA, Roles: []string{"admin"}}
}

func (f *fx) seed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	c, err := f.ch.Create(ctx, subj(tA), notify.ChannelInput{Name: "Relay", Type: "email", Settings: sealed.Settings{"host": "relay", "port": float64(587), "from": "a@b.c", "password": "NOTIF-MARKER-PW-1"}, Enabled: true, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ch.Create(ctx, subj(tA), notify.ChannelInput{Name: "SMS", Type: "sms", Settings: sealed.Settings{"api_key": "NOTIF-MARKER-PW-2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tp.Create(ctx, subj(tA), notify.TemplateInput{Name: "Welcome", ChannelID: c.ID, Subject: "Hi {{.Name}}", Body: "<p>{{.Name}}</p>", Variables: []string{"Name"}, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.msgs.CreateCategory(ctx, subj(tA), messages.CategoryInput{Name: "Ops", Description: "d", Sort: 3}); err != nil {
		t.Fatal(err)
	}
}

func TestExportRedactsUnlessAsked(t *testing.T) {
	f := newFx(t)
	f.seed(t)
	ctx := context.Background()
	doc, err := f.svc.Export(ctx, subj(tA), false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	if strings.Contains(string(raw), "NOTIF-MARKER") || doc.IncludesCredentials || len(doc.Channels) != 2 || len(doc.Templates) != 1 || len(doc.Categories) != 1 || doc.Templates[0].Channel != "Relay" || doc.Version != 1 {
		t.Fatalf("export: %s", raw)
	}
	if _, ok := doc.Channels[0].Settings["password"]; ok {
		t.Fatal("password key present")
	}
	doc, err = f.svc.Export(ctx, subj(tA), true)
	if err != nil || !doc.IncludesCredentials || doc.Channels[0].Settings["password"] != "NOTIF-MARKER-PW-1" {
		t.Fatalf("with credentials: %v %+v", err, doc.Channels)
	}
	// Re-encoding validates against the schema.
	raw, _ = json.Marshal(doc)
	if _, err := DecodeBounded(raw); err != nil {
		t.Fatalf("schema: %v", err)
	}
	f.aw.Flush()
	if len(f.ms.AuditEvents(tA, "backup_exported")) != 1 || len(f.ms.AuditEvents(tA, "backup_exported_with_credentials")) != 1 {
		t.Fatal("audit")
	}
	for _, op := range []string{"ListChannels", "GetChannel", "AllTemplates", "ListCategories"} {
		f.ms.FailOn(op, errors.New("down"))
		if _, err := f.svc.Export(ctx, subj(tA), false); err == nil {
			t.Errorf("%s outage", op)
		}
		f.ms.FailOn(op, nil)
	}
	f.ms.FailOn("GetChannel", errors.New("down"))
	if _, err := f.svc.Export(ctx, subj(tA), true); err == nil {
		t.Error("resolve outage")
	}
	f.ms.FailOn("GetChannel", nil)
}

func TestDecodeBounded(t *testing.T) {
	if _, err := DecodeBounded(bytes.Repeat([]byte("a"), MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	deep := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	if _, err := DecodeBounded([]byte(deep)); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "deep") {
		t.Fatal(err)
	}
	if _, err := DecodeBounded([]byte(`{"version":`)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := DecodeBounded([]byte(`{"version":2,"exported_at":"2024-01-01T00:00:00Z","tenant":"t","channels":[],"templates":[],"categories":[]}`)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := DecodeBounded([]byte(`{"version":1,"exported_at":"2024-01-01T00:00:00Z","tenant":"t","channels":[],"templates":[],"categories":[],"extra":"\"{[\\\"}"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	// Item bound: the schema caps arrays at 1000 each; the combined bound is checked after decoding.
	var cats []string
	for i := 0; i <= MaxItems/3; i++ {
		cats = append(cats, `{"name":"c`+string(rune('a'+i%26))+`"}`)
	}
	big := `{"version":1,"exported_at":"2024-01-01T00:00:00Z","tenant":"t","channels":[],"templates":[],"categories":[` + strings.Join(cats[:1000], ",") + `]}`
	if _, err := DecodeBounded([]byte(big)); err != nil {
		t.Fatalf("1000 categories must decode: %v", err)
	}
	doc, err := DecodeBounded([]byte(`{"version":1,"exported_at":"2024-01-01T00:00:00Z","tenant":"t","channels":[{"name":"a","type":"sms","settings":{},"enabled":true}],"templates":[],"categories":[]}`))
	if err != nil || len(doc.Channels) != 1 || doc.Channels[0].Type != "sms" {
		t.Fatal(err)
	}
	if d := depth([]byte(`{"a":"}{[","b":[[{}]]}`)); d != 4 {
		t.Fatalf("depth %d", d)
	}
	if firstLine("a\nb") != "a" || firstLine("a") != "a" {
		t.Fatal("firstLine")
	}
}

func TestImportModes(t *testing.T) {
	f := newFx(t)
	f.seed(t)
	ctx := context.Background()
	doc, _ := f.svc.Export(ctx, subj(tA), true)
	if _, err := f.svc.Import(ctx, subj(tB), doc, "merge"); !errors.Is(err, ErrMode) {
		t.Fatal(err)
	}
	rep, err := f.svc.Import(ctx, subj(tB), doc, "skip")
	if err != nil || rep.Channels.Created != 2 || rep.Templates.Created != 1 || rep.Categories.Created != 1 || len(rep.Warnings) != 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	// Credentials travelled: the imported channel resolves the password.
	chans, _ := f.ms.ListChannels(ctx, tB, "", "", 10)
	_, settings, _, err := f.ch.Resolve(ctx, tB, chans[0].ID)
	if err != nil || settings["password"] != "NOTIF-MARKER-PW-1" {
		t.Fatalf("credentials %v %v", err, settings)
	}
	rep, _ = f.svc.Import(ctx, subj(tB), doc, "skip")
	if rep.Channels.Skipped != 2 || rep.Templates.Skipped != 1 || rep.Categories.Skipped != 1 || rep.Channels.Created != 0 {
		t.Fatalf("skip %+v", rep)
	}
	// Overwrite without credentials keeps the stored password.
	pub, _ := f.svc.Export(ctx, subj(tA), false)
	pub.Channels[0].Enabled = false
	pub.Templates[0].Subject = "Changed {{.Name}}"
	pub.Categories[0].Sort = 7
	rep, err = f.svc.Import(ctx, subj(tB), pub, "overwrite")
	if err != nil || rep.Channels.Overwritten != 2 || rep.Templates.Overwritten != 1 || rep.Categories.Overwritten != 1 {
		t.Fatalf("overwrite %v %+v", err, rep)
	}
	_, settings, _, _ = f.ch.Resolve(ctx, tB, chans[0].ID)
	if settings["password"] != "NOTIF-MARKER-PW-1" {
		t.Fatalf("password lost: %v", settings)
	}
	tpls, _ := f.ms.AllTemplates(ctx, tB, 10)
	cats, _ := f.ms.ListCategories(ctx, tB)
	if tpls[0].Subject != "Changed {{.Name}}" || cats[0].Sort != 7 {
		t.Fatal("overwrite did not apply")
	}
	// Warnings: type clash, unknown template channel, invalid entities.
	bad := Document{Version: 1, Channels: []Channel{{Name: "Relay", Type: "sms", Settings: sealed.Settings{}}, {Name: "Broken", Type: "email", Settings: sealed.Settings{}}},
		Templates:  []Template{{Name: "Orphan", Channel: "Nope", Subject: "s", Body: "b"}, {Name: "Bad", Channel: "Relay", Subject: "{{.X", Body: "b"}, {Name: "Welcome", Channel: "Relay", Subject: "{{.Y}}", Body: "b"}},
		Categories: []Category{{Name: ""}, {Name: "Ops", Description: strings.Repeat("d", 501)}}}
	rep, err = f.svc.Import(ctx, subj(tB), bad, "overwrite")
	if err != nil || rep.Channels.Skipped != 1 || rep.Channels.Failed != 1 || rep.Templates.Failed != 3 || rep.Categories.Failed != 2 || len(rep.Warnings) != 7 {
		t.Fatalf("warnings %v %+v", err, rep)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "NOTIF-MARKER") {
			t.Fatalf("warning leaks: %s", w)
		}
	}
	// A template update failure and a channel update failure count as failed.
	f.ms.FailOn("UpdateTemplate", errors.New("down"))
	f.ms.FailOn("UpdateChannel", errors.New("down"))
	rep, _ = f.svc.Import(ctx, subj(tB), pub, "overwrite")
	if rep.Templates.Failed != 1 || rep.Channels.Failed != 2 {
		t.Fatalf("update failures %+v", rep)
	}
	f.ms.FailOn("UpdateTemplate", nil)
	f.ms.FailOn("UpdateChannel", nil)
	f.ms.FailOn("UpdateCategory", errors.New("down"))
	rep, _ = f.svc.Import(ctx, subj(tB), pub, "overwrite")
	if rep.Categories.Failed != 1 {
		t.Fatalf("category failure %+v", rep)
	}
	f.ms.FailOn("UpdateCategory", nil)
	for _, op := range []string{"ListCategories", "ListChannels", "AllTemplates"} {
		f.ms.FailOn(op, errors.New("down"))
		if _, err := f.svc.Import(ctx, subj(tB), pub, "skip"); err == nil {
			t.Errorf("%s outage", op)
		}
		f.ms.FailOn(op, nil)
	}
	f.aw.Flush()
	if n := len(f.ms.AuditEvents(tB, "backup_imported")); n < 3 {
		t.Fatalf("audit %d", n)
	}
}
