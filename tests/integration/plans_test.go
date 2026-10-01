//go:build integration

package integration

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra/v4/listquery"
	"github.com/jackc/pgx/v5"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// fullSort matches a plain Sort node (not an Incremental Sort, which only
// orders the tie-breaker within equal leading keys the index already sorted).
var fullSort = regexp.MustCompile(`(?m)^\s*(->\s+)?Sort\s+\(`)

// TestListPlans proves the default list orders are index-backed (032
// perf.md): with NotNull sort fields the ORDER BY carries no NULLS LAST, so
// the (tenant_id, col DESC) indexes serve the descending defaults. Sorting
// is disabled for the planner, so a plan still holding a full Sort node
// means no index can deliver the order.
func TestListPlans(t *testing.T) {
	e := StartPlatform(t)
	ctx := context.Background()
	tid := store.NewID()
	err := e.Notif.Store.Tx(ctx, store.Scope{TenantID: tid}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO notification_log (id, tenant_id, created_at, channel_id, channel_type, recipient, status, sender_kind, sender_id)
			SELECT gen_random_uuid(), $1, now() - (g || ' minutes')::interval, gen_random_uuid(), 'email', 'r' || g || '@x.test',
				(ARRAY['pending','sent','failed'])[1 + g % 3], 'user', 'u' || (g % 7)
			FROM generate_series(1, 3000) g`, tid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO messages (id, tenant_id, title, type, status, created_at)
			SELECT gen_random_uuid(), $1, 'm' || g, 'notification', 'draft', now() - (g || ' minutes')::interval
			FROM generate_series(1, 3000) g`, tid); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO notification_audit_events (ts, tenant_id, event_type, actor_kind, outcome)
			SELECT now() - (g || ' minutes')::interval, $1, 'notification.sent', 'system', 'ok'
			FROM generate_series(1, 3000) g`, tid)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, sql, index string
	}{
		{"log default", "SELECT id FROM notification_log WHERE tenant_id = $1 AND created_at >= now() - interval '7 days' AND created_at <= now() ORDER BY " +
			store.ListRequest(listquery.Request{}, store.LogList).OrderBy(store.LogList) + " LIMIT 25", "notification_log_tenant"},
		{"log status filter", "SELECT id FROM notification_log WHERE tenant_id = $1 AND created_at >= now() - interval '7 days' AND created_at <= now() AND status = 'failed' ORDER BY " +
			store.ListRequest(listquery.Request{}, store.LogList).OrderBy(store.LogList) + " LIMIT 25", "notification_log_"},
		{"log own sends", "SELECT id FROM notification_log WHERE tenant_id = $1 AND created_at >= now() - interval '7 days' AND created_at <= now() AND sender_id = 'u3' ORDER BY " +
			store.ListRequest(listquery.Request{}, store.LogList).OrderBy(store.LogList) + " LIMIT 25", "notification_log_sender"},
		{"audit default", "SELECT a.ts FROM notification_audit_events a WHERE a.tenant_id = $1 AND a.ts >= now() - interval '7 days' AND a.ts <= now() ORDER BY " +
			store.ListRequest(listquery.Request{}, store.AuditList).OrderBy(store.AuditList) + " LIMIT 50", "notification_audit_tenant_ts"},
		{"messages default", "SELECT m.id FROM messages m WHERE m.tenant_id = $1 ORDER BY " +
			store.ListRequest(listquery.Request{}, store.MessageList).OrderBy(store.MessageList) + " LIMIT 25", "messages_tenant_created"},
		{"messages created asc", "SELECT m.id FROM messages m WHERE m.tenant_id = $1 ORDER BY " +
			store.ListRequest(listquery.Request{Sort: "created_at", Order: listquery.Asc}, store.MessageList).OrderBy(store.MessageList) + " LIMIT 25", "messages_tenant_created"},
		{"messages subject desc", "SELECT m.id FROM messages m WHERE m.tenant_id = $1 ORDER BY " +
			store.ListRequest(listquery.Request{Sort: "subject", Order: listquery.Desc}, store.MessageList).OrderBy(store.MessageList) + " LIMIT 25", "messages_tenant_lower_title"},
	}
	for _, c := range cases {
		var plan []string
		err := e.Notif.Store.Tx(ctx, store.Scope{TenantID: tid}, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SET LOCAL enable_sort = off"); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, "EXPLAIN "+c.sql, tid)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					return err
				}
				plan = append(plan, line)
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		text := strings.Join(plan, "\n")
		if fullSort.MatchString(text) || !strings.Contains(text, c.index) {
			t.Errorf("%s: not served by %s:\n%s\n%s", c.name, c.index, c.sql, text)
		} else {
			t.Logf("%s:\n%s", c.name, text)
		}
	}
}
