//go:build integration

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	notificationv1 "github.com/go-tangra/go-tangra-notification/v4/api/proto/notification/v1"
)

// TestStream covers the live stream through the gateway: inbox events reach
// the recipient's streams only within 2 s, module publishes, Last-Event-ID
// replay, reset beyond the window, the per-person limit and sign-out
// (SC-005, SC-007).
func TestStream(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	alice := e.Invite(owner, "alice@acme.test", "member")
	bob := e.Invite(owner, "bob@acme.test", "member")
	a1, code := alice.Stream("")
	if code != 200 {
		t.Fatalf("stream → %d", code)
	}
	defer a1.Close()
	a2, _ := alice.Stream("")
	defer a2.Close()
	b1, _ := bob.Stream("")
	defer b1.Close()
	id := e.draft(owner, "Ping", map[string]any{"users": []string{alice.UserID}}, nil)
	start := time.Now()
	if code, _ := owner.JSON(http.MethodPost, api+"/messages/"+id+"/send", nil); code != 200 {
		t.Fatal("send")
	}
	ev1, ok1 := a1.Next("inbox", 5*time.Second)
	ev2, ok2 := a2.Next("inbox", 5*time.Second)
	if !ok1 || !ok2 || !strings.Contains(ev1.Data, id) || ev1.ID == "" || ev1.ID != ev2.ID {
		t.Fatalf("alice streams: %+v %+v", ev1, ev2)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("delivery took %s", d)
	}
	if ev, ok := b1.Next("inbox", 700*time.Millisecond); ok {
		t.Fatalf("bob received alice's event: %+v", ev)
	}
	// Module publish (gRPC over the Freya channel, as warden would do it): one user, then everyone.
	conn, err := e.Notif.Freya.Client(context.Background(), "notification")
	if err != nil {
		t.Fatal(err)
	}
	events := notificationv1.NewEventsClient(conn)
	res, err := events.Publish(context.Background(), &notificationv1.PublishRequest{TenantId: tid, UserIds: []string{bob.UserID}, Type: "warden.secret-rotated", Data: []byte(`{"secret":"s1"}`)})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if ev, ok := b1.Next("warden.secret-rotated", 5*time.Second); !ok || ev.ID != res.GetEventId() || !strings.Contains(ev.Data, "s1") {
		t.Fatalf("bob module event %+v", ev)
	}
	if _, ok := a1.Next("warden.secret-rotated", 500*time.Millisecond); ok {
		t.Fatal("alice received bob's event")
	}
	if _, err := events.Publish(context.Background(), &notificationv1.PublishRequest{TenantId: tid, All: true, Type: "maintenance", Data: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := a1.Next("maintenance", 5*time.Second); !ok {
		t.Fatal("broadcast missed alice")
	}
	if _, ok := b1.Next("maintenance", 5*time.Second); !ok {
		t.Fatal("broadcast missed bob")
	}
	if _, err := events.Publish(context.Background(), &notificationv1.PublishRequest{TenantId: tid, All: true, Type: "inbox"}); err == nil {
		t.Fatal("reserved type accepted")
	}
	// Reconnect with Last-Event-ID replays what was published during the gap.
	last := ev1.ID
	a1.Close()
	a2.Close()
	for i := 0; i < 3; i++ {
		if _, err := events.Publish(context.Background(), &notificationv1.PublishRequest{TenantId: tid, UserIds: []string{alice.UserID}, Type: "gap", Data: []byte(`{"n":` + string(rune('0'+i)) + `}`)}); err != nil {
			t.Fatal(err)
		}
	}
	a3, code := alice.Stream(last)
	if code != 200 {
		t.Fatalf("reconnect → %d", code)
	}
	defer a3.Close()
	seen := 0
	for seen < 3 {
		ev, ok := a3.Next("", 5*time.Second)
		if !ok {
			t.Fatalf("replay stopped after %d events", seen)
		}
		if ev.Type == "gap" {
			seen++
		} else if ev.Type == "reset" {
			t.Fatalf("unexpected reset: %+v", ev)
		}
	}
	// An id older than the window yields a reset.
	a4, _ := alice.Stream("1-0")
	if ev, ok := a4.Next("reset", 5*time.Second); !ok || !strings.Contains(ev.Data, "replay_window") {
		t.Fatalf("reset %+v", ev)
	}
	a4.Close()
	// Sixth stream refused (a3 + four more = 5 open).
	var extra []*SSE
	for i := 0; i < 4; i++ {
		s, code := alice.Stream("")
		if code != 200 {
			t.Fatalf("stream %d → %d", i, code)
		}
		extra = append(extra, s)
	}
	time.Sleep(300 * time.Millisecond)
	if _, code := alice.Stream(""); code != 429 {
		t.Fatalf("sixth stream → %d", code)
	}
	for _, s := range extra {
		s.Close()
	}
	// After sign-out the session no longer authorizes a stream (the open one
	// closes when its session lapses or at maxAge; a new one is refused now).
	if code, _ := alice.JSON(http.MethodPost, "/api/v1/signout", nil); code/100 != 2 {
		t.Log("signout status", code)
	}
	if _, code := alice.Stream(""); code != 401 {
		t.Fatalf("stream after sign-out → %d", code)
	}
	if e.AuditCount(tid, "stream_opened", "ok") < 8 || e.AuditCount(tid, "stream_refused", "refused") != 1 || e.AuditCount(tid, "event_published", "ok") < 5 {
		t.Fatalf("audit opened=%d refused=%d published=%d", e.AuditCount(tid, "stream_opened", "ok"), e.AuditCount(tid, "stream_refused", "refused"), e.AuditCount(tid, "event_published", "ok"))
	}
}

// TestNotifierRPC covers module-to-module sending over the Freya channel:
// tenant-wide use, SendTest, and the outcome in the log with the service actor.
func TestNotifierRPC(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	relay := e.EmailChannel(owner, "relay", true)
	tpl := e.Template(owner, "welcome", relay)
	conn, err := e.Notif.Freya.Client(context.Background(), "notification")
	if err != nil {
		t.Fatal(err)
	}
	c := notificationv1.NewNotifierClient(conn)
	if _, err := c.Send(context.Background(), &notificationv1.SendRequest{TenantId: tid, TemplateId: tpl, Recipient: "svc@acme.test", Variables: map[string]string{"Name": "Svc"}}); err == nil {
		t.Fatal("service sent without tenant-wide use")
	}
	if code, out := e.Grant(owner, "template", tpl, "tenant", "", "sharer", nil); code != 201 {
		t.Fatalf("tenant grant %d %v", code, out)
	}
	res, err := c.Send(context.Background(), &notificationv1.SendRequest{TenantId: tid, TemplateId: tpl, Recipient: "svc@acme.test", Variables: map[string]string{"Name": "Svc"}, CorrelationId: "corr-1"})
	if err != nil || res.GetStatus() != notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT {
		t.Fatalf("send %v %+v", err, res)
	}
	if mail := e.LastMail("svc@acme.test"); !strings.Contains(mail, "Hi Svc") {
		t.Fatalf("mail %q", mail)
	}
	code, entry := owner.JSON(http.MethodGet, api+"/notifications/"+res.GetLogId(), nil)
	if code != 200 || entry["sender_kind"] != "service" || !strings.Contains(entry["sender_id"].(string), "spiffe://") {
		t.Fatalf("log %d %v", code, entry)
	}
	if _, err := c.SendTest(context.Background(), &notificationv1.SendTestRequest{TenantId: tid, ChannelId: relay, Recipient: "t@acme.test"}); err == nil {
		t.Fatal("service test-sent without write")
	}
	rows := e.AuditRows(tid, "notification_sent")
	if len(rows) != 1 || rows[0].ActorKind != "service" {
		t.Fatalf("audit %+v", rows)
	}
}
