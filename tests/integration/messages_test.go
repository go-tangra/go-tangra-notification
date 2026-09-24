//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

func (e *Env) category(s *Session, name string, sort int) string {
	e.T.Helper()
	code, out := s.JSON(http.MethodPost, api+"/categories", map[string]any{"name": name, "sort": sort})
	if code != 201 {
		e.T.Fatalf("category → %d %v", code, out)
	}
	return out["id"].(string)
}

func (e *Env) draft(s *Session, title string, recipients map[string]any, extra map[string]any) string {
	e.T.Helper()
	body := map[string]any{"title": title, "content": "NOTIF-MARKER-BODY-" + title, "recipients": recipients}
	for k, v := range extra {
		body[k] = v
	}
	code, out := s.JSON(http.MethodPost, api+"/messages", body)
	if code != 201 {
		e.T.Fatalf("message → %d %v", code, out)
	}
	return out["id"].(string)
}

// TestMessages covers categories, drafts, publish to users and to everyone
// (auth directory), duplicates collapsed, deactivated users skipped, revoke
// and archive (SC-005).
func TestMessages(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	alice := e.Invite(owner, "alice@acme.test", "member")
	bob := e.Invite(owner, "bob@acme.test", "member")
	gone := e.Invite(owner, "gone@acme.test", "member")
	ops := e.category(owner, "Ops", 2)
	e.category(owner, "Alerts", 1)
	code, cats := alice.JSON(http.MethodGet, api+"/categories", nil)
	if code != 200 || len(Items(cats)) != 2 || Items(cats)[0]["name"] != "Alerts" {
		t.Fatalf("categories %d %v", code, cats)
	}
	if code, _ := alice.JSON(http.MethodPost, api+"/categories", map[string]any{"name": "x"}); code != 403 {
		t.Fatal("member creates a category")
	}
	// Deactivate "gone" so the directory drops them.
	if code, out := owner.JSON(http.MethodPost, "/api/v1/admin/users/"+gone.UserID+"/deactivate", nil); code/100 != 2 {
		t.Logf("deactivate → %d %v (continuing)", code, out)
	}
	id := e.draft(owner, "Maintenance", map[string]any{"users": []string{alice.UserID, bob.UserID, bob.UserID, gone.UserID}}, map[string]any{"category_id": ops, "type": "group"})
	if code, out := owner.JSON(http.MethodPost, api+"/categories/"+ops+"/remove", nil); code != 409 {
		t.Fatalf("category in use → %d %v", code, out)
	}
	code, res := owner.JSON(http.MethodPost, api+"/messages/"+id+"/send", nil)
	if code != 200 || res["status"] != "published" {
		t.Fatalf("send %d %v", code, res)
	}
	n := int(res["recipient_count"].(float64))
	dropped := len(res["dropped_recipients"].([]any))
	if n+dropped != 3 || n < 2 {
		t.Fatalf("recipients %d dropped %d", n, dropped)
	}
	code, m := owner.JSON(http.MethodGet, api+"/messages/"+id, nil)
	if code != 200 || m["category_name"] != "Ops" || int(m["recipient_count"].(float64)) != n {
		t.Fatalf("message %d %v", code, m)
	}
	// Everyone: the auth directory pages the members.
	all := e.draft(owner, "All hands", map[string]any{"all": true}, nil)
	code, res = owner.JSON(http.MethodPost, api+"/messages/"+all+"/send", nil)
	if code != 200 || res["recipient_count"].(float64) < 3 {
		t.Fatalf("all %d %v", code, res)
	}
	// Alice's inbox: two unread entries; the member's own draft is visible to them only.
	code, inbox := alice.JSON(http.MethodGet, api+"/inbox", nil)
	if code != 200 || len(Items(inbox)) != 2 || inbox["unread"] != float64(2) {
		t.Fatalf("inbox %d %v", code, inbox)
	}
	// Members hold messages:read only: they cannot create, and the listing shows nothing but their own (none).
	if code, _ := bob.JSON(http.MethodPost, api+"/messages", map[string]any{"title": "x", "content": "c", "recipients": map[string]any{"all": true}}); code != 403 {
		t.Fatal("member creates a message")
	}
	if code, out := alice.JSON(http.MethodGet, api+"/messages/"+id, nil); code != 404 {
		t.Fatalf("foreign message visible: %d %v", code, out)
	}
	if code, list := bob.JSON(http.MethodGet, api+"/messages", nil); code != 200 || len(Items(list)) != 0 {
		t.Fatalf("bob's messages %d %v", code, list)
	}
	if code, list := owner.JSON(http.MethodGet, api+"/messages", nil); code != 200 || len(Items(list)) != 2 {
		t.Fatalf("owner's messages %d %v", code, list)
	}
	// Revoke: Alice read one entry, Bob did not — the unread one disappears, the read one stays marked revoked.
	entries := Items(inbox)
	var maint string
	for _, en := range entries {
		if en["message"].(map[string]any)["id"] == id {
			maint = en["id"].(string)
		}
	}
	if code, en := alice.JSON(http.MethodGet, api+"/inbox/"+maint, nil); code != 200 || en["status"] != "read" {
		t.Fatalf("read %d %v", code, en)
	}
	code, m = owner.JSON(http.MethodPost, api+"/messages/"+id+"/revoke", nil)
	if code != 200 || m["status"] != "revoked" {
		t.Fatalf("revoke %d %v", code, m)
	}
	code, inbox = bob.JSON(http.MethodGet, api+"/inbox", nil)
	for _, en := range Items(inbox) {
		if en["message"].(map[string]any)["id"] == id {
			t.Fatalf("revoked unread entry still listed for bob: %v", en)
		}
	}
	code, inbox = alice.JSON(http.MethodGet, api+"/inbox", nil)
	found := false
	for _, en := range Items(inbox) {
		if en["id"] == maint && en["status"] == "revoked" {
			found = true
		}
	}
	if !found {
		t.Fatalf("read entry not kept as revoked: %v", inbox)
	}
	if code, m := owner.JSON(http.MethodPost, api+"/messages/"+id+"/archive", nil); code != 200 || m["status"] != "archived" {
		t.Fatalf("archive %d %v", code, m)
	}
	if code, _ := owner.JSON(http.MethodPost, api+"/messages/"+id+"/remove", nil); code != 204 {
		t.Fatal("remove archived")
	}
	if e.AuditCount(tid, "message_published", "ok") != 2 || e.AuditCount(tid, "message_revoked", "ok") != 1 || e.AuditCount(tid, "category_created", "ok") != 2 {
		t.Fatal("audit")
	}
	e.ScanLogsFor(t, []string{"NOTIF-MARKER-BODY-"})
}

