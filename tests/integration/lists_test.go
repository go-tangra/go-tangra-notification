//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Feature 032: every notification table on the list contract, end to end
// through the gateway against TimescaleDB (go-tangra
// specs/032-server-side-tables, T114).

func (s *Session) page(t *testing.T, path string) (total, page int, items []map[string]any, body map[string]any) {
	t.Helper()
	code, body := s.JSON(http.MethodGet, path, nil)
	if code != 200 {
		t.Fatalf("GET %s → %d %v", path, code, body)
	}
	raw, _ := body["items"].([]any)
	for _, r := range raw {
		items = append(items, r.(map[string]any))
	}
	tot, _ := body["total"].(float64)
	pg, _ := body["page"].(float64)
	return int(tot), int(pg), items, body
}

func field(items []map[string]any, key string) string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprint(it[key]))
	}
	return strings.Join(out, ",")
}

// TestLists covers channels and templates with partial grants (readable ids
// in SQL: exact totals, no hidden record counted or returned), messages
// under the sender scope, the log within its default window, categories,
// audit paging exactly once with equal timestamps, legacy cursor paging and
// 422 negatives.
func TestLists(t *testing.T) {
	e := StartPlatform(t)
	ctx := context.Background()
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	alice := e.Invite(owner, "alice@acme.test", "member")
	bob := e.Invite(owner, "bob@acme.test", "member")

	// ---- channels and templates: alice reads 3 of 6 channels and 2 of 6 templates.
	var chans, tpls []string
	for _, n := range []string{"foxtrot", "alpha", "echo", "bravo", "delta", "charlie"} {
		id := e.EmailChannel(owner, n, false)
		chans = append(chans, id)
		tpls = append(tpls, e.Template(owner, "t-"+n, id))
	}
	for _, i := range []int{0, 2, 4} { // foxtrot, echo, delta
		if code, out := e.Grant(owner, "channel", chans[i], "user", alice.UserID, "viewer", nil); code != 201 {
			t.Fatalf("grant → %d %v", code, out)
		}
	}
	for _, i := range []int{1, 3} { // t-alpha, t-bravo
		if code, out := e.Grant(owner, "template", tpls[i], "user", alice.UserID, "viewer", nil); code != 201 {
			t.Fatalf("grant → %d %v", code, out)
		}
	}
	total, _, items, _ := owner.page(t, api+"/channels?sort=name")
	if total != 6 || field(items, "name") != "alpha,bravo,charlie,delta,echo,foxtrot" {
		t.Fatalf("owner channels %d %s", total, field(items, "name"))
	}
	var seen []string
	for p := 1; p <= 3; p++ {
		total, page, items, _ := alice.page(t, fmt.Sprintf("%s/channels?page_size=1&page=%d&sort=name&order=desc", api, p))
		if total != 3 || page != p || len(items) != 1 {
			t.Fatalf("alice channels page %d: %d %d %v", p, total, page, items)
		}
		seen = append(seen, items[0]["name"].(string))
	}
	if strings.Join(seen, ",") != "foxtrot,echo,delta" {
		t.Fatalf("alice channel pages %v", seen)
	}
	if total, page, items, _ := alice.page(t, api+"/channels?page_size=2&page=50"); total != 3 || page != 2 || len(items) != 1 {
		t.Fatalf("clamp %d %d %v", total, page, items)
	}
	if total, _, items, _ := bob.page(t, api+"/channels"); total != 0 || len(items) != 0 {
		t.Fatalf("bob sees %d channels", total)
	}
	if total, _, items, _ := alice.page(t, api+"/templates?sort=channel&order=asc"); total != 2 || field(items, "name") != "t-alpha,t-bravo" {
		t.Fatalf("alice templates %d %s", total, field(items, "name"))
	}
	if total, _, _, _ := alice.page(t, api+"/templates?channel_id="+chans[0]); total != 0 {
		t.Fatalf("alice templates on foxtrot %d", total)
	}
	if total, _, _, _ := owner.page(t, api+"/templates?q=t-&sort=updated_at"); total != 6 {
		t.Fatalf("owner templates %d", total)
	}
	// Legacy cursor paging keeps its shape and reports the visible total.
	if total, _, items, body := alice.page(t, api+"/channels?limit=2"); total != 3 || len(items) != 2 || body["next_cursor"] != "echo" || body["page"] != nil {
		t.Fatalf("legacy %v", body)
	}

	// ---- messages: the sender scope applies to count and page.
	for i, who := range []string{alice.UserID, alice.UserID, bob.UserID} {
		uid := who
		if err := e.Notif.Repo.InsertMessage(ctx, store.Message{ID: store.NewID(), TenantID: tid, Title: fmt.Sprintf("m%d", i), Type: "notification", Status: "draft",
			SenderID: &uid, Recipients: []byte(`{"all":true}`), CreatedBy: &uid}); err != nil {
			t.Fatal(err)
		}
	}
	e.draft(owner, "owner note", map[string]any{"all": true}, nil)
	if total, _, items, _ := alice.page(t, api+"/messages?sort=subject"); total != 2 || field(items, "title") != "m0,m1" {
		t.Fatalf("alice messages %d %s", total, field(items, "title"))
	}
	if total, _, _, _ := owner.page(t, api+"/messages?page_size=1"); total != 4 {
		t.Fatalf("owner messages %d", total)
	}

	// ---- categories.
	e.category(owner, "zeta", 1)
	e.category(owner, "Eta", 3)
	e.category(owner, "theta", 2)
	if total, _, items, body := alice.page(t, api+"/categories"); total != 3 || field(items, "name") != "zeta,theta,Eta" || body["sort"] != "sort_order" {
		t.Fatalf("categories %v", body)
	}
	if _, _, items, _ := alice.page(t, api+"/categories?sort=name&page_size=2&page=2"); field(items, "name") != "zeta" {
		t.Fatalf("categories by name %s", field(items, "name"))
	}

	// ---- log: entries older than the default window are left out unless from widens it.
	now := time.Now()
	for i, at := range []time.Time{now.Add(-10 * 24 * time.Hour), now.Add(-time.Hour), now.Add(-2 * time.Hour)} {
		sender := owner.UserID
		if i == 2 {
			sender = alice.UserID
		}
		if err := e.Notif.Repo.InsertLog(ctx, store.LogRow{ID: store.NewID(), TenantID: tid, CreatedAt: at, ChannelID: chans[1], ChannelType: "email",
			Recipient: fmt.Sprintf("r%d@acme.test", i), Status: "sent", SenderKind: "user", SenderID: sender}); err != nil {
			t.Fatal(err)
		}
	}
	logTotal := func(s *Session, q string) int {
		t.Helper()
		_, _, items, body := s.page(t, api+"/notifications?page_size=200&"+q)
		if want := map[bool]string{true: "status", false: "created_at"}[strings.Contains(q, "sort=status")]; body["sort"] != want {
			t.Fatalf("log %v", body)
		}
		return len(Items(map[string]any{"items": anySlice(items)}))
	}
	if n := logTotal(owner, ""); n != 2 {
		t.Fatalf("default window %d", n)
	}
	from := url.QueryEscape(now.Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339))
	if n := logTotal(owner, "from="+from+"&sort=status"); n != 3 {
		t.Fatalf("widened %d", n)
	}
	if total, _, _, _ := alice.page(t, api+"/notifications?from="+from); total != 1 {
		t.Fatalf("alice own sends %d", total)
	}

	// ---- audit: equal timestamps page exactly once.
	ts := now.Add(-time.Minute).Truncate(time.Second)
	var rows []store.AuditRow
	for i := 0; i < 7; i++ {
		rows = append(rows, store.AuditRow{TS: ts, TenantID: tid, EventType: "category_deleted", ActorKind: "user", ActorID: owner.UserID, Outcome: "ok", CorrelationID: fmt.Sprintf("dup-%d", i), Details: []byte("{}")})
	}
	if err := e.Notif.Repo.InsertAuditRows(ctx, rows); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for p := 1; p <= 4; p++ {
		total, _, items, _ := owner.page(t, fmt.Sprintf("%s/audit?event_type=category_deleted&page_size=2&page=%d", api, p))
		if total != 7 {
			t.Fatalf("audit total %d", total)
		}
		for _, it := range items {
			got[fmt.Sprint(it["correlation_id"])]++
		}
	}
	if len(got) != 7 {
		t.Fatalf("audit pages %v", got)
	}
	for k, n := range got {
		if n != 1 {
			t.Fatalf("audit %s seen %d times", k, n)
		}
	}
	if total, _, items, _ := owner.page(t, api+"/audit?event_type=channel_created&order=asc&page_size=1"); total != 6 || items[0]["subject_name"] != "foxtrot" {
		t.Fatalf("audit channel_created %d %v", total, items)
	}

	// ---- 422 negatives name the parameter only.
	for _, c := range []struct{ path, param string }{
		{"/channels?sort=settings_sealed", "sort"}, {"/channels?order=up", "order"}, {"/channels?page=0", "page"}, {"/templates?page_size=201", "page_size"},
		{"/messages?sort=content", "sort"}, {"/notifications?sort=recipient", "sort"}, {"/categories?page=x", "page"}, {"/audit?sort=details", "sort"},
		{"/channels?cursor=a&page=1", "cursor"},
		// A window wider than 90 days (security review F-2), paged and legacy.
		{"/notifications?from=1970-01-01T00:00:00Z", "from"}, {"/notifications?from=1970-01-01T00:00:00Z&limit=5", "from"},
		{"/audit?from=1970-01-01T00:00:00Z&page=1", "from"}, {"/audit?from=1970-01-01T00:00:00Z&limit=5", "from"},
	} {
		code, body := owner.JSON(http.MethodGet, api+c.path, nil)
		detail, _ := body["detail"].(map[string]any)
		if code != 422 || body["reason"] != "validation_failed" || detail["param"] != c.param {
			t.Errorf("%s → %d %v", c.path, code, body)
		}
	}
}

func anySlice(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out
}
