package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (f *fx) category(t *testing.T, name string) string {
	t.Helper()
	var out map[string]any
	f.call(t, "POST", Prefix+"/categories", `{"name":"`+name+`","description":"d","sort":1}`, f.admin, 201, &out)
	return out["id"].(string)
}

func (f *fx) message(t *testing.T, tok, title, recipients string) string {
	t.Helper()
	var out map[string]any
	f.call(t, "POST", Prefix+"/messages", `{"title":"`+title+`","content":"body","recipients":`+recipients+`}`, tok, 201, &out)
	return out["id"].(string)
}

func TestCategoriesAndMessages(t *testing.T) {
	f := newFx(t)
	cat := f.category(t, "ops")
	f.call(t, "POST", Prefix+"/categories", `{"name":"ops"}`, f.admin, 409, nil)
	var list map[string]any
	f.call(t, "GET", Prefix+"/categories", "", f.member, 200, &list)
	if n := len(list["items"].([]any)); n != 1 {
		t.Fatalf("categories %d", n)
	}
	var cv map[string]any
	f.call(t, "PUT", Prefix+"/categories/"+cat, `{"name":"ops2","sort":5}`, f.admin, 200, &cv)
	if cv["name"] != "ops2" {
		t.Fatalf("updated: %v", cv)
	}
	f.call(t, "PUT", Prefix+"/categories/"+uC, `{"name":"x"}`, f.admin, 404, nil)
	// Draft with a category, to two users; the member (no manage) creates their own.
	var m map[string]any
	f.call(t, "POST", Prefix+"/messages", `{"title":"Maintenance","content":"Tonight","type":"group","category_id":"`+cat+`","recipients":{"users":["`+uB+`","`+uC+`"]}}`, f.admin, 201, &m)
	id := m["id"].(string)
	if m["status"] != "draft" || m["category_name"] != "ops2" || m["recipient_count"] != float64(0) {
		t.Fatalf("draft: %v", m)
	}
	f.call(t, "POST", Prefix+"/messages", `{"title":"x","content":"c","category_id":"`+uC+`","recipients":{"all":true}}`, f.admin, 422, nil)
	f.call(t, "POST", Prefix+"/messages", `{"title":"x","content":"c","recipients":{}}`, f.admin, 422, nil)
	// Category in use → 409 with detail.
	w := f.do("POST", Prefix+"/categories/"+cat+"/remove", "", f.admin)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "messages") {
		t.Fatalf("in use: %d %s", w.Code, w.Body)
	}
	mine := f.message(t, f.member, "Mine", `{"users":["`+uA+`"]}`)
	// Listing: manage sees everything, a member only their own; get honours the same.
	f.call(t, "GET", Prefix+"/messages", "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 2 {
		t.Fatalf("admin messages %d", n)
	}
	f.call(t, "GET", Prefix+"/messages?status=draft&q=min", "", f.member, 200, &list)
	if n := len(list["items"].([]any)); n != 1 {
		t.Fatalf("member messages %d", n)
	}
	f.call(t, "GET", Prefix+"/messages/"+id, "", f.member, 404, nil)
	f.call(t, "GET", Prefix+"/messages/"+id, "", f.admin, 200, nil)
	f.call(t, "GET", Prefix+"/messages/"+id, "", f.otherTen, 404, nil)
	// Update the draft; schedule it; cancel back to draft.
	f.call(t, "PUT", Prefix+"/messages/"+id, `{"title":"Maintenance!","content":"Tonight at 22:00","category_id":"`+cat+`","recipients":{"users":["`+uB+`","`+uC+`"]},"scheduled_at":"2099-01-01T00:00:00Z"}`, f.admin, 200, &m)
	if m["title"] != "Maintenance!" {
		t.Fatalf("updated: %v", m)
	}
	f.call(t, "PUT", Prefix+"/messages/"+id, `{"title":"nope","content":"c","recipients":{"all":true}}`, f.member, 404, nil)
	var res map[string]any
	f.call(t, "POST", Prefix+"/messages/"+id+"/send", "", f.admin, 200, &res)
	if res["status"] != "scheduled" {
		t.Fatalf("scheduled: %v", res)
	}
	f.call(t, "POST", Prefix+"/messages/"+id+"/send", "", f.admin, 409, nil)
	f.call(t, "POST", Prefix+"/messages/"+id+"/cancel", "", f.admin, 200, &m)
	if m["status"] != "draft" {
		t.Fatalf("cancel: %v", m)
	}
	// Publish now: two recipients, one inbox entry each; an unknown user is dropped.
	f.call(t, "PUT", Prefix+"/messages/"+id, `{"title":"Maintenance!","content":"Tonight","recipients":{"users":["`+uB+`","`+uC+`","`+tB+`"]}}`, f.admin, 200, nil)
	f.call(t, "POST", Prefix+"/messages/"+id+"/send", "", f.admin, 200, &res)
	if res["status"] != "published" || res["recipient_count"] != float64(2) || len(res["dropped_recipients"].([]any)) != 1 {
		t.Fatalf("published: %v", res)
	}
	f.call(t, "PUT", Prefix+"/messages/"+id, `{"title":"late","content":"c","recipients":{"all":true}}`, f.admin, 409, nil)
	f.call(t, "GET", Prefix+"/messages/"+id+"/recipients", "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 2 {
		t.Fatalf("recipients %d", n)
	}
	f.call(t, "GET", Prefix+"/messages/"+id+"/recipients?status=read", "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 0 {
		t.Fatalf("read recipients %d", n)
	}
	// The member's own message publishes to everyone (all=true handled by the directory).
	f.call(t, "PUT", Prefix+"/messages/"+mine, `{"title":"Mine","content":"c","recipients":{"all":true}}`, f.member, 200, nil)
	f.call(t, "POST", Prefix+"/messages/"+mine+"/send", "", f.member, 200, &res)
	if res["recipient_count"] != float64(3) {
		t.Fatalf("all: %v", res)
	}
	// Inbox for B: two entries, unread 2; read one; bulk status; delete.
	var page map[string]any
	f.call(t, "GET", Prefix+"/inbox", "", f.member, 200, &page)
	items := page["items"].([]any)
	if len(items) != 2 || page["unread"] != float64(2) {
		t.Fatalf("inbox: %v", page)
	}
	entry := items[0].(map[string]any)
	eid := entry["id"].(string)
	var un map[string]any
	f.call(t, "GET", Prefix+"/inbox/unread", "", f.member, 200, &un)
	if un["unread"] != float64(2) {
		t.Fatalf("unread: %v", un)
	}
	var e map[string]any
	f.call(t, "GET", Prefix+"/inbox/"+eid, "", f.member, 200, &e)
	if e["status"] != "read" || e["message"].(map[string]any)["content"] == "" {
		t.Fatalf("read: %v", e)
	}
	f.call(t, "GET", Prefix+"/inbox/"+eid, "", f.memberC, 404, nil)
	f.call(t, "GET", Prefix+"/inbox?status=unread", "", f.member, 200, &page)
	if len(page["items"].([]any)) != 1 || page["unread"] != float64(1) {
		t.Fatalf("unread page: %v", page)
	}
	var st map[string]any
	f.call(t, "POST", Prefix+"/inbox/status", `{"ids":["`+eid+`"],"status":"unread"}`, f.member, 200, &st)
	if st["updated"] != float64(1) || st["unread"] != float64(2) {
		t.Fatalf("status: %v", st)
	}
	f.call(t, "POST", Prefix+"/inbox/status", `{"ids":["`+eid+`"],"status":"received"}`, f.memberC, 200, &st)
	if st["updated"] != float64(0) {
		t.Fatalf("foreign status: %v", st)
	}
	f.call(t, "POST", Prefix+"/inbox/remove", `{"ids":["`+eid+`"]}`, f.member, 200, &st)
	if st["updated"] != float64(1) || st["unread"] != float64(1) {
		t.Fatalf("remove: %v", st)
	}
	f.call(t, "GET", Prefix+"/inbox/"+eid, "", f.member, 404, nil)
	// Revoke hides unread entries; archive; delete.
	f.call(t, "POST", Prefix+"/messages/"+id+"/revoke", "", f.member, 404, nil)
	f.call(t, "POST", Prefix+"/messages/"+id+"/revoke", "", f.admin, 200, &m)
	if m["status"] != "revoked" {
		t.Fatalf("revoke: %v", m)
	}
	f.call(t, "GET", Prefix+"/inbox", "", f.memberC, 200, &page)
	for _, it := range page["items"].([]any) {
		if it.(map[string]any)["message"].(map[string]any)["id"] == id {
			t.Fatalf("revoked entry still listed: %v", it)
		}
	}
	f.call(t, "POST", Prefix+"/messages/"+id+"/archive", "", f.admin, 200, &m)
	if m["status"] != "archived" {
		t.Fatalf("archive: %v", m)
	}
	f.call(t, "POST", Prefix+"/messages/"+id+"/archive", "", f.admin, 409, nil)
	f.call(t, "POST", Prefix+"/messages/"+id+"/remove", "", f.admin, 204, nil)
	f.call(t, "GET", Prefix+"/messages/"+id, "", f.admin, 404, nil)
	f.call(t, "POST", Prefix+"/messages/"+mine+"/remove", "", f.member, 409, nil) // published
	f.call(t, "POST", Prefix+"/categories/"+cat+"/remove", "", f.admin, 204, nil)
	f.call(t, "POST", Prefix+"/categories/"+cat+"/remove", "", f.admin, 404, nil)
	if n := f.audit("message_published"); n != 2 {
		t.Fatalf("audit published %d", n)
	}
}

