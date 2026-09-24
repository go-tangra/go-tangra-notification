package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBackupExportImport(t *testing.T) {
	f := newFx(t)
	ch := f.channel(t, "relay", true)
	f.template(t, "welcome", ch["id"].(string))
	f.category(t, "ops")
	// Export without credentials: password absent; with credentials: present and audited.
	w := f.do("POST", Prefix+"/backup/export", "", f.admin)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || strings.Contains(w.Body.String(), "NOTIF-MARKER") {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	var doc map[string]any
	f.call(t, "POST", Prefix+"/backup/export", `{"include_credentials":true}`, f.admin, 200, &doc)
	raw, _ := json.Marshal(doc)
	if !strings.Contains(string(raw), "NOTIF-MARKER-PW-relay") || len(doc["channels"].([]any)) != 1 || len(doc["templates"].([]any)) != 1 || len(doc["categories"].([]any)) != 1 {
		t.Fatalf("export creds: %s", raw)
	}
	if n := f.audit("backup_exported_with_credentials"); n != 1 {
		t.Fatalf("audit %d", n)
	}
	// Import into the other tenant (skip), then again (skip: everything skipped), then overwrite.
	var rep map[string]any
	f.call(t, "POST", Prefix+"/backup/import", string(raw), f.otherTen, 200, &rep)
	if rep["channels"].(map[string]any)["created"] != float64(1) || rep["templates"].(map[string]any)["created"] != float64(1) || rep["categories"].(map[string]any)["created"] != float64(1) {
		t.Fatalf("import: %v", rep)
	}
	f.call(t, "POST", Prefix+"/backup/import", string(raw), f.otherTen, 200, &rep)
	if rep["channels"].(map[string]any)["skipped"] != float64(1) {
		t.Fatalf("skip: %v", rep)
	}
	f.call(t, "POST", Prefix+"/backup/import?mode=overwrite", string(raw), f.otherTen, 200, &rep)
	if rep["channels"].(map[string]any)["overwritten"] != float64(1) || rep["templates"].(map[string]any)["overwritten"] != float64(1) {
		t.Fatalf("overwrite: %v", rep)
	}
	var list map[string]any
	f.call(t, "GET", Prefix+"/channels", "", f.otherTen, 200, &list)
	if n := len(list["items"].([]any)); n != 1 {
		t.Fatalf("imported channels %d", n)
	}
	// Invalid documents → 422; oversized → 413; malformed JSON → 400.
	w = f.do("POST", Prefix+"/backup/import", `{"version":9}`, f.otherTen)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "validation_failed") {
		t.Fatalf("invalid: %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", Prefix+"/backup/import", `{"version":1,"x":"`+strings.Repeat("y", 1<<20)+`"}`, f.otherTen); w.Code != 413 {
		t.Fatalf("large: %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", Prefix+"/backup/import", `{"version":`, f.otherTen); w.Code != 422 {
		t.Fatalf("malformed: %d %s", w.Code, w.Body)
	}
}

func TestStatsAuditHealth(t *testing.T) {
	f := newFx(t)
	ch := f.channel(t, "relay", true)
	f.template(t, "welcome", ch["id"].(string))
	var st map[string]any
	f.call(t, "GET", Prefix+"/stats", "", f.admin, 200, &st)
	if st["channels"] != float64(1) || st["templates"] != float64(1) || st["open_streams"] != float64(0) {
		t.Fatalf("stats: %v", st)
	}
	f.aw.Flush()
	var page map[string]any
	f.call(t, "GET", Prefix+"/audit?event_type=channel_created", "", f.admin, 200, &page)
	items := page["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["subject_name"] != "relay" {
		t.Fatalf("audit: %v", page)
	}
	f.call(t, "GET", Prefix+"/audit?actor_id="+uA+"&from=2020-01-01T00:00:00Z&to=2030-01-01T00:00:00Z&limit=1", "", f.admin, 200, &page)
	if len(page["items"].([]any)) != 1 {
		t.Fatalf("audit filtered: %v", page)
	}
	f.call(t, "GET", Prefix+"/audit?cursor=garbage", "", f.admin, 422, nil)
	f.call(t, "GET", Prefix+"/audit", "", f.otherTen, 200, &page)
	if len(page["items"].([]any)) != 0 {
		t.Fatalf("cross-tenant audit: %v", page)
	}
	var h map[string]any
	f.call(t, "GET", Prefix+"/health", "", f.admin, 200, &h)
	if h["status"] != "ok" || h["version"] != "test" || h["database"] != "ok" {
		t.Fatalf("health: %v", h)
	}
	ops := f.ops
	ops.Health = func(context.Context) any { return map[string]any{"database": "unreachable", "valkey": "ok"} }
	f.s.RegisterOps(ops)
	f.call(t, "GET", Prefix+"/health", "", f.admin, 200, &h)
	if h["status"] != "degraded" {
		t.Fatalf("degraded: %v", h)
	}
	// Store failures surface as 503 without detail.
	f.ms.FailOn("TenantStats", errors.New("pg down at 10.0.0.9"))
	if w := f.do("GET", Prefix+"/stats", "", f.admin); w.Code != 503 || strings.Contains(w.Body.String(), "10.0.0.9") {
		t.Fatalf("stats down: %d %s", w.Code, w.Body)
	}
}
