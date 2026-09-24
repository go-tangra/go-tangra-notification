//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
)

// TestAccess covers the relation × action matrix (incl. use) on a template
// and a channel, role grants through an auth group, tenant-wide use on the
// default channel, expiry, granter ceiling, cross-tenant 404 and audit
// (SC-004, SC-009).
func TestAccess(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	// The grants API needs permissions:manage (gateway); a member gets it through a custom role.
	if code, out := owner.JSON(http.MethodPost, "/api/v1/admin/roles", map[string]any{"slug": "sharing", "display_name": "Sharing", "permissions": []string{"permissions:manage"}}); code != 201 {
		t.Fatalf("role → %d %v", code, out)
	}
	viewerU := e.Invite(owner, "viewer@acme.test", "member")
	editorU := e.Invite(owner, "editor@acme.test", "member", "sharing")
	sharerU := e.Invite(owner, "sharer@acme.test", "member", "sharing")
	nobody := e.Invite(owner, "nobody@acme.test", "member", "sharing")
	relay := e.EmailChannel(owner, "relay", true)
	private := e.EmailChannel(owner, "private", false)
	tpl := e.Template(owner, "welcome", relay)
	for _, g := range []struct {
		u   *Session
		rel string
	}{{viewerU, "viewer"}, {editorU, "editor"}, {sharerU, "sharer"}} {
		if code, out := e.Grant(owner, "template", tpl, "user", g.u.UserID, g.rel, nil); code != 201 {
			t.Fatalf("grant %s → %d %v", g.rel, code, out)
		}
	}
	check := func(s *Session, action string) bool {
		code, out := s.JSON(http.MethodGet, api+"/access/check?resource_type=template&resource_id="+tpl+"&action="+action, nil)
		if code != 200 {
			t.Fatalf("check %s → %d %v", action, code, out)
		}
		return out["allowed"] == true
	}
	matrix := map[*Session]map[string]bool{
		viewerU: {"read": true, "write": false, "delete": false, "share": false, "use": false},
		editorU: {"read": true, "write": true, "delete": false, "share": false, "use": true},
		sharerU: {"read": true, "write": false, "delete": false, "share": true, "use": true},
		nobody:  {"read": false, "write": false, "delete": false, "share": false, "use": false},
		owner:   {"read": true, "write": true, "delete": true, "share": true, "use": true},
	}
	for s, row := range matrix {
		for action, want := range row {
			if got := check(s, action); got != want {
				t.Errorf("%s %s: %v", s.Email, action, got)
			}
		}
	}
	// The relations act on the routes: viewer cannot send, editor can (use), sharer can grant but not above sharer.
	send := map[string]any{"template_id": tpl, "recipient": "x@acme.test", "variables": map[string]string{"Name": "X"}}
	if code, _ := viewerU.JSON(http.MethodPost, api+"/notifications/send", send); code != 403 {
		t.Fatal("viewer sends")
	}
	if code, out := editorU.JSON(http.MethodPost, api+"/notifications/send", send); code != 200 {
		t.Fatalf("editor send → %d %v", code, out)
	}
	code, out := e.Grant(sharerU, "template", tpl, "user", nobody.UserID, "owner", nil)
	detail, _ := out["detail"].(map[string]any)
	if code != 403 || detail["reason"] != "relation_above_granter" {
		t.Fatalf("above granter → %d %v", code, out)
	}
	if code, _ := e.Grant(editorU, "template", tpl, "user", nobody.UserID, "viewer", nil); code != 403 {
		t.Fatal("editor grants")
	}
	code, g := e.Grant(sharerU, "template", tpl, "user", nobody.UserID, "viewer", nil)
	if code != 201 {
		t.Fatalf("sharer grant → %d %v", code, g)
	}
	if !check(nobody, "read") {
		t.Fatal("granted viewer cannot read")
	}
	// Effective permissions with sources; for a subject only with share.
	code, eff := nobody.JSON(http.MethodGet, api+"/access/effective?resource_type=template&resource_id="+tpl, nil)
	if code != 200 || eff["relation"] != "viewer" || len(eff["grants"].([]any)) != 1 {
		t.Fatalf("effective %d %v", code, eff)
	}
	if code, _ := nobody.JSON(http.MethodGet, api+"/access/effective?resource_type=template&resource_id="+tpl+"&subject_type=user&subject_id="+viewerU.UserID, nil); code != 403 {
		t.Fatal("effective-for without share")
	}
	if code, eff := sharerU.JSON(http.MethodGet, api+"/access/effective?resource_type=template&resource_id="+tpl+"&subject_type=user&subject_id="+editorU.UserID, nil); code != 200 || eff["relation"] != "editor" {
		t.Fatalf("effective-for %d %v", code, eff)
	}
	// Revocation is enforced on the next request.
	if code, _ := sharerU.JSON(http.MethodPost, api+"/grants/"+g["id"].(string)+"/revoke", nil); code != 204 {
		t.Fatal("revoke")
	}
	if check(nobody, "read") {
		t.Fatal("revocation not enforced")
	}
	// Tenant-wide use: every member may use the default channel but not the private one.
	code, chk := nobody.JSON(http.MethodGet, api+"/access/check?resource_type=channel&resource_id="+relay+"&action=use", nil)
	if code != 200 || chk["allowed"] != true {
		t.Fatalf("tenant use %d %v", code, chk)
	}
	if code, chk := nobody.JSON(http.MethodGet, api+"/access/check?resource_type=channel&resource_id="+private+"&action=read", nil); code != 200 || chk["allowed"] != false {
		t.Fatalf("private check %d %v", code, chk)
	}
	if code, _ := nobody.JSON(http.MethodGet, api+"/channels/"+private, nil); code != 404 {
		t.Fatal("private channel visible")
	}
	// A role grant held through an auth group: ops group → role "ops"; "grouped" joins the group.
	code, role := owner.JSON(http.MethodPost, "/api/v1/admin/roles", map[string]any{"slug": "ops", "display_name": "Ops", "permissions": []string{"templates:read", "notifications:send"}})
	if code != 201 {
		t.Fatalf("role → %d %v", code, role)
	}
	code, group := owner.JSON(http.MethodPost, "/api/v1/admin/groups", map[string]any{"name": "Ops team", "description": ""})
	if code != 201 {
		t.Fatalf("group → %d %v", code, group)
	}
	if code, out := owner.JSON(http.MethodPut, "/api/v1/admin/groups/"+group["id"].(string)+"/roles", map[string]any{"role_ids": []string{role["id"].(string)}}); code != 200 {
		t.Fatalf("group roles → %d %v", code, out)
	}
	grouped := e.Invite(owner, "grouped@acme.test", "member")
	if code, out := owner.JSON(http.MethodPost, "/api/v1/admin/groups/"+group["id"].(string)+"/members", map[string]any{"user_ids": []string{grouped.UserID}}); code/100 != 2 {
		t.Fatalf("membership → %d %v", code, out)
	}
	exp := time.Now().Add(4 * time.Second)
	if code, out := e.Grant(owner, "template", tpl, "role", "ops", "sharer", &exp); code != 201 {
		t.Fatalf("role grant → %d %v", code, out)
	}
	if code, _ := grouped.SignIn(); code != 200 { // the token must carry the new effective role
		t.Fatal("re-sign-in")
	}
	grouped.WaitAuthorized(api + "/templates/" + tpl)
	if code, out := grouped.JSON(http.MethodPost, api+"/notifications/send", send); code != 200 {
		t.Fatalf("send through the group role → %d %v", code, out)
	}
	time.Sleep(time.Until(exp) + 500*time.Millisecond)
	if code, _ := grouped.JSON(http.MethodGet, api+"/templates/"+tpl, nil); code != 404 {
		t.Fatal("expired grant honoured")
	}
	// Cross-tenant: another tenant's owner sees nothing (404), never 403.
	beta, _ := e.CreateTenant("beta", "owner@beta.test")
	if code, out := beta.JSON(http.MethodGet, api+"/templates/"+tpl, nil); code != 404 || out["reason"] != "not_found" {
		t.Fatalf("cross-tenant → %d %v", code, out)
	}
	if code, _ := e.Grant(beta, "template", tpl, "tenant", "", "viewer", nil); code != 404 {
		t.Fatal("cross-tenant grant")
	}
	if code, _ := beta.JSON(http.MethodGet, api+"/grants?resource_type=template&resource_id="+tpl, nil); code != 404 {
		t.Fatal("cross-tenant grant listing")
	}
	if e.AuditCount(tid, "grant_created", "ok") < 5 || e.AuditCount(tid, "grant_revoked", "ok") < 1 || e.AuditCount(tid, "access_refused", "refused") < 5 {
		t.Fatalf("audit: created %d revoked %d refused %d", e.AuditCount(tid, "grant_created", "ok"), e.AuditCount(tid, "grant_revoked", "ok"), e.AuditCount(tid, "access_refused", "refused"))
	}
}
