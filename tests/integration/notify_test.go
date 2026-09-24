//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestChannels covers the channel lifecycle through the gateway: settings
// redacted, test send lands in Mailpit, default flag moves, delete refused
// while templates reference the channel, credentials never leak (SC-001,
// SC-002, SC-003).
func TestChannels(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	member := e.Invite(owner, "bob@acme.test", "member")
	relay := e.EmailChannel(owner, "relay", true)
	code, ch := owner.JSON(http.MethodGet, api+"/channels/"+relay, nil)
	settings := ch["settings"].(map[string]any)
	if code != 200 || settings["host"] != e.MailHost || ch["permissions"].(map[string]any)["delete"] != true {
		t.Fatalf("channel %d %v", code, ch)
	}
	// Credentials are write-only: stored sealed, echoed as the marker, kept on update.
	sms := e.SecretChannel(owner, "sms")
	code, sv := owner.JSON(http.MethodGet, api+"/channels/"+sms, nil)
	if code != 200 || sv["settings"].(map[string]any)["api_key"] != "__set__" || sv["settings"].(map[string]any)["account"] != "acme" {
		t.Fatalf("sms channel %d %v", code, sv)
	}
	if code, sv := owner.JSON(http.MethodPut, api+"/channels/"+sms, map[string]any{"name": "sms", "type": "sms", "enabled": true, "settings": map[string]any{"account": "acme2", "api_key": "__set__"}}); code != 200 || sv["settings"].(map[string]any)["api_key"] != "__set__" || sv["settings"].(map[string]any)["account"] != "acme2" {
		t.Fatalf("sms update %d %v", code, sv)
	}
	if code, out := owner.JSON(http.MethodPost, api+"/channels/"+sms+"/test", map[string]any{"recipient": "+15550001"}); code != 422 || out["reason"] != "no_provider" {
		t.Fatalf("sms test %d %v", code, out)
	}
	// Test send → Mailpit.
	code, entry := owner.JSON(http.MethodPost, api+"/channels/"+relay+"/test", map[string]any{"recipient": "ops@acme.test"})
	if code != 200 || entry["status"] != "sent" || entry["test"] != true {
		t.Fatalf("test send %d %v", code, entry)
	}
	if mail := e.LastMail("ops@acme.test"); !strings.Contains(mail, "test message") {
		t.Fatalf("mail %q", mail)
	}
	// The member sees the default channel (tenant sharer) with use but not write; the second, non-default channel is invisible.
	second := e.EmailChannel(owner, "second", false)
	code, view := member.JSON(http.MethodGet, api+"/channels/"+relay, nil)
	if code != 200 || view["permissions"].(map[string]any)["use"] != true || view["permissions"].(map[string]any)["write"] != false {
		t.Fatalf("member view %d %v", code, view)
	}
	if code, _ := member.JSON(http.MethodGet, api+"/channels/"+second, nil); code != 404 {
		t.Fatalf("member sees the second channel: %d", code)
	}
	if code, _ := member.JSON(http.MethodPost, api+"/channels/"+relay+"/test", map[string]any{"recipient": "x@acme.test"}); code != 403 {
		t.Fatalf("member test send: %d", code)
	}
	// Default moves to the second channel; the tenant use grant follows.
	code, upd := owner.JSON(http.MethodPut, api+"/channels/"+second, map[string]any{"name": "second", "type": "email", "enabled": true, "is_default": true,
		"settings": map[string]any{"host": e.MailHost, "port": e.MailPort, "tls": "none", "from": "noreply@example.org"}})
	if code != 200 || upd["is_default"] != true {
		t.Fatalf("move default %d %v", code, upd)
	}
	code, old := owner.JSON(http.MethodGet, api+"/channels/"+relay, nil)
	if code != 200 || old["is_default"] != false {
		t.Fatalf("old default %v", old)
	}
	if code, _ := member.JSON(http.MethodGet, api+"/channels/"+second, nil); code != 200 {
		t.Fatalf("member lost the default: %d", code)
	}
	// Delete refused while a template references the channel.
	tpl := e.Template(owner, "welcome", relay)
	if code, out := owner.JSON(http.MethodPost, api+"/channels/"+relay+"/remove", nil); code != 409 || out["reason"] != "conflict" {
		t.Fatalf("in use %d %v", code, out)
	}
	if code, _ := owner.JSON(http.MethodPost, api+"/templates/"+tpl+"/remove", nil); code != 204 {
		t.Fatal("template remove")
	}
	if code, _ := owner.JSON(http.MethodPost, api+"/channels/"+relay+"/remove", nil); code != 204 {
		t.Fatal("channel remove")
	}
	if code, _ := owner.JSON(http.MethodGet, api+"/channels/"+relay, nil); code != 404 {
		t.Fatal("removed channel readable")
	}
	// Invalid settings for the type are refused with detail.
	if code, out := owner.JSON(http.MethodPost, api+"/channels", map[string]any{"name": "bad", "type": "email", "settings": map[string]any{"port": 25, "from": "a@b.c"}}); code != 422 || out["reason"] != "validation_failed" {
		t.Fatalf("bad settings %d %v", code, out)
	}
	// A test send is a notification with test=true (recorded as notification_sent).
	if e.AuditCount(tid, "channel_created", "ok") != 3 || e.AuditCount(tid, "notification_sent", "ok") < 1 || e.AuditCount(tid, "channel_deleted", "ok") != 1 {
		t.Fatalf("audit created=%d sent=%d deleted=%d", e.AuditCount(tid, "channel_created", "ok"), e.AuditCount(tid, "notification_sent", "ok"), e.AuditCount(tid, "channel_deleted", "ok"))
	}
	e.ScanForMaterial(t, []string{"NOTIF-MARKER-PW-sms"})
}