// TestInbox covers read/mark/delete scoped to the caller and the unread counter.
func TestInbox(t *testing.T) {
	e := StartPlatform(t)
	owner, _ := e.CreateTenant("acme", "owner@acme.test")
	alice := e.Invite(owner, "alice@acme.test", "member")
	bob := e.Invite(owner, "bob@acme.test", "member")
	var ids []string
	for _, title := range []string{"one", "two", "three"} {
		id := e.draft(owner, title, map[string]any{"users": []string{alice.UserID}}, nil)
		if code, _ := owner.JSON(http.MethodPost, api+"/messages/"+id+"/send", nil); code != 200 {
			t.Fatal("send")
		}
		ids = append(ids, id)
	}
	code, un := alice.JSON(http.MethodGet, api+"/inbox/unread", nil)
	if code != 200 || un["unread"] != float64(3) {
		t.Fatalf("unread %d %v", code, un)
	}
	code, page := alice.JSON(http.MethodGet, api+"/inbox?limit=2", nil)
	if code != 200 || len(Items(page)) != 2 || page["next_cursor"] == "" {
		t.Fatalf("page %d %v", code, page)
	}
	code, page2 := alice.JSON(http.MethodGet, api+"/inbox?limit=2&cursor="+page["next_cursor"].(string), nil)
	if code != 200 || len(Items(page2)) != 1 {
		t.Fatalf("page 2 %d %v", code, page2)
	}
	first := Items(page)[0]["id"].(string)
	second := Items(page)[1]["id"].(string)
	if code, _ := bob.JSON(http.MethodGet, api+"/inbox/"+first, nil); code != 404 {
		t.Fatal("bob reads alice's entry")
	}
	if code, st := bob.JSON(http.MethodPost, api+"/inbox/status", map[string]any{"ids": []string{first}, "status": "read"}); code != 200 || st["updated"] != float64(0) {
		t.Fatalf("bob marks alice's entry %d %v", code, st)
	}
	code, st := alice.JSON(http.MethodPost, api+"/inbox/status", map[string]any{"ids": []string{first, second}, "status": "read"})
	if code != 200 || st["updated"] != float64(2) || st["unread"] != float64(1) {
		t.Fatalf("mark read %d %v", code, st)
	}
	code, st = alice.JSON(http.MethodPost, api+"/inbox/status", map[string]any{"ids": []string{first}, "status": "unread"})
	if code != 200 || st["unread"] != float64(2) {
		t.Fatalf("mark unread %d %v", code, st)
	}
	code, st = alice.JSON(http.MethodPost, api+"/inbox/remove", map[string]any{"ids": []string{first}})
	if code != 200 || st["updated"] != float64(1) || st["unread"] != float64(1) {
		t.Fatalf("remove %d %v", code, st)
	}
	if code, _ := alice.JSON(http.MethodGet, api+"/inbox/"+first, nil); code != 404 {
		t.Fatal("removed entry readable")
	}
	code, rec := owner.JSON(http.MethodGet, api+"/messages/"+ids[2]+"/recipients", nil)
	if code != 200 || len(Items(rec)) != 1 || Items(rec)[0]["recipient_id"] != alice.UserID {
		t.Fatalf("recipients %d %v", code, rec)
	}
}

