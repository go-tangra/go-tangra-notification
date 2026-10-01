package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Feature 032: the list contract on every notification table (go-tangra
// specs/032-server-side-tables, contracts/http-list.md).

type pageBody struct {
	Items    []map[string]any `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Sort     string           `json:"sort"`
	Order    string           `json:"order"`
}

func (f *fx) page(t *testing.T, path, tok string) pageBody {
	t.Helper()
	var p pageBody
	f.call(t, "GET", path, "", tok, 200, &p)
	return p
}

func (f *fx) grant(t *testing.T, typ, id, user, rel string) {
	t.Helper()
	f.call(t, "POST", Prefix+"/grants", `{"resource_type":"`+typ+`","resource_id":"`+id+`","subject_type":"user","subject_id":"`+user+`","relation":"`+rel+`"}`, f.admin, 201, nil)
}

func names(p pageBody, key string) []string {
	out := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, fmt.Sprint(it[key]))
	}
	return out
}

// TestListChannelsTemplatesVisibility: a member with grants on some channels
// and templates sees exactly those, the total counts only those, and paging
// returns each once; administrators see every record.
func TestListChannelsTemplatesVisibility(t *testing.T) {
	f := newFx(t)
	var chIDs, tpIDs []string
	for _, n := range []string{"echo", "alpha", "delta", "charlie", "bravo"} {
		c := f.channel(t, n, false)
		chIDs = append(chIDs, c["id"].(string))
		tpIDs = append(tpIDs, f.template(t, "tpl-"+n, c["id"].(string))["id"].(string))
	}
	// uB reads echo, delta, bravo (channels) and two templates; uC nothing.
	for _, i := range []int{0, 2, 4} {
		f.grant(t, "channel", chIDs[i], uB, "viewer")
	}
	f.grant(t, "template", tpIDs[1], uB, "viewer")
	f.grant(t, "template", tpIDs[3], uB, "editor")

	p := f.page(t, Prefix+"/channels", f.admin)
	if p.Total != 5 || len(p.Items) != 5 || p.Sort != "name" || p.Order != "asc" || p.Page != 1 || p.PageSize != 25 {
		t.Fatalf("admin %+v", p)
	}
	if got := strings.Join(names(p, "name"), ","); got != "alpha,bravo,charlie,delta,echo" {
		t.Fatalf("order %s", got)
	}
	var seen []string
	for page := 1; page <= 3; page++ {
		p = f.page(t, Prefix+"/channels?page_size=1&sort=name&order=desc&page="+fmt.Sprint(page), f.member)
		if p.Total != 3 || len(p.Items) != 1 {
			t.Fatalf("member page %d: %+v", page, p)
		}
		seen = append(seen, names(p, "name")...)
	}
	if strings.Join(seen, ",") != "echo,delta,bravo" {
		t.Fatalf("member pages %v", seen)
	}
	// Beyond the end: the last page.
	if p = f.page(t, Prefix+"/channels?page=99&page_size=2", f.member); p.Page != 2 || len(p.Items) != 1 || p.Total != 3 {
		t.Fatalf("clamp %+v", p)
	}
	if p = f.page(t, Prefix+"/channels", f.memberC); p.Total != 0 || len(p.Items) != 0 || p.Page != 1 {
		t.Fatalf("no grants %+v", p)
	}
	// Permissions decorate each visible row.
	if p = f.page(t, Prefix+"/channels?page_size=1", f.member); p.Items[0]["permissions"].(map[string]any)["read"] != true || p.Items[0]["permissions"].(map[string]any)["write"] != false {
		t.Fatalf("permissions %+v", p.Items[0])
	}

	p = f.page(t, Prefix+"/templates?sort=channel&order=desc", f.member)
	if p.Total != 2 || strings.Join(names(p, "name"), ",") != "tpl-charlie,tpl-alpha" {
		t.Fatalf("member templates %+v", p)
	}
	if p = f.page(t, Prefix+"/templates?q=tpl-a", f.member); p.Total != 1 {
		t.Fatalf("member template search %+v", p)
	}
	if p = f.page(t, Prefix+"/templates?sort=updated_at", f.admin); p.Total != 5 || p.Order != "desc" {
		t.Fatalf("admin templates %+v", p)
	}
	if p = f.page(t, Prefix+"/templates", f.memberC); p.Total != 0 {
		t.Fatalf("no template grants %+v", p)
	}
	// Other tenants never see tenant A.
	if p = f.page(t, Prefix+"/channels", f.otherTen); p.Total != 0 {
		t.Fatalf("other tenant %+v", p)
	}
}

// TestListLegacyShape: cursor/limit alone keep {items,next_cursor} plus total
// (counting only visible records); mixing styles is 422 on "cursor".
func TestListLegacyShape(t *testing.T) {
	f := newFx(t)
	var ids []string
	for _, n := range []string{"a1", "a2", "a3"} {
		ids = append(ids, f.channel(t, n, false)["id"].(string))
	}
	f.grant(t, "channel", ids[0], uB, "viewer")
	f.grant(t, "channel", ids[2], uB, "viewer")
	var legacy map[string]any
	f.call(t, "GET", Prefix+"/channels?limit=1", "", f.member, 200, &legacy)
	if legacy["total"] != float64(2) || legacy["next_cursor"] != "a1" || len(legacy["items"].([]any)) != 1 || legacy["page"] != nil {
		t.Fatalf("legacy %v", legacy)
	}
	f.call(t, "GET", Prefix+"/channels?limit=1&cursor=a1", "", f.member, 200, &legacy)
	if items := legacy["items"].([]any); len(items) != 1 || items[0].(map[string]any)["name"] != "a3" {
		t.Fatalf("legacy next %v", legacy)
	}
	for _, path := range []string{"/channels", "/templates", "/messages", "/notifications", "/audit"} {
		f.call(t, "GET", Prefix+path+"?limit=5", "", f.admin, 200, &legacy)
		if _, ok := legacy["total"]; !ok {
			t.Errorf("%s legacy total missing: %v", path, legacy)
		}
		w := f.do("GET", Prefix+path+"?cursor=x&page=1", "", f.admin)
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"param":"cursor"`) {
			t.Errorf("%s mixed: %d %s", path, w.Code, w.Body)
		}
	}
}

