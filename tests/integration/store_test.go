//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/go-freya/freya/services/notification/internal/store"
)

// TestMigrationsAndRLS proves the schema from data-model.md is in place
// under row-level security: the application role sees only the tenant of
// its transaction, the audit hypertable is append-only, and the migration
// is idempotent (T009).
func TestMigrationsAndRLS(t *testing.T) {
	e := StartPlatform(t)
	ctx := context.Background()
	st := e.Notif.Store
	for _, table := range []string{"channels", "templates", "grants", "message_categories", "messages", "inbox", "notification_log", "notification_audit_events"} {
		var rls bool
		err := st.Tx(ctx, store.Scope{System: true}, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT relrowsecurity FROM pg_class WHERE relname = $1", table).Scan(&rls)
		})
		if err != nil || !rls {
			t.Errorf("%s: rls=%v err=%v", table, rls, err)
		}
	}
	var hyper int
	_ = st.Tx(ctx, store.Scope{System: true}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM timescaledb_information.hypertables WHERE hypertable_name IN ('notification_log','notification_audit_events')").Scan(&hyper)
	})
	if hyper != 2 {
		t.Fatalf("hypertables %d", hyper)
	}
	// Two tenants: rows of one are invisible in the other's transaction, and
	// an insert for a foreign tenant is refused by the policy.
	tA, tB := store.NewID(), store.NewID()
	for _, tid := range []string{tA, tB} {
		if err := st.Tx(ctx, store.Scope{TenantID: tid}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO message_categories (id, tenant_id, name, description, sort) VALUES ($1, $2, 'c', '', 0)", store.NewID(), tid)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = st.Tx(ctx, store.Scope{TenantID: tA}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM message_categories").Scan(&n)
	})
	if n != 1 {
		t.Fatalf("tenant A sees %d categories", n)
	}
	err := st.Tx(ctx, store.Scope{TenantID: tA}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO message_categories (id, tenant_id, name, description, sort) VALUES ($1, $2, 'x', '', 0)", store.NewID(), tB)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("cross-tenant insert: %v", err)
	}
	// Audit rows are append-only for the application role.
	err = st.Tx(ctx, store.Scope{TenantID: tA}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM notification_audit_events")
		return err
	})
	if err == nil {
		t.Fatal("audit rows deletable")
	}
	// Uniqueness per tenant: the same name in another tenant is fine, in the same tenant a conflict.
	err = st.Tx(ctx, store.Scope{TenantID: tA}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO message_categories (id, tenant_id, name, description, sort) VALUES ($1, $2, 'C', '', 0)", store.NewID(), tA)
		return err
	})
	if err == nil {
		t.Fatal("case-insensitive uniqueness not enforced")
	}
	// Migrations are idempotent.
	if err := store.Migrate(ctx, e.Notif.Cfg.DB.MigrateDSN); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	// The repository binding round-trips a channel with sealed settings and expires stale pending log rows.
	repo := e.Notif.Repo
	ch := store.Channel{ID: store.NewID(), TenantID: tA, Name: "relay", Type: "email", SettingsSealed: []byte("sealed"), SettingsPublic: []byte(`{"host":"h"}`), Enabled: true}
	if err := repo.InsertChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetChannel(ctx, tA, ch.ID)
	if err != nil || string(got.SettingsSealed) != "sealed" || got.Name != "relay" {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := repo.GetChannel(ctx, tB, ch.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	if err := repo.InsertLog(ctx, store.LogRow{ID: store.NewID(), TenantID: tA, ChannelID: ch.ID, ChannelType: "email", Recipient: "a@b.c", Status: "pending", SenderKind: "user", SenderID: "u"}); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ExpirePendingLogs(ctx, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("expire %d %v", n, err)
	}
}
