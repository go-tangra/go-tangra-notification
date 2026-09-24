package messages

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/memstore"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

const (
	tA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	uA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c77"
	uB = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c88"
	uC = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c99"
)

type recPub struct {
	mu     sync.Mutex
	events []string
	err    error
}

func (p *recPub) Publish(_ context.Context, _ string, to []string, all bool, typ string, _ any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, typ+":"+strings.Join(to, ","))
	return p.err
}

type fx struct {
	ms  *memstore.Store
	aw  *audit.Writer
	svc *Service
	pub *recPub
	dir *StaticDirectory
	now time.Time
}

func newFx(t *testing.T) *fx {
	t.Helper()
	ms := memstore.New()
	aw := audit.NewWriter(ms, nil)
	t.Cleanup(aw.Close)
	f := &fx{ms: ms, aw: aw, pub: &recPub{}, dir: &StaticDirectory{Active: map[string]bool{uA: true, uB: true, uC: true}}, now: time.Unix(1_700_000_000, 0)}
	ms.Now = func() time.Time { return f.now }
	f.svc = New(ms, aw, f.dir, nil)
	f.svc.SetPublisher(f.pub)
	f.svc.SetClock(func() time.Time { return f.now })
	return f
}

func admin() authz.Subjects {
	return authz.Subjects{TenantID: tA, UserID: uA, Roles: []string{"admin"}}
}
func member() authz.Subjects {
	return authz.Subjects{TenantID: tA, UserID: uB, Roles: []string{"member"}}
}
func service() authz.Subjects {
	return authz.Subjects{TenantID: tA, Service: "spiffe://example.org/svc/warden"}
}

func (f *fx) audit(et audit.EventType) int {
	f.aw.Flush()
	return len(f.ms.AuditEvents(tA, string(et)))
}

