package store

import (
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
	from, to := Window(time.Time{}, time.Time{}, now)
	if !to.Equal(now.Add(time.Minute)) || to.Sub(from) != LogWindow {
		t.Fatalf("default %v %v", from, to)
	}
	explicit := now.Add(-30 * 24 * time.Hour)
	if from, to = Window(explicit, time.Time{}, now); !from.Equal(explicit) || !to.Equal(now.Add(time.Minute)) {
		t.Fatalf("from only %v %v", from, to)
	}
	if from, to = Window(time.Time{}, explicit, now); !to.Equal(explicit) || to.Sub(from) != LogWindow {
		t.Fatalf("to only %v %v", from, to)
	}
	if (Visible{}).Allows("x") {
		t.Fatal("zero Visible must show nothing")
	}
	if !(Visible{All: true}).Allows("x") || !(Visible{IDs: []string{"a", "x"}}).Allows("x") || (Visible{IDs: []string{"a"}}).Allows("x") {
		t.Fatal("Allows")
	}
}
