package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/memstore"
	"github.com/go-tangra/go-tangra-notification/v4/internal/messages"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

const (
	tA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	uA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c77"
	uB = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c88"
)

func TestInbox(t *testing.T) {
	ms := memstore.New()
	aw := audit.NewWriter(ms, nil)
	t.Cleanup(aw.Close)
	now := time.Unix(1_700_000_000, 0)
	ms.Now = func() time.Time { now = now.Add(time.Second); return now }
	msgs := messages.New(ms, aw, messages.StaticDirectory{Active: map[string]bool{uA: true, uB: true}}, nil)
	svc := New(ms, aw)
	ctx := context.Background()
	admin := authz.Subjects{TenantID: tA, UserID: uA, Roles: []string{"admin"}}
	me := authz.Subjects{TenantID: tA, UserID: uB}
	cat, _ := msgs.CreateCategory(ctx, admin, messages.CategoryInput{Name: "ops"})
	var ids []string
	for i := 0; i < 3; i++ {
		m, _ := msgs.Create(ctx, admin, messages.MessageInput{Title: "m", Content: "secret body", CategoryID: cat.ID, Recipients: messages.Recipients{Users: []string{uB}}})
		_, _ = msgs.Send(ctx, admin, m.ID, true)
		ids = append(ids, m.ID)
	}
	svcMsg, _ := msgs.Create(ctx, authz.Subjects{TenantID: tA, Service: "svc"}, messages.MessageInput{Title: "svc", Recipients: messages.Recipients{Users: []string{uB}}})
	_, _ = msgs.Send(ctx, authz.Subjects{TenantID: tA, Service: "svc"}, svcMsg.ID, false)
	page, err := svc.List(ctx, me, "", "", 2)
	if err != nil || len(page.Items) != 2 || page.Unread != 4 || page.NextCursor == "" {
		t.Fatalf("%v %+v", err, page)
	}
	if page.Items[0].Message.ID != svcMsg.ID || page.Items[0].Message.SenderID != "svc" || page.Items[1].Message.CategoryName != "ops" || page.Items[1].Message.SenderID != uA {
		t.Fatalf("order/view %+v", page.Items)
	}
	page2, _ := svc.List(ctx, me, "all", page.NextCursor, 10)
	if len(page2.Items) != 2 || page2.NextCursor != "" {
		t.Fatalf("page 2 %+v", page2)
	}
	if _, err := svc.List(ctx, me, "archived", "", 10); !errors.Is(err, ErrInput) {
		t.Fatal("status")
	}
	if _, err := svc.List(ctx, me, "", "bad", 10); !errors.Is(err, ErrInput) {
		t.Fatal("cursor")
	}
	// Read marks the entry read once and audits once.
	e := page.Items[1]
	got, err := svc.Read(ctx, me, e.ID)
	if err != nil || got.Status != "read" || got.ReadAt == nil || got.Message.Content != "secret body" {
		t.Fatalf("read %v %+v", err, got)
	}
	if got, _ = svc.Read(ctx, me, e.ID); got.Status != "read" {
		t.Fatal("re-read")
	}
	if _, err := svc.Read(ctx, admin, e.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign read: %v", err)
	}
	if n, _ := svc.Unread(ctx, me); n != 3 {
		t.Fatalf("unread %d", n)
	}
	unread, _ := svc.List(ctx, me, "unread", "", 10)
	read, _ := svc.List(ctx, me, "read", "", 10)
	if len(unread.Items) != 3 || len(read.Items) != 1 {
		t.Fatalf("filters %d %d", len(unread.Items), len(read.Items))
	}
	// Bulk status.
	if _, _, err := svc.SetStatus(ctx, me, nil, "read"); !errors.Is(err, ErrInput) {
		t.Fatal("empty ids")
	}
	if _, _, err := svc.SetStatus(ctx, me, []string{e.ID}, "deleted"); !errors.Is(err, ErrInput) {
		t.Fatal("bad status")
	}
	if _, _, err := svc.SetStatus(ctx, me, make([]string, 501), "read"); !errors.Is(err, ErrInput) {
		t.Fatal("too many")
	}
	all := []string{}
	for _, it := range append(page.Items, page2.Items...) {
		all = append(all, it.ID)
	}
	updated, n, err := svc.SetStatus(ctx, me, all, "read")
	if err != nil || updated != 4 || n != 0 { // rows already read count as updated (SQL semantics)
		t.Fatalf("mark read %d %d %v", updated, n, err)
	}
	updated, n, _ = svc.SetStatus(ctx, me, all[:2], "unread")
	if updated != 2 || n != 2 {
		t.Fatalf("mark unread %d %d", updated, n)
	}
	updated, n, _ = svc.SetStatus(ctx, me, all[:1], "received")
	if updated != 1 || n != 2 {
		t.Fatalf("received %d %d", updated, n)
	}
	// Remove hides; unknown ids count zero.
	if _, _, err := svc.Remove(ctx, me, nil); !errors.Is(err, ErrInput) {
		t.Fatal("remove empty")
	}
	updated, n, err = svc.Remove(ctx, me, []string{all[0], uA})
	if err != nil || updated != 1 || n != 1 {
		t.Fatalf("remove %d %d %v", updated, n, err)
	}
	if _, err := svc.Read(ctx, me, all[0]); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("removed entry readable")
	}
	updated, _, _ = svc.Remove(ctx, me, []string{all[0]})
	if updated != 0 {
		t.Fatal("remove twice")
	}
	aw.Flush()
	if n := len(ms.AuditEvents(tA, "inbox_read")); n != 2 {
		t.Fatalf("audit read %d", n)
	}
	if n := len(ms.AuditEvents(tA, "inbox_deleted")); n != 1 {
		t.Fatalf("audit deleted %d", n)
	}
	// Outages.
	for _, op := range []string{"InboxPage", "InboxUnread", "GetInboxEntry", "SetInboxStatus"} {
		ms.FailOn(op, errors.New("down"))
		_, e1 := svc.List(ctx, me, "", "", 10)
		_, e2 := svc.Unread(ctx, me)
		_, e3 := svc.Read(ctx, me, all[1])
		_, _, e4 := svc.SetStatus(ctx, me, all[1:2], "read")
		_, _, e5 := svc.Remove(ctx, me, all[1:2])
		ms.FailOn(op, nil)
		if e1 == nil && e2 == nil && e3 == nil && e4 == nil && e5 == nil {
			t.Errorf("%s outage not surfaced", op)
		}
	}
	_ = ids
}
