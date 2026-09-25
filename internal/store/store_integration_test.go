//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/go-tangra/go-tangra-notification/v4/internal/repo/repodb"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// openStore starts TimescaleDB, applies the migrations as the owner and
// opens the application role (no BYPASSRLS), as in production.
func openStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image: "timescale/timescaledb:latest-pg16", ExposedPorts: []string{"5432/tcp"}, Env: map[string]string{"POSTGRES_PASSWORD": "test", "POSTGRES_DB": "notification"},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(2 * time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432/tcp")
	owner := "postgres://postgres:test@" + host + ":" + port.Port() + "/notification?sslmode=disable"
	for i := 0; ; i++ {
		conn, err := pgx.Connect(ctx, owner)
		if err == nil {
			_, err = conn.Exec(ctx, "CREATE ROLE notification_app LOGIN PASSWORD 'app' NOBYPASSRLS")
			_ = conn.Close(ctx)
			if err == nil {
				break
			}
		}
		if i == 30 {
			t.Fatalf("database: %v", err)
		}
		time.Sleep(time.Second)
	}
	if err := store.Migrate(ctx, owner); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, "postgres://notification_app:app@"+host+":"+port.Port()+"/notification?sslmode=disable", 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

func ptr(s string) *string { return &s }

// TestSystemEmailSchema covers migration 0005 (T007): the managed channel
// (at most one per tenant), the system template columns and their key
// uniqueness and format, and template_key on log entries.
func TestSystemEmailSchema(t *testing.T) {
	st := openStore(t)
	db := repodb.New(st)
	ctx := context.Background()
	tA, tB := store.NewID(), store.NewID()

	// Managed channel: found by tenant; a second one in the same tenant conflicts; another tenant is independent.
	managed := store.Channel{ID: store.NewID(), TenantID: tA, Name: "Platform email", Type: "email", SettingsSealed: []byte("s"), Enabled: true, IsDefault: true, Managed: true}
	if err := db.InsertChannel(ctx, managed); err != nil {
		t.Fatal(err)
	}
	got, err := db.ManagedChannel(ctx, tA)
	if err != nil || got.ID != managed.ID || !got.Managed {
		t.Fatalf("managed %v %+v", err, got)
	}
	if err := db.InsertChannel(ctx, store.Channel{ID: store.NewID(), TenantID: tA, Name: "second", Type: "email", SettingsSealed: []byte("s"), Managed: true}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second managed: %v", err)
	}
	if _, err := db.ManagedChannel(ctx, tB); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tenant B managed: %v", err)
	}
	if err := db.InsertChannel(ctx, store.Channel{ID: store.NewID(), TenantID: tB, Name: "Platform email", Type: "email", SettingsSealed: []byte("s"), Managed: true}); err != nil {
		t.Fatalf("tenant B: %v", err)
	}
	// UpdateChannel never clears the managed flag.
	managed.Name = "Platform email"
	managed.Enabled = false
	if err := db.UpdateChannel(ctx, managed); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetChannel(ctx, tA, managed.ID); !got.Managed || got.Enabled {
		t.Fatalf("after update %+v", got)
	}
	// Default email channel of a tenant (enabled or not: the caller decides).
	if got, err := db.DefaultEmailChannel(ctx, tA); err != nil || got.ID != managed.ID {
		t.Fatalf("default %v %+v", err, got)
	}
	if _, err := db.DefaultEmailChannel(ctx, tB); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tenant B default: %v", err)
	}

	// System template columns round-trip; the key is unique per tenant and well formed.
	sys := store.Template{ID: store.NewID(), TenantID: tA, Name: "auth.invite", ChannelType: "email", Subject: "S {{.link}}", Body: "B {{.link}}", Variables: []string{"link", "valid_for"},
		SystemKey: ptr("auth.invite"), BuiltinSubject: "S {{.link}}", BuiltinBody: "B {{.link}}", RequiredVariables: []string{"link"}, SecretVariables: []string{"link"}}
	if err := db.InsertTemplate(ctx, sys); err != nil {
		t.Fatal(err)
	}
	tpl, err := db.TemplateByKey(ctx, tA, "auth.invite")
	if err != nil || tpl.ID != sys.ID || tpl.SystemKey == nil || *tpl.SystemKey != "auth.invite" || tpl.BuiltinBody != "B {{.link}}" || len(tpl.RequiredVariables) != 1 || tpl.SecretVariables[0] != "link" || tpl.ChannelID != nil {
		t.Fatalf("by key %v %+v", err, tpl)
	}
	if _, err := db.TemplateByKey(ctx, tB, "auth.invite"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tenant B key: %v", err)
	}
	dup := sys
	dup.ID, dup.Name = store.NewID(), "other name"
	if err := db.InsertTemplate(ctx, dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate key: %v", err)
	}
	bad := sys
	bad.ID, bad.Name, bad.SystemKey = store.NewID(), "bad", ptr("Auth.Invite")
	if err := db.InsertTemplate(ctx, bad); err == nil {
		t.Fatal("malformed key accepted")
	}
	// Ordinary templates carry no system columns.
	plain := store.Template{ID: store.NewID(), TenantID: tA, Name: "welcome", ChannelType: "email", Subject: "s", Body: "b"}
	if err := db.InsertTemplate(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetTemplate(ctx, tA, plain.ID); got.SystemKey != nil || got.RequiredVariables == nil || len(got.SecretVariables) != 0 || got.BuiltinSubject != "" {
		t.Fatalf("plain %+v", got)
	}
	// Refreshing the built-in wording never touches subject/body; updating subject/body keeps the system columns.
	sys.Subject, sys.Body = "edited", "edited {{.link}}"
	if err := db.UpdateTemplate(ctx, sys); err != nil {
		t.Fatal(err)
	}
	sys.BuiltinSubject, sys.BuiltinBody, sys.Variables, sys.RequiredVariables, sys.SecretVariables = "S2", "B2 {{.link}}", []string{"link", "valid_for", "x"}, []string{"link", "valid_for"}, []string{"link"}
	if err := db.SetTemplateBuiltin(ctx, sys); err != nil {
		t.Fatal(err)
	}
	tpl, _ = db.GetTemplate(ctx, tA, sys.ID)
	if tpl.Subject != "edited" || tpl.BuiltinSubject != "S2" || tpl.BuiltinBody != "B2 {{.link}}" || len(tpl.Variables) != 3 || len(tpl.RequiredVariables) != 2 || tpl.SystemKey == nil {
		t.Fatalf("builtin refresh %+v", tpl)
	}
	if err := db.SetTemplateBuiltin(ctx, store.Template{ID: store.NewID(), TenantID: tA}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refresh unknown: %v", err)
	}

	// Log entries carry the template key.
	row := store.LogRow{ID: store.NewID(), TenantID: tB, CreatedAt: time.Now(), ChannelID: managed.ID, ChannelType: "email", TemplateID: &sys.ID, TemplateKey: ptr("auth.invite"),
		Recipient: "a@b.c", Status: "pending", SenderKind: "service", SenderID: "spiffe://example.org/svc/auth"}
	if err := db.InsertLog(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := db.SetLogOutcome(ctx, tB, row.ID, "sent", "", "s", "b [redacted]", ptr2(time.Now())); err != nil {
		t.Fatal(err)
	}
	l, err := db.GetLog(ctx, tB, row.ID)
	if err != nil || l.TemplateKey == nil || *l.TemplateKey != "auth.invite" || l.RenderedBody != "b [redacted]" {
		t.Fatalf("log %v %+v", err, l)
	}
	page, err := db.LogPage(ctx, tB, store.LogFilter{Limit: 10})
	if err != nil || len(page) != 1 || page[0].TemplateKey == nil {
		t.Fatalf("page %v %+v", err, page)
	}
}

func ptr2(t time.Time) *time.Time { return &t }
