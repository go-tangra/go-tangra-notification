package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
)

// TestManagedChannel: the configuration-managed channel is readable (with
// managed=true and write/delete false), test sends work, and update and
// removal answer 409 managed_channel (T014).
func TestManagedChannel(t *testing.T) {
	f := newFx(t)
	pe := &notify.PlatformEmail{Host: "relay", Port: 587, Username: "u", Password: "NOTIF-MARKER-PW-platform", From: "noreply@example.org"}
	if _, err := f.ch.EnsurePlatformChannel(context.Background(), tA, pe); err != nil {
		t.Fatal(err)
	}
	var list map[string]any
	f.call(t, "GET", Prefix+"/channels", "", f.admin, 200, &list)
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list %v", list)
	}
	ch := items[0].(map[string]any)
	id := ch["id"].(string)
	perms := ch["permissions"].(map[string]any)
	if ch["managed"] != true || ch["name"] != "Platform email" || perms["write"] != false || perms["delete"] != false || perms["read"] != true ||
		ch["settings"].(map[string]any)["password"] != "__set__" {
		t.Fatalf("managed view %v", ch)
	}
	w := f.do("PUT", Prefix+"/channels/"+id, `{"name":"mine","type":"email","settings":{"host":"relay","port":587,"from":"noreply@example.org"},"enabled":true}`, f.admin)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"managed_channel"`) {
		t.Fatalf("update %d %s", w.Code, w.Body)
	}
	w = f.do("POST", Prefix+"/channels/"+id+"/remove", "", f.admin)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"managed_channel"`) {
		t.Fatalf("remove %d %s", w.Code, w.Body)
	}
	var entry map[string]any
	f.call(t, "POST", Prefix+"/channels/"+id+"/test", `{"recipient":"ops@example.org"}`, f.admin, 200, &entry)
	if entry["status"] != "sent" || len(f.email.sent) != 1 {
		t.Fatalf("test send %v", entry)
	}
	// Ordinary channels report managed=false.
	other := f.channel(t, "relay", false)
	if other["managed"] != false {
		t.Fatalf("ordinary %v", other)
	}
	if strings.Contains(f.do("GET", Prefix+"/channels/"+id, "", f.admin).Body.String(), "NOTIF-MARKER") {
		t.Fatal("password echoed")
	}
}