func TestCategories(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	if _, err := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: ""}); err == nil {
		t.Fatal("empty name")
	}
	if _, err := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: strings.Repeat("x", 101)}); err == nil {
		t.Fatal("long name")
	}
	if _, err := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: "x", Description: strings.Repeat("d", 501)}); err == nil {
		t.Fatal("long description")
	}
	if _, err := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: "x", Sort: -1}); err == nil {
		t.Fatal("negative sort")
	}
	c, err := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: "Ops", Description: "d", Sort: 2})
	if err != nil || c.Name != "Ops" || c.CreatedBy != uA {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: "ops"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	c2, _ := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: "Alerts", Sort: 1})
	list, err := f.svc.ListCategories(ctx, member())
	if err != nil || len(list) != 2 || list[0].Name != "Alerts" {
		t.Fatalf("list %v %v", err, list)
	}
	if _, err := f.svc.UpdateCategory(ctx, admin(), c.ID, CategoryInput{Name: ""}); err == nil {
		t.Fatal("update validation")
	}
	if _, err := f.svc.UpdateCategory(ctx, admin(), c.ID, CategoryInput{Name: "alerts"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("rename clash: %v", err)
	}
	up, err := f.svc.UpdateCategory(ctx, admin(), c.ID, CategoryInput{Name: "Ops2", Sort: 9})
	if err != nil || up.Name != "Ops2" || up.Sort != 9 {
		t.Fatalf("update %v %+v", err, up)
	}
	if _, err := f.svc.UpdateCategory(ctx, admin(), uC, CategoryInput{Name: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	m, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "t", Content: "c", CategoryID: c.ID, Recipients: Recipients{All: true}})
	if err := f.svc.DeleteCategory(ctx, admin(), c.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("in use: %v", err)
	}
	_ = f.svc.Delete(ctx, admin(), m.ID, true)
	if err := f.svc.DeleteCategory(ctx, admin(), c.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteCategory(ctx, admin(), c2.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteCategory(ctx, admin(), c2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("gone: %v", err)
	}
	f.ms.FailOn("ListCategories", errors.New("down"))
	if _, err := f.svc.ListCategories(ctx, admin()); err == nil {
		t.Fatal("outage")
	}
	f.ms.FailOn("ListCategories", nil)
	f.ms.FailOn("InsertCategory", errors.New("down"))
	if _, err := f.svc.CreateCategory(ctx, admin(), CategoryInput{Name: "z"}); err == nil {
		t.Fatal("insert outage")
	}
	f.ms.FailOn("InsertCategory", nil)
	if f.audit(audit.CategoryCreated) != 2 || f.audit(audit.CategoryDeleted) != 2 || f.audit(audit.CategoryUpdated) != 1 {
		t.Fatal("audit")
	}
}

func TestMessageValidation(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	for name, in := range map[string]MessageInput{
		"title":      {Content: "c", Recipients: Recipients{All: true}},
		"long title": {Title: strings.Repeat("t", 201), Content: "c", Recipients: Recipients{All: true}},
		"content":    {Title: "t", Content: strings.Repeat("c", 64<<10+1), Recipients: Recipients{All: true}},
		"type":       {Title: "t", Type: "broadcast", Recipients: Recipients{All: true}},
		"recipients": {Title: "t"},
		"both":       {Title: "t", Recipients: Recipients{All: true, Users: []string{uA}}},
		"many":       {Title: "t", Recipients: Recipients{Users: make([]string, MaxRecipients+1)}},
		"empty id":   {Title: "t", Recipients: Recipients{Users: []string{""}}},
	} {
		var ve *ValidationError
		if _, err := f.svc.Create(ctx, admin(), in); !errors.As(err, &ve) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Duplicated recipients collapse; the default type is notification.
	m, err := f.svc.Create(ctx, admin(), MessageInput{Title: "t", Recipients: Recipients{Users: []string{uA, uA, uB}}})
	if err != nil || len(m.Recipients.Users) != 2 || m.Type != TypeNotification || m.Status != Draft || m.SenderID != uA {
		t.Fatalf("%v %+v", err, m)
	}
	if _, err := f.svc.Create(ctx, admin(), MessageInput{Title: "t", CategoryID: uC, Recipients: Recipients{All: true}}); err == nil {
		t.Fatal("unknown category")
	}
	if _, err := f.svc.Update(ctx, admin(), m.ID, MessageInput{Title: "t", CategoryID: uC, Recipients: Recipients{All: true}}, true); err == nil {
		t.Fatal("unknown category on update")
	}
	if _, err := f.svc.Update(ctx, admin(), m.ID, MessageInput{}, true); err == nil {
		t.Fatal("update validation")
	}
	if _, _, err := DecodeCursor("nope"); err == nil {
		t.Fatal("cursor")
	}
	if _, _, err := DecodeCursor("2020-13-99T00:00:00Z|x"); err == nil {
		t.Fatal("cursor date")
	}
	ts, id, err := DecodeCursor(EncodeCursor(f.now, "id1"))
	if err != nil || !ts.Equal(f.now) || id != "id1" {
		t.Fatal("cursor roundtrip")
	}
	if _, _, err := f.svc.List(ctx, admin(), ListFilter{Cursor: "bad"}, true); err == nil {
		t.Fatal("list cursor")
	}
}

func TestMessageLifecycle(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	// A service sender creates and manages its own messages.
	sm, err := f.svc.Create(ctx, service(), MessageInput{Title: "from warden", Recipients: Recipients{Users: []string{uA}}})
	if err != nil || sm.SenderID != "spiffe://example.org/svc/warden" {
		t.Fatalf("service create: %v %+v", err, sm)
	}
	if _, err := f.svc.Get(ctx, service(), sm.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, member(), sm.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("member sees service draft: %v", err)
	}
	// Member creates a draft; member listing is theirs only; a service without id lists nothing.
	m, _ := f.svc.Create(ctx, member(), MessageInput{Title: "hello", Content: "world", Recipients: Recipients{Users: []string{uA, uC, "unknown"}}})
	items, next, err := f.svc.List(ctx, member(), ListFilter{}, false)
	if err != nil || len(items) != 1 || next != "" {
		t.Fatalf("member list %v %v", err, items)
	}
	items, _, _ = f.svc.List(ctx, admin(), ListFilter{}, true)
	if len(items) != 2 {
		t.Fatalf("admin list %d", len(items))
	}
	items, _, _ = f.svc.List(ctx, authz.Subjects{TenantID: tA}, ListFilter{}, false)
	if len(items) != 0 {
		t.Fatal("anonymous list")
	}
	items, next, _ = f.svc.List(ctx, admin(), ListFilter{Limit: 1}, true)
	if len(items) != 1 || next == "" {
		t.Fatal("paging")
	}
	items, _, _ = f.svc.List(ctx, admin(), ListFilter{Limit: 1, Cursor: next}, true)
	if len(items) != 1 {
		t.Fatal("page 2")
	}
	items, _, _ = f.svc.List(ctx, admin(), ListFilter{Q: "HELLO", Status: Draft}, true)
	if len(items) != 1 || items[0].ID != m.ID {
		t.Fatal("search")
	}
	// Schedule in the future, then cancel; scheduling in the past publishes.
	future := f.now.Add(time.Hour)
	if _, err := f.svc.Update(ctx, member(), m.ID, MessageInput{Title: "hello", Content: "world", Recipients: Recipients{Users: []string{uA, uC, "unknown"}}, ScheduledAt: &future}, false); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.Send(ctx, member(), m.ID, false)
	if err != nil || res.Status != Scheduled {
		t.Fatalf("schedule %v %+v", err, res)
	}
	if _, err := f.svc.Send(ctx, member(), m.ID, false); !errors.Is(err, ErrState) {
		t.Fatalf("resend: %v", err)
	}
	// Updating a scheduled message without a future time returns it to draft.
	v, err := f.svc.Update(ctx, member(), m.ID, MessageInput{Title: "hello", Content: "world", Recipients: Recipients{Users: []string{uA, uC, "unknown"}}}, false)
	if err != nil || v.Status != Draft {
		t.Fatalf("unschedule by update %v %+v", err, v)
	}
	if _, err := f.svc.Cancel(ctx, member(), m.ID, false); !errors.Is(err, ErrState) {
		t.Fatalf("cancel draft: %v", err)
	}
	_, _ = f.svc.Update(ctx, member(), m.ID, MessageInput{Title: "hello", Content: "world", Recipients: Recipients{Users: []string{uA, uC, "unknown"}}, ScheduledAt: &future}, false)
	_, _ = f.svc.Send(ctx, member(), m.ID, false)
	if v, err := f.svc.Cancel(ctx, member(), m.ID, false); err != nil || v.Status != Draft {
		t.Fatalf("cancel %v %+v", err, v)
	}
	// Publish now: unknown recipient dropped, inbox rows for the active ones, live events.
	_, _ = f.svc.Update(ctx, member(), m.ID, MessageInput{Title: "hello", Content: "world", Recipients: Recipients{Users: []string{uA, uC, "unknown"}}}, false)
	res, err = f.svc.Send(ctx, member(), m.ID, false)
	if err != nil || res.Status != Published || res.RecipientCount != 2 || len(res.DroppedRecipients) != 1 || res.DroppedRecipients[0] != "unknown" {
		t.Fatalf("publish %v %+v", err, res)
	}
	if f.pub.events[0] != "inbox:"+uA+","+uC {
		t.Fatalf("events %v", f.pub.events)
	}
	got, _ := f.svc.Get(ctx, member(), m.ID, false)
	if got.RecipientCount != 2 || got.PublishedAt == nil {
		t.Fatalf("published view %+v", got)
	}
	recips, next, err := f.svc.Recipients(ctx, member(), m.ID, "", "", 1, false)
	if err != nil || len(recips) != 1 || next == "" {
		t.Fatalf("recipients %v %v", err, recips)
	}
	recips, next, _ = f.svc.Recipients(ctx, member(), m.ID, "", next, 100, false)
	if len(recips) != 1 || next != "" {
		t.Fatalf("recipients page 2 %v", recips)
	}
	if _, _, err := f.svc.Recipients(ctx, admin(), m.ID, "", "", 0, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign recipients: %v", err)
	}
	// Published messages cannot be edited, sent again or deleted.
	if _, err := f.svc.Update(ctx, member(), m.ID, MessageInput{Title: "x", Recipients: Recipients{All: true}}, false); !errors.Is(err, ErrState) {
		t.Fatalf("edit published: %v", err)
	}
	if err := f.svc.Delete(ctx, member(), m.ID, false); !errors.Is(err, ErrState) {
		t.Fatalf("delete published: %v", err)
	}
	if _, err := f.svc.Archive(ctx, admin(), sm.ID, true); !errors.Is(err, ErrState) {
		t.Fatalf("archive draft: %v", err)
	}
	// Read one entry, then revoke: only the unread recipient gets the event.
	_, _ = f.ms.SetInboxStatus(ctx, tA, uA, []string{recips[0].ID}, "read")
	rows, _ := f.ms.MessageRecipients(ctx, tA, m.ID, "", "", 10)
	for _, r := range rows {
		if r.RecipientID == uA {
			_, _ = f.ms.SetInboxStatus(ctx, tA, uA, []string{r.ID}, "read")
		}
	}
	if _, err := f.svc.Revoke(ctx, admin(), sm.ID, true); !errors.Is(err, ErrState) {
		t.Fatalf("revoke draft: %v", err)
	}
	v, err = f.svc.Revoke(ctx, member(), m.ID, false)
	if err != nil || v.Status != Revoked {
		t.Fatalf("revoke %v %+v", err, v)
	}
	if last := f.pub.events[len(f.pub.events)-1]; last != "inbox.revoked:"+uA+","+uC {
		t.Fatalf("revoke events %v", f.pub.events)
	}
	if n, _ := f.ms.InboxUnread(ctx, tA, uC); n != 0 {
		t.Fatal("revoked entry still unread")
	}
	if rows, _ := f.ms.MessageRecipients(ctx, tA, m.ID, "revoked", "", 10); len(rows) != 2 {
		t.Fatalf("revoked rows %d", len(rows))
	}
	// Archive a revoked message and delete it with its inbox rows.
	if v, err := f.svc.Archive(ctx, admin(), m.ID, true); err != nil || v.Status != Archived {
		t.Fatalf("archive %v %+v", err, v)
	}
	if err := f.svc.Delete(ctx, member(), m.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, member(), m.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("deleted message still readable")
	}
	if rows, _ := f.ms.MessageRecipients(ctx, tA, m.ID, "", "", 10); len(rows) != 0 {
		t.Fatal("inbox rows survived the delete")
	}
	// Everyone: the directory pages members; a directory outage fails the send.
	all, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "all", Recipients: Recipients{All: true}})
	res, err = f.svc.Send(ctx, admin(), all.ID, true)
	if err != nil || res.RecipientCount != 3 {
		t.Fatalf("all %v %+v", err, res)
	}
	f.dir.Err = errors.New("auth down")
	d1, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "d1", Recipients: Recipients{All: true}})
	if _, err := f.svc.Send(ctx, admin(), d1.ID, true); err == nil {
		t.Fatal("directory outage (all)")
	}
	d2, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "d2", Recipients: Recipients{Users: []string{uA}}})
	if _, err := f.svc.Send(ctx, admin(), d2.ID, true); err == nil {
		t.Fatal("directory outage (users)")
	}
	f.dir.Err = nil
	// A publisher failure never fails the publish.
	f.pub.err = errors.New("valkey down")
	d3, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "d3", Recipients: Recipients{Users: []string{uA}}})
	if res, err := f.svc.Send(ctx, admin(), d3.ID, true); err != nil || res.RecipientCount != 1 {
		t.Fatalf("publisher failure %v %+v", err, res)
	}
	f.pub.err = nil
	// Store outages surface.
	for _, op := range []string{"InsertInboxBatch", "SetMessageStatus", "GetMessage", "InsertMessage", "UpdateMessage", "DeleteMessage", "ListMessages", "MessageRecipients", "RevokeUnread"} {
		f.ms.FailOn(op, errors.New("down"))
		d, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "x", Recipients: Recipients{Users: []string{uA}}})
		_, e1 := f.svc.Send(ctx, admin(), d.ID, true)
		_, e2 := f.svc.Get(ctx, admin(), d.ID, true)
		_, e3 := f.svc.Update(ctx, admin(), d.ID, MessageInput{Title: "y", Recipients: Recipients{All: true}}, true)
		e4 := f.svc.Delete(ctx, admin(), d.ID, true)
		_, _, e5 := f.svc.List(ctx, admin(), ListFilter{}, true)
		_, _, e6 := f.svc.Recipients(ctx, admin(), d.ID, "", "", 0, true)
		_, e7 := f.svc.Revoke(ctx, admin(), d.ID, true)
		_, e8 := f.svc.Archive(ctx, admin(), d.ID, true)
		_, e9 := f.svc.Cancel(ctx, admin(), d.ID, true)
		f.ms.FailOn(op, nil)
		if e1 == nil && e2 == nil && e3 == nil && e4 == nil && e5 == nil && e6 == nil && e7 == nil && e8 == nil && e9 == nil {
			t.Errorf("%s outage not surfaced", op)
		}
	}
	if p, r, a, d := f.audit(audit.MessagePublished), f.audit(audit.MessageRevoked), f.audit(audit.MessageArchived), f.audit(audit.MessageDeleted); p < 3 || r < 1 || a < 1 || d < 1 {
		t.Fatalf("audit published=%d revoked=%d archived=%d deleted=%d", p, r, a, d)
	}
}