// TestTemplates covers validation (syntax position, undeclared variable),
// preview without a log entry, default per channel and grants on templates.
func TestTemplates(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	member := e.Invite(owner, "bob@acme.test", "member")
	relay := e.EmailChannel(owner, "relay", true)
	code, out := owner.JSON(http.MethodPost, api+"/templates", map[string]any{"name": "bad", "channel_id": relay, "subject": "{{.Name", "body": "x"})
	if code != 422 || out["detail"].(map[string]any)["position"] == nil {
		t.Fatalf("syntax %d %v", code, out)
	}
	code, out = owner.JSON(http.MethodPost, api+"/templates", map[string]any{"name": "bad", "channel_id": relay, "subject": "{{.Other}}", "body": "x", "variables": []string{"Name"}})
	if code != 422 || out["detail"].(map[string]any)["variable"] != "Other" {
		t.Fatalf("undeclared %d %v", code, out)
	}
	tpl := e.Template(owner, "welcome", relay)
	code, pv := owner.JSON(http.MethodPost, api+"/templates/preview", map[string]any{"template_id": tpl, "subject": "Hello {{.Name}}", "body": "<b>{{.Name}}</b>", "variables": []string{"Name"}, "values": map[string]string{"Name": "<Ana>"}})
	if code != 200 || pv["rendered_subject"] != "Hello <Ana>" || pv["rendered_body"] != "<b>&lt;Ana&gt;</b>" {
		t.Fatalf("preview %d %v", code, pv)
	}
	if code, log := owner.JSON(http.MethodGet, api+"/notifications", nil); code != 200 || len(Items(log)) != 0 {
		t.Fatalf("preview logged: %d %v", code, log)
	}
	// Member: invisible until a viewer grant; then read but no write; the owner may make it default.
	if code, _ := member.JSON(http.MethodGet, api+"/templates/"+tpl, nil); code != 404 {
		t.Fatal("member sees an ungranted template")
	}
	if code, out := e.Grant(owner, "template", tpl, "user", member.UserID, "viewer", nil); code != 201 {
		t.Fatalf("grant %d %v", code, out)
	}
	code, view := member.JSON(http.MethodGet, api+"/templates/"+tpl, nil)
	if code != 200 || view["permissions"].(map[string]any)["read"] != true || view["permissions"].(map[string]any)["use"] != false {
		t.Fatalf("viewer %d %v", code, view)
	}
	if code, _ := member.JSON(http.MethodPut, api+"/templates/"+tpl, map[string]any{"name": "welcome", "channel_id": relay, "subject": "s", "body": "b"}); code != 403 {
		t.Fatal("viewer writes")
	}
	code, upd := owner.JSON(http.MethodPut, api+"/templates/"+tpl, map[string]any{"name": "welcome", "channel_id": relay, "subject": "Hello {{.Name}}", "body": "{{.Name}}", "variables": []string{"Name"}, "is_default": true})
	if code != 200 || upd["is_default"] != true {
		t.Fatalf("default %d %v", code, upd)
	}
	code, list := member.JSON(http.MethodGet, api+"/templates?q=wel", nil)
	if code != 200 || len(Items(list)) != 1 {
		t.Fatalf("list %d %v", code, list)
	}
	if e.AuditCount(tid, "template_created", "ok") != 1 || e.AuditCount(tid, "template_previewed", "ok") < 1 {
		t.Fatal("audit")
	}
}