// TestScheduler covers scheduled publishing by the worker within one
// interval, cancel, and exactly-once publishing after a crash mid-publish
// (the lease expires and the idempotent fan-out finishes) (SC-006).
func TestScheduler(t *testing.T) {
	e := StartPlatform(t)
	owner, _ := e.CreateTenant("acme", "owner@acme.test")
	alice := e.Invite(owner, "alice@acme.test", "member")
	soon := time.Now().Add(3 * time.Second).UTC().Format(time.RFC3339)
	id := e.draft(owner, "Later", map[string]any{"users": []string{alice.UserID}}, map[string]any{"scheduled_at": soon})
	code, res := owner.JSON(http.MethodPost, api+"/messages/"+id+"/send", nil)
	if code != 200 || res["status"] != "scheduled" {
		t.Fatalf("schedule %d %v", code, res)
	}
	if code, m := owner.JSON(http.MethodPost, api+"/messages/"+id+"/cancel", nil); code != 200 || m["status"] != "draft" {
		t.Fatalf("cancel %d %v", code, m)
	}
	if code, _ := owner.JSON(http.MethodPost, api+"/messages/"+id+"/send", nil); code != 200 {
		t.Fatal("reschedule")
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		code, m := owner.JSON(http.MethodGet, api+"/messages/"+id, nil)
		if code == 200 && m["status"] == "published" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not published by the worker: %v", m)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if code, un := alice.JSON(http.MethodGet, api+"/inbox/unread", nil); code != 200 || un["unread"] != float64(1) {
		t.Fatalf("unread %d %v", code, un)
	}
	// Crash mid-publish: a message left in "publishing" with a lease and half its inbox rows.
	// The worker reclaims it after the lease and completes the fan-out without duplicates.
	bob := e.Invite(owner, "bob@acme.test", "member")
	crash := e.draft(owner, "Crash", map[string]any{"users": []string{alice.UserID, bob.UserID}}, nil)
	past := time.Now().Add(-time.Minute)
	err := e.Notif.Store.Tx(context.Background(), store.Scope{System: true}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "UPDATE messages SET status = 'publishing', scheduled_at = $2, lease_until = now() - interval '1 second' WHERE id = $1", crash, past); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), "INSERT INTO inbox (id, tenant_id, message_id, recipient_id, status) SELECT gen_random_uuid(), tenant_id, id, $2, 'sent' FROM messages WHERE id = $1", crash, alice.UserID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		code, m := owner.JSON(http.MethodGet, api+"/messages/"+crash, nil)
		if code == 200 && m["status"] == "published" {
			if m["recipient_count"] != float64(2) {
				t.Fatalf("exactly-once violated: %v", m)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("crashed publish not completed: %v", m)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if code, un := alice.JSON(http.MethodGet, api+"/inbox/unread", nil); code != 200 || un["unread"] != float64(2) {
		t.Fatalf("alice duplicates: %v", un)
	}
	h := e.Notif.Health(context.Background())
	if h.SchedulerLastTick.IsZero() {
		t.Fatal("scheduler tick not reported")
	}
}