func TestStreamRoute(t *testing.T) {
	f := newFx(t)
	serve := func(tok, lastID string) (*httptest.ResponseRecorder, context.CancelFunc, chan struct{}) {
		ctx, cancel := context.WithCancel(context.Background())
		r := httptest.NewRequest("GET", "https://localhost"+Prefix+"/stream", nil).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+tok)
		if lastID != "" {
			r.Header.Set("Last-Event-ID", lastID)
		}
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { f.s.Handler().ServeHTTP(w, r); close(done) }()
		return w, cancel, done
	}
	w, cancel, done := serve(f.member, "")
	deadline := time.Now().Add(3 * time.Second)
	for f.hub.OpenStreams() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.hub.OpenStreams() != 1 {
		t.Fatal("stream not open")
	}
	if _, err := f.hub.PublishID(context.Background(), tA, []string{uB}, false, "warden.secret", `{"id":1}`, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.hub.PublishID(context.Background(), tA, []string{uC}, false, "other", `{}`, true); err != nil {
		t.Fatal(err)
	}
	for !strings.Contains(w.Body.String(), "event: warden.secret") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(body, "retry: 3000") || !strings.Contains(body, `data: {"id":1}`) || strings.Contains(body, "event: other") {
		t.Fatalf("stream: %d %q", w.Code, body)
	}
	if n := f.audit("stream_opened"); n != 1 {
		t.Fatalf("audit opened %d", n)
	}
	// Replay from a stale id yields a reset event.
	w, cancel, done = serve(f.member, "1-0")
	for !strings.Contains(w.Body.String(), "event: reset") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(w.Body.String(), "replay_window") {
		t.Fatalf("reset: %q", w.Body.String())
	}
	// Over the per-user limit → 429 and a refused audit event.
	var cancels []context.CancelFunc
	var dones []chan struct{}
	for i := 0; i < 2; i++ {
		_, c, d := serve(f.memberC, "")
		cancels = append(cancels, c)
		dones = append(dones, d)
	}
	for f.hub.OpenStreams() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if w := f.do("GET", Prefix+"/stream", "", f.memberC); w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limited") {
		t.Fatalf("limit: %d %s", w.Code, w.Body)
	}
	for i, c := range cancels {
		c()
		<-dones[i]
	}
	if n := f.audit("stream_refused"); n != 1 {
		t.Fatalf("audit refused %d", n)
	}
	// Without a token the route is refused before any stream opens.
	if w := do(f.s, "GET", Prefix+"/stream", "", nil); w.Code != 401 {
		t.Fatalf("anon: %d", w.Code)
	}
}