// TestListValidation: invalid paging and sort parameters answer 422
// validation_failed naming the parameter, never echoing the value.
func TestListValidation(t *testing.T) {
	f := newFx(t)
	cases := []struct{ path, query, param string }{
		{"/channels", "sort=settings_sealed", "sort"},
		{"/channels", "sort=name%3Bdrop%20table%20channels", "sort"},
		{"/channels", "order=sideways", "order"},
		{"/channels", "page=0", "page"},
		{"/channels", "page=abc", "page"},
		{"/channels", "page=99999999999", "page"},
		{"/channels", "page_size=0", "page_size"},
		{"/channels", "page_size=201", "page_size"},
		{"/templates", "sort=body", "sort"},
		{"/templates", "page_size=-1", "page_size"},
		{"/messages", "sort=content", "sort"},
		{"/messages", "order=DESC", "order"},
		{"/notifications", "sort=rendered_body", "sort"},
		{"/notifications", "sort=recipient", "sort"},
		{"/notifications", "from=yesterday", "from"},
		{"/notifications", "to=2026-13-40T00:00:00Z", "to"},
		{"/categories", "sort=description", "sort"},
		{"/categories", "page=-3", "page"},
		{"/audit", "sort=details", "sort"},
		{"/audit", "from=nope", "from"},
		{"/audit", "page_size=500", "page_size"},
	}
	for _, c := range cases {
		w := f.do("GET", Prefix+c.path+"?"+c.query, "", f.admin)
		var body struct {
			Reason string         `json:"reason"`
			Detail map[string]any `json:"detail"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 422 || body.Reason != "validation_failed" || body.Detail["param"] != c.param {
			t.Errorf("%s?%s: %d %s", c.path, c.query, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "drop") || strings.Contains(w.Body.String(), "settings_sealed") {
			t.Errorf("%s?%s echoed: %s", c.path, c.query, w.Body)
		}
	}
}

// TestListMessagesCategories: messages respect the sender scope in the total;
// categories page and sort.
func TestListMessagesCategories(t *testing.T) {
	f := newFx(t)
	f.message(t, f.member, "Bravo", `{"all":true}`)
	f.message(t, f.member, "alpha", `{"all":true}`)
	f.message(t, f.admin, "Charlie", `{"all":true}`)
	f.message(t, f.memberC, "delta", `{"all":true}`)
	p := f.page(t, Prefix+"/messages?sort=subject", f.member)
	if p.Total != 2 || strings.Join(names(p, "title"), ",") != "alpha,Bravo" {
		t.Fatalf("own messages %+v", p)
	}
	if p = f.page(t, Prefix+"/messages?page_size=1&page=2", f.admin); p.Total != 4 || len(p.Items) != 1 || p.Sort != "created_at" || p.Order != "desc" {
		t.Fatalf("manage %+v", p)
	}
	if p = f.page(t, Prefix+"/messages?status=draft&sort=status&order=asc", f.admin); p.Total != 4 {
		t.Fatalf("status filter %+v", p)
	}

	for i, n := range []string{"zulu", "Yankee", "xray"} {
		f.call(t, "POST", Prefix+"/categories", fmt.Sprintf(`{"name":%q,"sort":%d}`, n, 3-i), f.admin, 201, nil)
	}
	p = f.page(t, Prefix+"/categories", f.member)
	if p.Total != 3 || p.Sort != "sort_order" || strings.Join(names(p, "name"), ",") != "xray,Yankee,zulu" {
		t.Fatalf("categories %+v", p)
	}
	if p = f.page(t, Prefix+"/categories?sort=name&order=desc&page_size=2&page=2", f.member); p.Total != 3 || strings.Join(names(p, "name"), ",") != "xray" {
		t.Fatalf("categories by name %+v", p)
	}
}

// TestListLogWindow: the log pages within the last 7 days unless from/to
// widen it; callers without stats:read count only their own sends.
func TestListLogWindow(t *testing.T) {
	f := newFx(t)
	c := f.channel(t, "relay", false)
	id := c["id"].(string)
	f.grant(t, "channel", id, uB, "editor")
	sendTest := func(tok string) {
		f.call(t, "POST", Prefix+"/channels/"+id+"/test", `{"recipient":"ops@example.org"}`, tok, 200, nil)
	}
	sendTest(f.admin)
	sendTest(f.member)
	start := f.now
	f.now = f.now.Add(8 * 24 * time.Hour)
	sendTest(f.admin)

	p := f.page(t, Prefix+"/notifications", f.admin)
	if p.Total != 1 || p.Sort != "created_at" || p.Order != "desc" {
		t.Fatalf("default window %+v", p)
	}
	from := url.QueryEscape(start.Add(-time.Hour).UTC().Format(time.RFC3339))
	if p = f.page(t, Prefix+"/notifications?from="+from+"&sort=status", f.admin); p.Total != 3 {
		t.Fatalf("widened %+v", p)
	}
	if p = f.page(t, Prefix+"/notifications?from="+from, f.member); p.Total != 1 {
		t.Fatalf("own sends %+v", p)
	}
	if p = f.page(t, Prefix+"/notifications?from="+from+"&sort=channel&page_size=2&page=5", f.admin); p.Page != 2 || len(p.Items) != 1 {
		t.Fatalf("clamp %+v", p)
	}
	to := url.QueryEscape(start.Add(time.Hour).UTC().Format(time.RFC3339))
	if p = f.page(t, Prefix+"/notifications?to="+to, f.admin); p.Total != 2 {
		t.Fatalf("to only %+v", p)
	}
	w := f.do("GET", Prefix+"/notifications?from="+to+"&to="+from, "", f.admin)
	if w.Code != 422 {
		t.Fatalf("inverted window %d %s", w.Code, w.Body)
	}
}

// TestListAuditPaging: audit pages newest first; walking every page returns
// each event exactly once and the total matches.
func TestListAuditPaging(t *testing.T) {
	f := newFx(t)
	for _, n := range []string{"c1", "c2", "c3", "c4"} {
		f.channel(t, n, false)
	}
	f.aw.Flush()
	p := f.page(t, Prefix+"/audit", f.admin)
	if p.Total < 4 || p.Sort != "ts" || p.Order != "desc" || p.PageSize != 50 {
		t.Fatalf("audit %+v", p)
	}
	total := p.Total
	seen := map[string]int{}
	for page := 1; page <= total; page++ {
		q := f.page(t, Prefix+"/audit?page_size=1&page="+fmt.Sprint(page), f.admin)
		if q.Total != total || len(q.Items) != 1 {
			t.Fatalf("page %d %+v", page, q)
		}
		b, _ := json.Marshal(q.Items[0])
		seen[string(b)]++
	}
	if len(seen) != total {
		t.Fatalf("distinct %d of %d", len(seen), total)
	}
	if p = f.page(t, Prefix+"/audit?event_type=channel_created&order=asc", f.admin); p.Total != 4 || p.Items[0]["subject_name"] != "c1" {
		t.Fatalf("filtered %+v", p)
	}
}

// TestListStoreFailures: a failing count/page or grant lookup answers 503
// without rows (never an unfiltered list).
func TestListStoreFailures(t *testing.T) {
	f := newFx(t)
	f.channel(t, "relay", false)
	boom := errors.New("db down")
	for _, c := range []struct{ op, path, tok string }{
		{"PageChannels", "/channels", f.admin}, {"PageTemplates", "/templates", f.admin}, {"GrantsForSubjects", "/channels", f.member},
		{"GrantsForSubjects", "/templates", f.member}, {"PageMessages", "/messages", f.admin}, {"PageCategories", "/categories", f.admin},
		{"PageLog", "/notifications", f.admin}, {"PageAudit", "/audit", f.admin}, {"ListChannels", "/channels?limit=5", f.admin},
	} {
		f.ms.FailOn(c.op, boom)
		w := f.do("GET", Prefix+c.path, "", c.tok)
		if w.Code != 503 || strings.Contains(w.Body.String(), "items") {
			t.Errorf("%s %s: %d %s", c.op, c.path, w.Code, w.Body)
		}
		f.ms.FailOn(c.op, nil)
	}
	// An unknown channel type filter is a validation error on the new path too.
	if w := f.do("GET", Prefix+"/channels?type=fax&page=1", "", f.admin); w.Code != 422 {
		t.Fatalf("type %d %s", w.Code, w.Body)
	}
}
