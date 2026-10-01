package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra/v4/listquery"
)

func TestListSpecs(t *testing.T) {
	specs := map[string]listquery.Spec{"channels": ChannelList, "templates": TemplateList, "messages": MessageList, "log": LogList, "categories": CategoryList, "audit": AuditList}
	for name, s := range specs {
		if err := s.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		// No sortable field orders by a sealed, secret or content column.
		for f, fd := range s.Fields {
			for _, bad := range []string{"settings", "sealed", "body", "content", "recipient", "details"} {
				if strings.Contains(fd.Expr, bad) {
					t.Errorf("%s.%s orders by %s", name, f, fd.Expr)
				}
			}
		}
	}
	// The contract's sortable fields (contracts/sortable-fields.md).
	want := map[string][]string{"channels": {"name", "type", "created_at"}, "templates": {"name", "channel", "updated_at"}, "messages": {"created_at", "subject", "status"},
		"log": {"created_at", "status", "channel"}, "categories": {"name", "sort_order"}, "audit": {"ts"}}
	for name, fields := range want {
		if len(specs[name].Fields) != len(fields) {
			t.Errorf("%s fields %v", name, specs[name].Fields)
		}
		for _, f := range fields {
			if _, ok := specs[name].Fields[f]; !ok {
				t.Errorf("%s lacks %s", name, f)
			}
		}
	}
	r := ListRequest(listquery.Request{}, MessageList)
	if r.Sort != "created_at" || r.Order != listquery.Desc || r.PageSize != 25 || r.Page != 1 {
		t.Fatalf("defaults %+v", r)
	}
	if r = ListRequest(listquery.Request{Sort: "bogus", PageSize: 5000}, ChannelList); r.Sort != "name" || r.PageSize != 25 {
		t.Fatalf("fallback %+v", r)
	}
	if got := ListRequest(listquery.Request{Sort: "ts"}, AuditList).OrderBy(AuditList); got != "a.ts DESC, a.ctid DESC" {
		t.Fatalf("audit order %q", got)
	}
	// Every sortable field is over a NOT NULL column (032 perf), so no ORDER BY
	// carries NULLS LAST and the (tenant_id, col) indexes serve both
	// directions; only the template channel name (a LEFT JOIN) is nullable.
	for name, s := range specs {
		for f, fd := range s.Fields {
			if nullable := name == "templates" && f == "channel"; fd.NotNull == nullable {
				t.Errorf("%s.%s NotNull=%v", name, f, fd.NotNull)
			}
		}
	}
	if got := ListRequest(listquery.Request{}, LogList).OrderBy(LogList); got != "created_at DESC, id DESC" {
		t.Fatalf("log default order %q", got)
	}
	if got := ListRequest(listquery.Request{Sort: "channel", Order: listquery.Desc}, TemplateList).OrderBy(TemplateList); got != "lower(c.name) DESC NULLS LAST, t.id DESC" {
		t.Fatalf("template channel order %q", got)
	}
}

func TestLogWhere(t *testing.T) {
	from, to := time.Unix(1, 0), time.Unix(2, 0)
	where, args := logWhere("t1", LogFilter{}, from, to)
	if strings.Contains(where, "''") || strings.Contains(where, " OR ") || len(args) != 3 {
		t.Fatalf("no filters: %q %v", where, args)
	}
	where, args = logWhere("t1", LogFilter{ChannelID: "c", TemplateID: "tp", Recipient: "a%b", Status: "failed", SenderID: "u"}, from, to)
	for _, want := range []string{"channel_id::text = $4", "template_id::text = $5", "recipient ILIKE '%' || $6 || '%'", "status = $7", "sender_id = $8"} {
		if !strings.Contains(where, want) {
			t.Errorf("all filters: %q lacks %q", where, want)
		}
	}
	if len(args) != 8 || args[5] != `a\%b` || args[7] != "u" {
		t.Fatalf("args %v", args)
	}
	where, args = logWhere("t1", LogFilter{SenderID: "u"}, from, to)
	if !strings.HasSuffix(where, " AND sender_id = $4") || len(args) != 4 {
		t.Fatalf("sender only: %q %v", where, args)
	}
}

func TestWindowAndVisible(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	from, to, err := Window(time.Time{}, time.Time{}, now)
	if err != nil || !to.Equal(now.Add(time.Minute)) || to.Sub(from) != LogWindow {
		t.Fatalf("default %v %v %v", from, to, err)
	}
	explicit := now.Add(-30 * 24 * time.Hour)
	if from, to, err = Window(explicit, time.Time{}, now); err != nil || !from.Equal(explicit) || !to.Equal(now.Add(time.Minute)) {
		t.Fatalf("from only %v %v %v", from, to, err)
	}
	if from, to, err = Window(time.Time{}, explicit, now); err != nil || !to.Equal(explicit) || to.Sub(from) != LogWindow {
		t.Fatalf("to only %v %v %v", from, to, err)
	}
	if MaxSpan != 90*24*time.Hour {
		t.Fatalf("MaxSpan %v", MaxSpan)
	}
	// Exactly MaxSpan passes, one day more is ErrSpan naming from (both with
	// an explicit to and with to defaulting to now).
	if from, to, err = Window(now.Add(-MaxSpan), now, now); err != nil || to.Sub(from) != MaxSpan {
		t.Fatalf("90 days %v %v %v", from, to, err)
	}
	if _, _, err = Window(now.Add(-MaxSpan), time.Time{}, now); err != nil {
		t.Fatalf("90 days, to absent: %v", err)
	}
	for _, to := range []time.Time{now, {}} {
		_, _, err = Window(now.Add(-MaxSpan-24*time.Hour), to, now)
		var le *listquery.Error
		if !errors.As(err, &le) || le.Param != "from" {
			t.Fatalf("91 days (to %v): %v", to, err)
		}
	}
	if _, _, err = Window(time.Unix(0, 0), time.Unix(0, 0).Add(MaxSpan), now); err != nil {
		t.Fatalf("old but narrow window: %v", err)
	}
	if (Visible{}).Allows("x") {
		t.Fatal("zero Visible must show nothing")
	}
	if !(Visible{All: true}).Allows("x") || !(Visible{IDs: []string{"a", "x"}}).Allows("x") || (Visible{IDs: []string{"a"}}).Allows("x") {
		t.Fatal("Allows")
	}
}
