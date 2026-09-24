//go:build integration

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// TestEveryMutationAudited drives one call of every mutating route and
// proves each leaves an audit event, none carrying credentials or content
// (constitution principle: every operation audited).
func TestEveryMutationAudited(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	if code, out := owner.JSON(http.MethodPost, "/api/v1/admin/roles", map[string]any{"slug": "tpl-mgr", "display_name": "TplMgr", "permissions": []string{"templates:manage", "backup:manage"}}); code != 201 {
		t.Fatalf("role → %d %v", code, out)
	}
	alice := e.Invite(owner, "alice@acme.test", "member", "tpl-mgr")
	count := func() int { return e.AuditCount(tid, "", "") }
	step := func(name string, fn func()) {
		t.Helper()
		before := count()
		fn()
		if after := count(); after <= before {
			t.Errorf("%s left no audit event", name)
		}
	}
	var relay, tpl, cat, msg, grant string
	step("create channel", func() { relay = e.EmailChannel(owner, "relay", true) })
	step("update channel", func() {
		owner.JSON(http.MethodPut, api+"/channels/"+relay, map[string]any{"name": "relay2", "type": "email", "enabled": true, "is_default": true, "settings": map[string]any{"host": e.MailHost, "port": e.MailPort, "tls": "none", "from": "n@example.org"}})
	})
	step("test channel", func() {
		owner.JSON(http.MethodPost, api+"/channels/"+relay+"/test", map[string]any{"recipient": "t@acme.test"})
	})
	step("create template", func() { tpl = e.Template(owner, "welcome", relay) })
	step("update template", func() {
		owner.JSON(http.MethodPut, api+"/templates/"+tpl, map[string]any{"name": "welcome", "channel_id": relay, "subject": "s {{.Name}}", "body": "NOTIF-MARKER-BODY-x {{.Name}}", "variables": []string{"Name"}})
	})
	step("preview", func() {
		owner.JSON(http.MethodPost, api+"/templates/preview", map[string]any{"template_id": tpl, "subject": "s", "body": "b"})
	})
	step("send", func() {
		owner.JSON(http.MethodPost, api+"/notifications/send", map[string]any{"template_id": tpl, "recipient": "a@acme.test", "variables": map[string]string{"Name": "A"}})
	})
	step("grant", func() {
		_, g := e.Grant(owner, "template", tpl, "user", alice.UserID, "viewer", nil)
		grant, _ = g["id"].(string)
	})
	step("refused access", func() {
		alice.JSON(http.MethodPut, api+"/templates/"+tpl, map[string]any{"name": "x", "channel_id": relay, "subject": "s", "body": "b"})
	})
	step("revoke", func() { owner.JSON(http.MethodPost, api+"/grants/"+grant+"/revoke", nil) })
	step("create category", func() { cat = e.category(owner, "Ops", 1) })
	step("update category", func() { owner.JSON(http.MethodPut, api+"/categories/"+cat, map[string]any{"name": "Ops2"}) })
	step("create message", func() {
		msg = e.draft(owner, "M", map[string]any{"users": []string{alice.UserID}}, map[string]any{"category_id": cat})
	})
	step("update message", func() {
		owner.JSON(http.MethodPut, api+"/messages/"+msg, map[string]any{"title": "M2", "content": "NOTIF-MARKER-BODY-m", "recipients": map[string]any{"users": []string{alice.UserID}}})
	})
	step("send message", func() { owner.JSON(http.MethodPost, api+"/messages/"+msg+"/send", nil) })
	var entry string
	step("read inbox entry", func() {
		_, page := alice.JSON(http.MethodGet, api+"/inbox", nil)
		entry = Items(page)[0]["id"].(string)
		alice.JSON(http.MethodGet, api+"/inbox/"+entry, nil)
	})
	step("mark inbox", func() {
		alice.JSON(http.MethodPost, api+"/inbox/status", map[string]any{"ids": []string{entry}, "status": "unread"})
		alice.JSON(http.MethodPost, api+"/inbox/status", map[string]any{"ids": []string{entry}, "status": "read"})
	})
	step("remove from inbox", func() { alice.JSON(http.MethodPost, api+"/inbox/remove", map[string]any{"ids": []string{entry}}) })
	step("revoke message", func() { owner.JSON(http.MethodPost, api+"/messages/"+msg+"/revoke", nil) })
	step("archive message", func() { owner.JSON(http.MethodPost, api+"/messages/"+msg+"/archive", nil) })
	step("remove message", func() { owner.JSON(http.MethodPost, api+"/messages/"+msg+"/remove", nil) })
	step("remove category", func() { owner.JSON(http.MethodPost, api+"/categories/"+cat+"/remove", nil) })
	step("open stream", func() {
		s, _ := alice.Stream("")
		s.Close()
	})
	step("export", func() { owner.JSON(http.MethodPost, api+"/backup/export", map[string]any{"include_credentials": true}) })
	step("import", func() {
		owner.JSON(http.MethodPost, api+"/backup/import", map[string]any{"version": 1, "exported_at": "2024-01-01T00:00:00Z", "tenant": "t",
			"channels": []map[string]any{}, "templates": []map[string]any{}, "categories": []map[string]any{{"name": "Imported"}}})
	})
	step("remove template", func() { owner.JSON(http.MethodPost, api+"/templates/"+tpl+"/remove", nil) })
	step("remove channel", func() { owner.JSON(http.MethodPost, api+"/channels/"+relay+"/remove", nil) })
	// No audit detail carries a credential, a rendered body or message content.
	e.Notif.Audit.Flush()
	var dump strings.Builder
	_ = e.Notif.Store.Tx(context.Background(), store.Scope{System: true}, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), "SELECT details::text FROM notification_audit_events WHERE tenant_id = $1", tid)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			_ = rows.Scan(&d)
			dump.WriteString(d + "\n")
		}
		return nil
	})
	for _, m := range []string{"NOTIF-MARKER", "\"password\"", "\"body\"", "\"content\""} {
		if strings.Contains(dump.String(), m) {
			t.Fatalf("audit details carry %q", m)
		}
	}
}