// TestSend covers the send pipeline through the gateway: render + deliver +
// log, override channel, refusals, provider outage → failed with a scrubbed
// reason, log visibility (SC-001, SC-002, SC-003).
func TestSend(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	member := e.Invite(owner, "bob@acme.test", "member")
	relay := e.EmailChannel(owner, "relay", true)
	tpl := e.Template(owner, "welcome", relay)
	send := func(s *Session, body map[string]any) (int, map[string]any) {
		return s.JSON(http.MethodPost, api+"/notifications/send", body)
	}
	// Member lacks use on the template → 403; sharer grant → sends.
	if code, _ := send(member, map[string]any{"template_id": tpl, "recipient": "ana@acme.test", "variables": map[string]string{"Name": "Ana"}}); code != 403 {
		t.Fatalf("no use → %d", code)
	}
	if code, out := e.Grant(owner, "template", tpl, "user", member.UserID, "sharer", nil); code != 201 {
		t.Fatalf("grant %d %v", code, out)
	}
	code, entry := send(member, map[string]any{"template_id": tpl, "recipient": "ana@acme.test", "variables": map[string]string{"Name": "Ana"}})
	if code != 200 || entry["status"] != "sent" || entry["rendered_subject"] != "Hello Ana" || entry["sender_id"] != member.UserID {
		t.Fatalf("send %d %v", code, entry)
	}
	if mail := e.LastMail("ana@acme.test"); !strings.Contains(mail, "Hi Ana") {
		t.Fatalf("mail %q", mail)
	}
	// Refusals.
	for name, tc := range map[string]struct {
		body map[string]any
		code int
	}{
		"missing variable": {map[string]any{"template_id": tpl, "recipient": "ana@acme.test"}, 422},
		"bad recipient":    {map[string]any{"template_id": tpl, "recipient": "not-an-address", "variables": map[string]string{"Name": "x"}}, 422},
		"crlf recipient":   {map[string]any{"template_id": tpl, "recipient": "a@b.c\r\nBcc: x@y", "variables": map[string]string{"Name": "x"}}, 422},
		"unknown template": {map[string]any{"template_id": "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c99", "recipient": "a@b.c"}, 404},
	} {
		if code, out := send(owner, tc.body); code != tc.code {
			t.Errorf("%s → %d %v", name, code, out)
		}
	}
	// Override with a second channel of the same type is honoured; a disabled one is refused.
	second := e.EmailChannel(owner, "second", false)
	code, entry = send(owner, map[string]any{"template_id": tpl, "channel_id": second, "recipient": "bo@acme.test", "variables": map[string]string{"Name": "Bo"}})
	if code != 200 || entry["channel_id"] != second || entry["status"] != "sent" {
		t.Fatalf("override %d %v", code, entry)
	}
	if code, _ := owner.JSON(http.MethodPut, api+"/channels/"+second, map[string]any{"name": "second", "type": "email", "enabled": false,
		"settings": map[string]any{"host": e.MailHost, "port": e.MailPort, "tls": "none", "from": "noreply@example.org"}}); code != 200 {
		t.Fatal("disable")
	}
	if code, out := send(owner, map[string]any{"template_id": tpl, "channel_id": second, "recipient": "bo@acme.test", "variables": map[string]string{"Name": "Bo"}}); code != 422 || out["reason"] != "channel_disabled" {
		t.Fatalf("disabled %d %v", code, out)
	}
	// Mailpit paused → failed with a scrubbed reason (200: the outcome is in the entry).
	e.MailStop()
	code, entry = send(owner, map[string]any{"template_id": tpl, "recipient": "cy@acme.test", "variables": map[string]string{"Name": "Cy"}})
	e.MailStart()
	if code != 200 || entry["status"] != "failed" || entry["error"] == "" || strings.Contains(entry["error"].(string), "NOTIF-MARKER") {
		t.Fatalf("outage %d %v", code, entry)
	}
	failedID := entry["id"].(string)
	// Log: member sees own entries only; owner (stats:read) sees all; the single view carries the body.
	code, list := member.JSON(http.MethodGet, api+"/notifications", nil)
	if code != 200 || len(Items(list)) != 1 {
		t.Fatalf("member log %d %v", code, list)
	}
	code, list = owner.JSON(http.MethodGet, api+"/notifications?status=failed", nil)
	if code != 200 || len(Items(list)) != 1 || Items(list)[0]["id"] != failedID {
		t.Fatalf("owner log %d %v", code, list)
	}
	code, one := owner.JSON(http.MethodGet, api+"/notifications/"+failedID, nil)
	if code != 200 || !strings.Contains(one["rendered_body"].(string), "Hi Cy") {
		t.Fatalf("entry %d %v", code, one)
	}
	if code, _ := member.JSON(http.MethodGet, api+"/notifications/"+failedID, nil); code != 404 {
		t.Fatal("member reads a foreign entry")
	}
	if e.AuditCount(tid, "notification_sent", "ok") != 2 || e.AuditCount(tid, "notification_failed", "failed") != 1 {
		t.Fatalf("audit sent=%d failed=%d", e.AuditCount(tid, "notification_sent", "ok"), e.AuditCount(tid, "notification_failed", "failed"))
	}
	e.SecretChannel(owner, "sms")
	e.ScanForMaterial(t, []string{"NOTIF-MARKER-PW-sms"})
	e.ScanLogsFor(t, []string{"NOTIF-MARKER-BODY-welcome"})
	_ = time.Second
}
