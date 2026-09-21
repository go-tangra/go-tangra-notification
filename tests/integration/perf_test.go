//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-freya/freya/services/notification/internal/authz"
	"github.com/go-freya/freya/services/notification/internal/messages"
	"github.com/go-freya/freya/services/notification/internal/store"
)

// TestPerformance checks the budgets of SC-005/SC-006/SC-007 on the real
// stack: a 10,000-member "everyone" fan-out under 60 s, the 601st send of
// a minute refused, and delivery under 2 s with many open streams (T088).
func TestPerformance(t *testing.T) {
	if getenv("NOTIFICATION_PERF") == "" {
		t.Skip("set NOTIFICATION_PERF=1 to run the performance budget checks")
	}
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	// 10,000-member fan-out: a synthetic directory over the real store and publisher.
	members := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		members[store.NewID()] = true
	}
	svc := messages.New(e.Notif.Repo, e.Notif.Audit, messages.StaticDirectory{Active: members}, e.Notif.Hub)
	subj := authz.Subjects{TenantID: tid, UserID: owner.UserID, Roles: []string{"owner"}}
	m, err := svc.Create(context.Background(), subj, messages.MessageInput{Title: "All", Recipients: messages.Recipients{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res, err := svc.Send(context.Background(), subj, m.ID, true)
	if err != nil || res.RecipientCount != 10000 {
		t.Fatalf("fan-out %v %+v", err, res)
	}
	if d := time.Since(start); d > 60*time.Second {
		t.Fatalf("fan-out took %s", d)
	}
	t.Logf("10,000-member fan-out in %s", time.Since(start))
	// 601st send in a minute refused (the harness raised the per-sender limit to 600 too).
	relay := e.EmailChannel(owner, "relay", true)
	tpl := e.Template(owner, "welcome", relay)
	body := map[string]any{"template_id": tpl, "recipient": "perf@acme.test", "variables": map[string]string{"Name": "P"}}
	start = time.Now()
	limited := false
	for i := 0; i < 601; i++ {
		code, _ := owner.JSON(http.MethodPost, api+"/notifications/send", body)
		if code == 429 {
			limited = i == 600
			break
		}
		if code != 200 {
			t.Fatalf("send %d → %d", i, code)
		}
	}
	if !limited {
		t.Fatal("601st send not refused")
	}
	t.Logf("600 sends in %s", time.Since(start))
	// Delivery under 2 s with 500 open streams (100 members × 5 streams).
	var streams []*SSE
	var users []*Session
	for i := 0; i < 100; i++ {
		u := e.Invite(owner, fmt.Sprintf("u%03d@acme.test", i), "member")
		users = append(users, u)
		for j := 0; j < 5; j++ {
			s, code := u.Stream("")
			if code != 200 {
				t.Fatalf("stream → %d", code)
			}
			streams = append(streams, s)
		}
	}
	defer func() {
		for _, s := range streams {
			s.Close()
		}
	}()
	id := e.draft(owner, "Broadcast", map[string]any{"all": true}, nil)
	start = time.Now()
	if code, _ := owner.JSON(http.MethodPost, api+"/messages/"+id+"/send", nil); code != 200 {
		t.Fatal("send")
	}
	for _, s := range streams {
		if _, ok := s.Next("inbox", 5*time.Second); !ok {
			t.Fatal("a stream missed the event")
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("delivery to 500 streams took %s", d)
	}
	t.Logf("delivery to 500 streams in %s", time.Since(start))
	_ = users
}
