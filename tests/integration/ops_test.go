//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestBackup covers export (credentials omitted by default, included with the
// flag and audited), import skip/overwrite reports, warnings and refusals (SC-008).
func TestBackup(t *testing.T) {
	e := StartPlatform(t)
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	relay := e.EmailChannel(owner, "relay", true)
	e.SecretChannel(owner, "sms")
	for i := 0; i < 100; i++ {
		e.Template(owner, fmt.Sprintf("tpl-%03d", i), relay)
	}
	e.category(owner, "Ops", 1)
	resp := owner.Raw(http.MethodPost, api+"/backup/export", nil, "")
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if i := strings.Index(string(raw), "NOTIF-MARKER-PW"); resp.StatusCode != 200 || i >= 0 {
		ctx := ""
		if i >= 0 {
			ctx = string(raw[max(i-80, 0):min(i+40, len(raw))])
		}
		t.Fatalf("export %d credential-marker@%d …%s…", resp.StatusCode, i, ctx)
	}
	start := time.Now()
	resp = owner.Raw(http.MethodPost, api+"/backup/export", []byte(`{"include_credentials":true}`), "application/json")
	full, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(full), "NOTIF-MARKER-PW-sms") {
		t.Fatalf("export with credentials %d", resp.StatusCode)
	}
	if e.AuditCount(tid, "backup_exported_with_credentials", "ok") != 1 || e.AuditCount(tid, "backup_exported", "ok") != 1 {
		t.Fatal("export audit")
	}
	// Round trip into another tenant: skip, then overwrite; 100 templates in < 10 s.
	beta, btid := e.CreateTenant("beta", "owner@beta.test")
	code, rep := beta.JSON(http.MethodPost, api+"/backup/import", json.RawMessage(full))
	if code != 200 || rep["templates"].(map[string]any)["created"] != float64(100) || rep["channels"].(map[string]any)["created"] != float64(2) || rep["categories"].(map[string]any)["created"] != float64(1) {
		t.Fatalf("import %d %v", code, rep)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("round trip took %s", d)
	}
	code, rep = beta.JSON(http.MethodPost, api+"/backup/import", json.RawMessage(full))
	if code != 200 || rep["templates"].(map[string]any)["skipped"] != float64(100) {
		t.Fatalf("skip %d %v", code, rep)
	}
	code, rep = beta.JSON(http.MethodPost, api+"/backup/import?mode=overwrite", json.RawMessage(full))
	if code != 200 || rep["templates"].(map[string]any)["overwritten"] != float64(100) || rep["channels"].(map[string]any)["overwritten"] != float64(2) {
		t.Fatalf("overwrite %d %v", code, rep)
	}
	// The imported channel works (credentials travelled): a test send lands in Mailpit.
	code, list := beta.JSON(http.MethodGet, api+"/channels?type=email", nil)
	if code != 200 || len(Items(list)) != 1 {
		t.Fatalf("beta channels %d %v", code, list)
	}
	if code, out := beta.JSON(http.MethodPost, api+"/channels/"+Items(list)[0]["id"].(string)+"/test", map[string]any{"recipient": "beta@beta.test"}); code != 200 || out["status"] != "sent" {
		t.Fatalf("imported channel test %d %v", code, out)
	}
	if code, list := beta.JSON(http.MethodGet, api+"/channels?type=sms", nil); code != 200 || len(Items(list)) != 1 || Items(list)[0]["settings"].(map[string]any)["api_key"] != "__set__" {
		t.Fatalf("imported sms channel %d %v", code, list)
	}
	// Unknown channel name → warning; malformed and oversized files refused.
	code, rep = beta.JSON(http.MethodPost, api+"/backup/import", map[string]any{"version": 1, "exported_at": "2024-01-01T00:00:00Z", "tenant": "t",
		"channels": []map[string]any{}, "categories": []map[string]any{}, "templates": []map[string]any{{"name": "orphan", "channel": "nope", "subject": "s", "body": "b"}}})
	if code != 200 || rep["templates"].(map[string]any)["failed"] != float64(1) || len(rep["warnings"].([]any)) != 1 {
		t.Fatalf("orphan %d %v", code, rep)
	}
	if code, out := beta.JSON(http.MethodPost, api+"/backup/import", map[string]any{"version": 9}); code != 422 || out["reason"] != "validation_failed" {
		t.Fatalf("invalid %d %v", code, out)
	}
	huge := []byte(`{"version":1,"exported_at":"2024-01-01T00:00:00Z","tenant":"t","channels":[],"templates":[],"categories":[],"pad":"` + strings.Repeat("x", 17<<20) + `"}`)
	resp = beta.Raw(http.MethodPost, api+"/backup/import", huge, "application/json")
	_ = resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("oversized → %d", resp.StatusCode)
	}
	if e.AuditCount(btid, "backup_imported", "ok") != 4 {
		t.Fatalf("import audit %d", e.AuditCount(btid, "backup_imported", "ok"))
	}
	e.ScanLogsFor(t, []string{"NOTIF-MARKER-PW-sms"})
}

// TestOps covers stats, the audit listing with resolved names and health
// with Valkey paused (SC-008).
func TestOps(t *testing.T) {
	e := StartPlatform(t)
	owner, _ := e.CreateTenant("acme", "owner@acme.test")
	member := e.Invite(owner, "bob@acme.test", "member")
	relay := e.EmailChannel(owner, "relay", true)
	e.Template(owner, "welcome", relay)
	st, _ := member.Stream("")
	defer st.Close()
	time.Sleep(300 * time.Millisecond)
	e.Notif.Audit.Flush()
	code, stats := owner.JSON(http.MethodGet, api+"/stats", nil)
	if code != 200 || stats["channels"] != float64(1) || stats["templates"] != float64(1) || stats["open_streams"] != float64(1) || stats["operations_24h"].(float64) < 2 {
		t.Fatalf("stats %d %v", code, stats)
	}
	if code, _ := member.JSON(http.MethodGet, api+"/stats", nil); code != 403 {
		t.Fatal("member reads stats")
	}
	code, page := owner.JSON(http.MethodGet, api+"/audit?event_type=channel_created", nil)
	if code != 200 || len(Items(page)) != 1 || Items(page)[0]["subject_name"] != "relay" || Items(page)[0]["actor_id"] != owner.UserID {
		t.Fatalf("audit %d %v", code, page)
	}
	code, page = owner.JSON(http.MethodGet, api+"/audit?actor_id="+member.UserID, nil)
	if code != 200 {
		t.Fatalf("audit by actor %d", code)
	}
	for _, it := range Items(page) {
		if it["actor_id"] != member.UserID {
			t.Fatalf("filter leaked %v", it)
		}
	}
	// Valkey paused → the module reports degraded (checked directly: while Valkey
	// is down the gateway cannot reach its decision path to authorize a request).
	e.ValkeyStop()
	hh := e.Notif.Health(context.Background())
	e.ValkeyStart()
	if hh.Valkey == "ok" || hh.DB != "ok" {
		t.Fatalf("degraded health %+v", hh)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if e.Notif.Health(context.Background()).Valkey == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("health did not recover")
		}
		time.Sleep(500 * time.Millisecond)
	}
	// Through the gateway the recovered health route reports ok (poll: the module
	// re-registers with the gateway after the outage).
	deadline = time.Now().Add(30 * time.Second)
	for {
		code, h := owner.JSON(http.MethodGet, api+"/health", nil)
		if code == 200 && h["status"] == "ok" && h["valkey"] == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health through the gateway did not recover: %d %v", code, h)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
