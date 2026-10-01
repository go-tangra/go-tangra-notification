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
	if got := ListRequest(listquery.Request{Sort: "ts"}, AuditList).OrderBy(AuditList); got != "a.ts DESC NULLS LAST, a.ctid DESC" {
		t.Fatalf("audit order %q", got)
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
