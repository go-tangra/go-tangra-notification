package httpapi

import (
	"errors"
	"strings"
	"testing"
)

func TestChannelsLifecycle(t *testing.T) {
	f := newFx(t)
	ch := f.channel(t, "relay", true)
	id := ch["id"].(string)
	settings := ch["settings"].(map[string]any)
	if settings["password"] != "__set__" || settings["host"] != "relay" || ch["permissions"].(map[string]any)["delete"] != true {
		t.Fatalf("created: %v", ch)
	}
	// Duplicate name → 409.
	f.call(t, "POST", Prefix+"/channels", `{"name":"relay","type":"email","settings":{"host":"h","port":25,"from":"a@b.c"}}`, f.admin, 409, nil)
	// Invalid settings for the type → 422 with detail.
	w := f.do("POST", Prefix+"/channels", `{"name":"bad","type":"email","settings":{"port":25,"from":"a@b.c"}}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "validation_failed") {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	// Read: the default channel's tenant sharer grant lets a member see the redacted view.
	var got map[string]any
	f.call(t, "GET", Prefix+"/channels/"+id, "", f.member, 200, &got)
	if got["settings"].(map[string]any)["password"] != "__set__" || got["permissions"].(map[string]any)["write"] != false {
		t.Fatalf("member view: %v", got)
	}
	// Another tenant never sees it.
	f.call(t, "GET", Prefix+"/channels/"+id, "", f.otherTen, 404, nil)
	// Non-default channel is invisible to a member without a grant.
	other := f.channel(t, "second", false)
	f.call(t, "GET", Prefix+"/channels/"+other["id"].(string), "", f.member, 404, nil)
	var list map[string]any
	f.call(t, "GET", Prefix+"/channels?type=email", "", f.member, 200, &list)
	if n := len(list["items"].([]any)); n != 1 {
		t.Fatalf("member list %d", n)
	}
	f.call(t, "GET", Prefix+"/channels", "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 2 {
		t.Fatalf("admin list %d", n)
	}
	// Update keeps the stored password with the marker and never echoes it.
	f.call(t, "PUT", Prefix+"/channels/"+id, `{"name":"relay2","type":"email","settings":{"host":"relay","port":587,"from":"noreply@example.org","password":"__set__"},"enabled":false}`, f.admin, 200, &got)
	if got["name"] != "relay2" || got["enabled"] != false {
		t.Fatalf("updated: %v", got)
	}
	// Member cannot write.
	f.call(t, "PUT", Prefix+"/channels/"+id, `{"name":"x","type":"email","settings":{"host":"relay","port":587,"from":"noreply@example.org"}}`, f.member, 403, nil)
	// Test send works while disabled (that is what it is for); regular sends do not.
	f.call(t, "POST", Prefix+"/channels/"+id+"/test", `{"recipient":"ops@example.org"}`, f.admin, 200, nil)
	f.email.sent = nil
	f.call(t, "PUT", Prefix+"/channels/"+id, `{"name":"relay2","type":"email","settings":{"host":"relay","port":587,"from":"noreply@example.org"},"enabled":true,"is_default":true}`, f.admin, 200, nil)
	var entry map[string]any
	f.call(t, "POST", Prefix+"/channels/"+id+"/test", `{"recipient":"ops@example.org"}`, f.admin, 200, &entry)
	if entry["status"] != "sent" || entry["test"] != true || len(f.email.sent) != 1 {
		t.Fatalf("test send: %v", entry)
	}
	// Provider failure → 200 with status failed and a scrubbed error.
	f.email.fail = errors.New("535 auth failed for NOTIF-MARKER-PW-relay")
	f.call(t, "POST", Prefix+"/channels/"+id+"/test", `{"recipient":"ops@example.org"}`, f.admin, 200, &entry)
	if entry["status"] != "failed" || strings.Contains(entry["error"].(string), "NOTIF-MARKER") {
		t.Fatalf("failed send: %v", entry)
	}
	f.email.fail = nil
	// A type without a provider → 422 no_provider on test.
	var sms map[string]any
	f.call(t, "POST", Prefix+"/channels", `{"name":"sms","type":"sms","settings":{"api_key":"NOTIF-MARKER-PW-sms"},"enabled":true}`, f.admin, 201, &sms)
	w = f.do("POST", Prefix+"/channels/"+sms["id"].(string)+"/test", `{"recipient":"+15550001"}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "no_provider") {
		t.Fatalf("no provider: %d %s", w.Code, w.Body)
	}
	// Delete refused while a template references it, allowed afterwards.
	tp := f.template(t, "welcome", id)
	w = f.do("POST", Prefix+"/channels/"+id+"/remove", "", f.admin)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"templates":1`) {
		t.Fatalf("in use: %d %s", w.Code, w.Body)
	}
	f.call(t, "POST", Prefix+"/templates/"+tp["id"].(string)+"/remove", "", f.admin, 204, nil)
	f.call(t, "POST", Prefix+"/channels/"+id+"/remove", "", f.member, 403, nil)
	f.call(t, "POST", Prefix+"/channels/"+id+"/remove", "", f.admin, 204, nil)
	f.call(t, "GET", Prefix+"/channels/"+id, "", f.admin, 404, nil)
	if n := f.audit("channel_created"); n != 3 {
		t.Fatalf("audit channel_created %d", n)
	}
}

func TestTemplatesLifecycle(t *testing.T) {
	f := newFx(t)
	ch := f.channel(t, "relay", true)
	cid := ch["id"].(string)
	tp := f.template(t, "welcome", cid)
	id := tp["id"].(string)
	if tp["channel_name"] != "relay" || tp["channel_type"] != "email" {
		t.Fatalf("created: %v", tp)
	}
	// Syntax error → 422 with position; undeclared variable → detail.variable.
	w := f.do("POST", Prefix+"/templates", `{"name":"bad","channel_id":"`+cid+`","subject":"{{.Name","body":"x"}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "position") {
		t.Fatalf("syntax: %d %s", w.Code, w.Body)
	}
	w = f.do("POST", Prefix+"/templates", `{"name":"bad","channel_id":"`+cid+`","subject":"{{.Other}}","body":"x","variables":["Name"]}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"variable":"Other"`) {
		t.Fatalf("undeclared: %d %s", w.Code, w.Body)
	}
	// Unknown channel → 404.
	f.call(t, "POST", Prefix+"/templates", `{"name":"x","channel_id":"`+uC+`","subject":"s","body":"b"}`, f.admin, 404, nil)
	// Member without a grant on the template cannot see it; a viewer grant lets them.
	f.call(t, "GET", Prefix+"/templates/"+id, "", f.member, 404, nil)
	f.call(t, "POST", Prefix+"/grants", `{"resource_type":"template","resource_id":"`+id+`","subject_type":"user","subject_id":"`+uB+`","relation":"viewer"}`, f.admin, 201, nil)
	var got map[string]any
	f.call(t, "GET", Prefix+"/templates/"+id, "", f.member, 200, &got)
	if got["permissions"].(map[string]any)["read"] != true || got["permissions"].(map[string]any)["use"] != false {
		t.Fatalf("viewer: %v", got)
	}
	f.call(t, "PUT", Prefix+"/templates/"+id, `{"name":"welcome","channel_id":"`+cid+`","subject":"s","body":"b"}`, f.member, 403, nil)
	// List with search and channel filter.
	var list map[string]any
	f.call(t, "GET", Prefix+"/templates?q=wel&channel_id="+cid, "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 1 {
		t.Fatalf("list %d", n)
	}
	f.call(t, "GET", Prefix+"/templates?q=zzz", "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 0 {
		t.Fatalf("search %d", n)
	}
	// Update and default.
	f.call(t, "PUT", Prefix+"/templates/"+id, `{"name":"welcome2","channel_id":"`+cid+`","subject":"Hi {{.Name}}","body":"{{.Name}}","variables":["Name"],"is_default":true}`, f.admin, 200, &got)
	if got["name"] != "welcome2" || got["is_default"] != true {
		t.Fatalf("updated: %v", got)
	}
	// Preview: saved template with values, draft override, undeclared variable.
	var pv map[string]any
	f.call(t, "POST", Prefix+"/templates/preview", `{"template_id":"`+id+`","subject":"Hi {{.Name}}","body":"<b>{{.Name}}</b>","variables":["Name"],"values":{"Name":"<Ana>"}}`, f.admin, 200, &pv)
	if pv["rendered_subject"] != "Hi <Ana>" || pv["rendered_body"] != "<b>&lt;Ana&gt;</b>" {
		t.Fatalf("preview: %v", pv)
	}
	f.call(t, "POST", Prefix+"/templates/preview", `{"channel_type":"sms","subject":"s","body":"{{.Name}}","variables":["Name"],"values":{"Name":"<x>"}}`, f.admin, 200, &pv)
	if pv["rendered_body"] != "<x>" {
		t.Fatalf("text preview: %v", pv)
	}
	w = f.do("POST", Prefix+"/templates/preview", `{"subject":"s","body":"{{.Nope}}","variables":["Name"]}`, f.admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"variable":"Nope"`) {
		t.Fatalf("preview undeclared: %d %s", w.Code, w.Body)
	}
	f.call(t, "POST", Prefix+"/templates/preview", `{"template_id":"`+id+`","subject":"s","body":"b"}`, f.memberC, 404, nil)
	// Delete: viewer cannot, owner can.
	f.call(t, "POST", Prefix+"/templates/"+id+"/remove", "", f.member, 403, nil)
	f.call(t, "POST", Prefix+"/templates/"+id+"/remove", "", f.admin, 204, nil)
	f.call(t, "GET", Prefix+"/templates/"+id, "", f.admin, 404, nil)
}

func TestSendAndLog(t *testing.T) {
	f := newFx(t)
	ch := f.channel(t, "relay", true)
	cid := ch["id"].(string)
	tp := f.template(t, "welcome", cid)
	tid := tp["id"].(string)
	// Member has no use on the template → 403 (channel use comes from the default grant).
	f.call(t, "POST", Prefix+"/notifications/send", `{"template_id":"`+tid+`","recipient":"ana@example.org","variables":{"Name":"Ana"}}`, f.member, 403, nil)
	f.call(t, "POST", Prefix+"/grants", `{"resource_type":"template","resource_id":"`+tid+`","subject_type":"user","subject_id":"`+uB+`","relation":"editor"}`, f.admin, 201, nil)
	var entry map[string]any
	f.call(t, "POST", Prefix+"/notifications/send", `{"template_id":"`+tid+`","recipient":"ana@example.org","variables":{"Name":"Ana"}}`, f.member, 200, &entry)
	if entry["status"] != "sent" || entry["rendered_subject"] != "Hello Ana" || entry["sender_id"] != uB || len(f.email.sent) != 1 || f.email.sent[0].To != "ana@example.org" {
		t.Fatalf("sent: %v", entry)
	}
	logID := entry["id"].(string)
	// Missing variable → 422; bad recipient for email → 422; unknown template → 404.
	w := f.do("POST", Prefix+"/notifications/send", `{"template_id":"`+tid+`","recipient":"ana@example.org"}`, f.member)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "validation_failed") {
		t.Fatalf("missing var: %d %s", w.Code, w.Body)
	}
	f.call(t, "POST", Prefix+"/notifications/send", `{"template_id":"`+tid+`","recipient":"not-an-address","variables":{"Name":"A"}}`, f.member, 422, nil)
	f.call(t, "POST", Prefix+"/notifications/send", `{"template_id":"`+uC+`","recipient":"ana@example.org"}`, f.member, 404, nil)
	// Channel override of another type → 422.
	var sms map[string]any
	f.call(t, "POST", Prefix+"/channels", `{"name":"sms","type":"sms","settings":{"api_key":"k"},"enabled":true,"is_default":true}`, f.admin, 201, &sms)
	f.call(t, "POST", Prefix+"/notifications/send", `{"template_id":"`+tid+`","channel_id":"`+sms["id"].(string)+`","recipient":"ana@example.org","variables":{"Name":"A"}}`, f.admin, 422, nil)
	// Provider failure is reported in the entry with a scrubbed error and 200.
	f.email.fail = errors.New("connect relay: NOTIF-MARKER-PW-relay refused")
	f.call(t, "POST", Prefix+"/notifications/send", `{"template_id":"`+tid+`","recipient":"bo@example.org","variables":{"Name":"Bo"}}`, f.admin, 200, &entry)
	if entry["status"] != "failed" || strings.Contains(entry["error"].(string), "NOTIF-MARKER") {
		t.Fatalf("failed: %v", entry)
	}
	f.email.fail = nil
	// Listing: the member sees own entries only; stats:read (admin) sees all; filters work.
	var list map[string]any
	f.call(t, "GET", Prefix+"/notifications", "", f.member, 200, &list)
	if n := len(list["items"].([]any)); n != 1 {
		t.Fatalf("member log %d", n)
	}
	f.call(t, "GET", Prefix+"/notifications", "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 2 {
		t.Fatalf("admin log %d", n)
	}
	if _, ok := list["items"].([]any)[0].(map[string]any)["rendered_body"]; ok {
		t.Fatal("listing carried the body")
	}
	f.call(t, "GET", Prefix+"/notifications?status=failed&recipient=bo&channel_id="+cid+"&template_id="+tid+"&from=2020-01-01T00:00:00Z&to=2030-01-01T00:00:00Z&limit=1", "", f.admin, 200, &list)
	if n := len(list["items"].([]any)); n != 1 {
		t.Fatalf("filtered %d", n)
	}
	f.call(t, "GET", Prefix+"/notifications?cursor=garbage", "", f.admin, 422, nil)
	// Single entry carries the body; other users cannot read it without stats:read.
	var one map[string]any
	f.call(t, "GET", Prefix+"/notifications/"+logID, "", f.member, 200, &one)
	if !strings.Contains(one["rendered_body"].(string), "Hi Ana") {
		t.Fatalf("body: %v", one)
	}
	f.call(t, "GET", Prefix+"/notifications/"+logID, "", f.memberC, 404, nil)
	f.call(t, "GET", Prefix+"/notifications/"+logID, "", f.admin, 200, nil)
	f.call(t, "GET", Prefix+"/notifications/"+logID, "", f.otherTen, 404, nil)
}

func TestGrantsAndAccess(t *testing.T) {
	f := newFx(t)
	ch := f.channel(t, "relay", false)
	cid := ch["id"].(string)
	base := `{"resource_type":"channel","resource_id":"` + cid + `"`
	// Member cannot grant (no share); owner grants editor to the member.
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"user","subject_id":"`+uC+`","relation":"viewer"}`, f.member, 403, nil)
	var g map[string]any
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"user","subject_id":"`+uB+`","relation":"editor"}`, f.admin, 201, &g)
	if g["relation"] != "editor" || g["granted_by"] != uA {
		t.Fatalf("grant: %v", g)
	}
	// Editor cannot share; sharer can, but not above own relation.
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"user","subject_id":"`+uC+`","relation":"viewer"}`, f.member, 403, nil)
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"user","subject_id":"`+uB+`","relation":"sharer"}`, f.admin, 201, nil)
	w := f.do("POST", Prefix+"/grants", base+`,"subject_type":"user","subject_id":"`+uC+`","relation":"owner"}`, f.member)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "relation_above_granter") {
		t.Fatalf("above: %d %s", w.Code, w.Body)
	}
	var g2 map[string]any
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"user","subject_id":"`+uC+`","relation":"viewer","expires_at":"2030-01-01T00:00:00Z"}`, f.member, 201, &g2)
	// Tenant grant needs no subject id; a role grant carries a slug.
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"tenant","relation":"viewer"}`, f.admin, 201, nil)
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"role","subject_id":"auditor","relation":"viewer"}`, f.admin, 201, nil)
	f.call(t, "POST", Prefix+"/grants", base+`,"subject_type":"user","relation":"viewer"}`, f.admin, 422, nil)
	// Listing needs read; other tenants get 404.
	var list map[string]any
	f.call(t, "GET", Prefix+"/grants?resource_type=channel&resource_id="+cid, "", f.member, 200, &list)
	if n := len(list["items"].([]any)); n != 5 {
		t.Fatalf("grants %d", n)
	}
	f.call(t, "GET", Prefix+"/grants?resource_type=channel&resource_id="+cid, "", f.otherTen, 404, nil)
	// Check and effective for the caller and for a subject.
	var chk map[string]any
	f.call(t, "GET", Prefix+"/access/check?resource_type=channel&resource_id="+cid+"&action=share", "", f.member, 200, &chk)
	if chk["allowed"] != true || chk["relation"] != "sharer" {
		t.Fatalf("check: %v", chk)
	}
	f.call(t, "GET", Prefix+"/access/check?resource_type=channel&resource_id="+cid+"&action=delete", "", f.member, 200, &chk)
	if chk["allowed"] != false {
		t.Fatalf("check delete: %v", chk)
	}
	var eff map[string]any
	f.call(t, "GET", Prefix+"/access/effective?resource_type=channel&resource_id="+cid, "", f.memberC, 200, &eff)
	if eff["relation"] != "viewer" || len(eff["grants"].([]any)) != 2 {
		t.Fatalf("effective: %v", eff)
	}
	f.call(t, "GET", Prefix+"/access/effective?resource_type=channel&resource_id="+cid+"&subject_type=user&subject_id="+uC, "", f.member, 200, &eff)
	if eff["relation"] != "viewer" {
		t.Fatalf("effective for: %v", eff)
	}
	f.call(t, "GET", Prefix+"/access/effective?resource_type=channel&resource_id="+cid+"&subject_type=user&subject_id="+uB, "", f.memberC, 403, nil)
	// Revoke: the sharer may revoke their grant; unknown → 404; afterwards C keeps only the tenant grant.
	f.call(t, "POST", Prefix+"/grants/"+g2["id"].(string)+"/revoke", "", f.memberC, 403, nil)
	f.call(t, "POST", Prefix+"/grants/"+g2["id"].(string)+"/revoke", "", f.member, 204, nil)
	f.call(t, "POST", Prefix+"/grants/"+g2["id"].(string)+"/revoke", "", f.member, 404, nil)
	f.call(t, "GET", Prefix+"/access/effective?resource_type=channel&resource_id="+cid, "", f.memberC, 200, &eff)
	if eff["relation"] != "viewer" || len(eff["grants"].([]any)) != 1 {
		t.Fatalf("after revoke: %v", eff)
	}
	if n := f.audit("grant_created"); n != 5 {
		t.Fatalf("audit grants %d", n)
	}
}