func TestDirectory(t *testing.T) {
	d := StaticDirectory{Active: map[string]bool{uA: true, uB: true}}
	ctx := context.Background()
	ids, err := d.Lookup(ctx, tA, []string{uA, uC})
	if err != nil || len(ids) != 1 || ids[0] != uA {
		t.Fatalf("%v %v", err, ids)
	}
	var pages [][]string
	if err := d.Members(ctx, tA, func(ids []string) error { pages = append(pages, ids); return nil }); err != nil || len(pages) != 1 || len(pages[0]) != 2 {
		t.Fatalf("%v %v", err, pages)
	}
	if err := d.Members(ctx, tA, func([]string) error { return errors.New("stop") }); err == nil {
		t.Fatal("callback error")
	}
	d.Err = errors.New("down")
	if _, err := d.Lookup(ctx, tA, nil); err == nil {
		t.Fatal("lookup outage")
	}
	if err := d.Members(ctx, tA, nil); err == nil {
		t.Fatal("members outage")
	}
	if (NopPublisher{}).Publish(ctx, tA, nil, true, "x", nil) != nil {
		t.Fatal("nop")
	}
}

func TestScheduler(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	ticks := 0
	w := NewScheduler(f.ms, f.svc, SchedulerConfig{Interval: 10 * time.Millisecond, Lease: time.Minute, Batch: 10}, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), func() { ticks++ })
	w.SetClock(func() time.Time { return f.now })
	w2 := NewScheduler(f.ms, f.svc, SchedulerConfig{}, nil, nil)
	if w2.cfg.Interval != 15*time.Second || w2.cfg.Lease != time.Minute || w2.cfg.Batch != 50 {
		t.Fatalf("defaults %+v", w2.cfg)
	}
	due := f.now.Add(-time.Minute)
	later := f.now.Add(time.Hour)
	m1, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "due", Recipients: Recipients{Users: []string{uA}}, ScheduledAt: &later})
	m2, _ := f.svc.Create(ctx, admin(), MessageInput{Title: "later", Recipients: Recipients{Users: []string{uB}}, ScheduledAt: &later})
	_, _ = f.svc.Send(ctx, admin(), m1.ID, true)
	_, _ = f.svc.Send(ctx, admin(), m2.ID, true)
	// Move m1's time into the past directly (it was scheduled with a future time).
	row, _ := f.ms.GetMessage(ctx, tA, m1.ID)
	row.ScheduledAt = &due
	_ = f.ms.UpdateMessage(ctx, row)
	if n, err := w.Once(ctx); err != nil || n != 1 || ticks != 1 {
		t.Fatalf("once %d %v %d", n, err, ticks)
	}
	got, _ := f.svc.Get(ctx, admin(), m1.ID, true)
	if got.Status != Published || got.RecipientCount != 1 {
		t.Fatalf("m1 %+v", got)
	}
	got, _ = f.svc.Get(ctx, admin(), m2.ID, true)
	if got.Status != Scheduled {
		t.Fatalf("m2 %+v", got)
	}
	if n, _ := w.Once(ctx); n != 0 {
		t.Fatal("nothing due")
	}
	// A publish failure leaves the lease to expire and is retried afterwards.
	row, _ = f.ms.GetMessage(ctx, tA, m2.ID)
	row.ScheduledAt = &due
	_ = f.ms.UpdateMessage(ctx, row)
	f.dir.Err = errors.New("auth down")
	if n, err := w.Once(ctx); err != nil || n != 0 {
		t.Fatalf("failed publish %d %v", n, err)
	}
	f.dir.Err = nil
	if n, _ := w.Once(ctx); n != 0 {
		t.Fatal("lease must hold")
	}
	f.now = f.now.Add(2 * time.Minute)
	if n, _ := w.Once(ctx); n != 1 {
		t.Fatal("retry after lease")
	}
	// Claim failures are reported; Run loops until cancelled.
	f.ms.FailOn("ClaimDueMessages", errors.New("down"))
	if _, err := w.Once(ctx); err == nil {
		t.Fatal("claim outage")
	}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { w.Run(rctx); close(done) }()
	time.Sleep(35 * time.Millisecond)
	cancel()
	<-done
	f.ms.FailOn("ClaimDueMessages", nil)
	if ticks < 4 {
		t.Fatalf("ticks %d", ticks)
	}
}
