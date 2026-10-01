package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra/v4/listquery"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

type memQ struct {
	rows []store.AuditRow
	got  []any
	err  error
}

func (m *memQ) QueryAudit(_ context.Context, tenantID, eventType, actorID string, from, to, cursor time.Time, limit int) ([]store.AuditRow, error) {
	m.got = []any{tenantID, eventType, actorID, from, to, cursor, limit}
	if m.err != nil {
		return nil, m.err
	}
	if limit > len(m.rows) {
		limit = len(m.rows)
	}
	return m.rows[:limit], nil
}

func (m *memQ) PageAudit(_ context.Context, tenantID, eventType, actorID string, from, to time.Time, req listquery.Request) ([]store.AuditRow, int, listquery.Request, error) {
	m.got = []any{tenantID, eventType, actorID, from, to, req}
	if m.err != nil {
		return nil, 0, req, m.err
	}
	page, total, applied := listquery.Window(m.rows, req)
	return page, total, applied, nil
}

func TestQueryPage(t *testing.T) {
	base := time.Now()
	q := &memQ{}
	for i := 0; i < 5; i++ {
		q.rows = append(q.rows, store.AuditRow{TS: base.Add(-time.Duration(i) * time.Second), TenantID: "t", EventType: "notification_sent", ActorKind: "user", ActorID: "u", Outcome: "ok"})
	}
	// A zero request pages with the AuditList defaults; no from/to → the 7-day window.
	p, err := QueryPage(context.Background(), q, "t", Filter{}, listquery.Request{})
	if err != nil || p.Total != 5 || len(p.Items) != 5 || p.Sort != "ts" || p.Order != listquery.Desc || p.PageSize != 50 {
		t.Fatalf("%v %+v", err, p)
	}
	from, to := q.got[3].(time.Time), q.got[4].(time.Time)
	if d := to.Sub(from); d != store.LogWindow {
		t.Fatalf("window %v", d)
	}
	if string(p.Items[0].Details) != "{}" {
		t.Fatalf("details %s", p.Items[0].Details)
	}
	// Beyond the last page answers the last page.
	p, err = QueryPage(context.Background(), q, "t", Filter{EventType: "notification_sent", From: base.Add(-time.Hour), To: base}, listquery.Request{Page: 9, PageSize: 2, Sort: "ts", Order: listquery.Desc})
	if err != nil || p.Page != 3 || len(p.Items) != 1 || p.Total != 5 {
		t.Fatalf("%v %+v", err, p)
	}
	if !q.got[3].(time.Time).Equal(base.Add(-time.Hour)) || !q.got[4].(time.Time).Equal(base) {
		t.Fatalf("explicit window %v", q.got)
	}
	for _, f := range []Filter{{EventType: "nope"}, {From: base, To: base.Add(-time.Second)}} {
		if _, err := QueryPage(context.Background(), q, "t", f, listquery.Request{}); !errors.Is(err, ErrFilter) {
			t.Errorf("%+v: %v", f, err)
		}
	}
	q.err = errors.New("db")
	if _, err := QueryPage(context.Background(), q, "t", Filter{}, listquery.Request{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestQuery(t *testing.T) {
	base := time.Unix(1000, 0)
	q := &memQ{}
	for i := 0; i < 5; i++ {
		q.rows = append(q.rows, store.AuditRow{TS: base.Add(-time.Duration(i) * time.Second), TenantID: "t", EventType: "notification_sent", ActorKind: "user", ActorID: "u", Outcome: "ok"})
	}
	p, err := Query(context.Background(), q, "t", Filter{Limit: 2})
	if err != nil || len(p.Items) != 2 || p.NextCursor == "" {
		t.Fatalf("%v %+v", err, p)
	}
	if string(p.Items[0].Details) != "{}" {
		t.Fatalf("details %s", p.Items[0].Details)
	}
	// The resolved subject name is passed through.
	q.rows[0].SubjectName = "db"
	if p, err := Query(context.Background(), q, "t", Filter{Limit: 1}); err != nil || p.Items[0].SubjectName != "db" {
		t.Fatalf("subject name: %v %+v", err, p)
	}
	p2, err := Query(context.Background(), q, "t", Filter{Limit: 10, Cursor: p.NextCursor, EventType: "notification_sent", ActorID: "u", From: base.Add(-time.Hour), To: base})
	if err != nil || len(p2.Items) != 5 || p2.NextCursor != "" {
		t.Fatalf("%v %+v", err, p2)
	}
	if q.got[5].(time.Time).IsZero() || q.got[6] != 11 {
		t.Fatalf("args %v", q.got)
	}
	// Default limit and "to" bound.
	if _, err := Query(context.Background(), q, "t", Filter{Limit: 999}); err != nil || q.got[6] != 51 || q.got[4].(time.Time).IsZero() {
		t.Fatalf("defaults %v %v", err, q.got)
	}
	for _, f := range []Filter{{EventType: "nope"}, {Cursor: "abc"}, {From: base, To: base.Add(-time.Second)}} {
		if _, err := Query(context.Background(), q, "t", f); !errors.Is(err, ErrFilter) {
			t.Errorf("%+v: %v", f, err)
		}
	}
	q.err = errors.New("db")
	if _, err := Query(context.Background(), q, "t", Filter{}); err == nil {
		t.Fatal("expected error")
	}
}
