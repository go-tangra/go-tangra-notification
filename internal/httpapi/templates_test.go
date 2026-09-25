package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestSystemTemplatesHTTP (US4, T056): read model fields, 422 reasons on
// PUT, 409 on remove, restore (200 / 409 for ordinary templates / 403
// without write); log entries carry the template key.
func TestSystemTemplatesHTTP(t *testing.T) {
	f := newFx(t)
	if _, err := f.tp.EnsureSystemTemplates(context.Background(), tA); err != nil {
		t.Fatal(err)
	}
	var list map[string]any
	f.call(t, "GET", Prefix+"/templates?q=auth.invite", "", f.admin, 200, &list)
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list %v", list)
	}
	tp := items[0].(map[string]any)
	id := tp["id"].(string)
	if tp["system_key"] != "auth.invite" || tp["edited"] != false || tp["channel_id"] != nil || tp["permissions"].(map[string]any)["delete"] != false {
		t.Fatalf("read model %v", tp)
	}
	if req, _ := json.Marshal(tp["required_variables"]); string(req) != `["link","valid_for"]` {
		t.Fatalf("required %s", req)
	}
	if sec, _ := json.Marshal(tp["secret_variables"]); string(sec) != `["link"]` {
		t.Fatalf("secret %s", sec)
	}
	// Edit subject/body only (channel_id null, as the UI sends it).
	var got map[string]any
	f.call(t, "PUT", Prefix+"/templates/"+id, `{"name":"auth.invite","channel_id":null,"subject":"Welcome to Tangra","body":"<p>{{.link}} {{.valid_for}}</p>","variables":["link","valid_for","tenant"]}`, f.admin, 200, &got)
	if got["subject"] != "Welcome to Tangra" || got["edited"] != true {
		t.Fatalf("edit %v", got)
	}
	w := f.do("PUT", Prefix+"/templates/"+id, `{"name":"auth.invite","subject":"s","body":"<p>{{.valid_for}}</p>"}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"reason":"missing_required_variable"`) || !strings.Contains(w.Body.String(), `"variable":"link"`) {
		t.Fatalf("missing link %d %s", w.Code, w.Body)
	}
	w = f.do("PUT", Prefix+"/templates/"+id, `{"name":"renamed","subject":"s","body":"{{.link}} {{.valid_for}}"}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"reason":"system_template_field"`) {
		t.Fatalf("rename %d %s", w.Code, w.Body)
	}
	w = f.do("POST", Prefix+"/templates/"+id+"/remove", "", f.admin)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"system_template"`) {
		t.Fatalf("remove %d %s", w.Code, w.Body)
	}
	// A member without write gets 403 on restore.
	f.call(t, "POST", Prefix+"/templates/"+id+"/restore", "", f.member, 403, nil)
	f.call(t, "POST", Prefix+"/templates/"+id+"/restore", "", f.admin, 200, &got)
	if got["edited"] != false || !strings.Contains(got["body"].(string), "Accept the invitation") {
		t.Fatalf("restore %v", got)
	}
	// Ordinary templates cannot be restored; they report no system fields.
	ch := f.channel(t, "relay", true)
	plain := f.template(t, "welcome", ch["id"].(string))
	if plain["system_key"] != nil || plain["edited"] != false {
		t.Fatalf("plain %v", plain)
	}
	w = f.do("POST", Prefix+"/templates/"+plain["id"].(string)+"/restore", "", f.admin)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"not_system_template"`) {
		t.Fatalf("restore ordinary %d %s", w.Code, w.Body)
	}
	// Ordinary templates still need a channel.
	w = f.do("PUT", Prefix+"/templates/"+plain["id"].(string), `{"name":"welcome","subject":"s","body":"b"}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "channel_id") {
		t.Fatalf("ordinary without channel %d %s", w.Code, w.Body)
	}
}
