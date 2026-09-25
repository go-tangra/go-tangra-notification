//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/app"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel/email"
	"github.com/go-tangra/go-tangra-notification/v4/internal/config"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
)

// relayMod points platform_email at an in-process relay offering STARTTLS
// with a certificate for "localhost" from the relay's own test CA.
func relayMod(r *Relay) Mod {
	return func(c *config.Config, o *app.Options) {
		c.PlatformEmail = &config.PlatformEmail{Host: "localhost", Port: r.Port, TLS: "starttls", From: "Tangra <tangra@example.org>"}
		o.PlatformProvider = &email.Provider{DialTimeout: 10 * time.Second, TLSConfig: &tls.Config{RootCAs: r.CA, MinVersion: tls.VersionTLS12}}
	}
}

// TestPlatformEmailChannel (US1, T015): with platform_email configured the
// module starts with a managed "Platform email" channel in the platform
// tenant; it is read-only, a test send is delivered over STARTTLS with the
// relay certificate verified; a relay without STARTTLS fails without any
// plaintext fallback.
func TestPlatformEmailChannel(t *testing.T) {
	relay := StartRelay(t, true)
	e := StartPlatform(t, relayMod(relay))
	if !e.LogContains("notification", "platform email channel ready") {
		t.Fatal("no start-up log line for the platform channel")
	}
	code, list := e.Operator.JSON(http.MethodGet, api+"/channels?type=email", nil)
	if code != 200 {
		t.Fatalf("list %d %v", code, list)
	}
	var ch map[string]any
	for _, it := range list["items"].([]any) {
		if m := it.(map[string]any); m["managed"] == true {
			ch = m
		}
	}
	if ch == nil || ch["name"] != "Platform email" || ch["enabled"] != true || ch["is_default"] != true {
		t.Fatalf("platform channel %v", list)
	}
	id := ch["id"].(string)
	if p := ch["permissions"].(map[string]any); p["write"] != false || p["delete"] != false {
		t.Fatalf("permissions %v", p)
	}
	if code, out := e.Operator.JSON(http.MethodPut, api+"/channels/"+id, map[string]any{"name": "mine", "type": "email", "enabled": true,
		"settings": map[string]any{"host": "localhost", "port": relay.Port, "from": "a@example.org"}}); code != 409 || out["reason"] != "managed_channel" {
		t.Fatalf("update %d %v", code, out)
	}
	if code, out := e.Operator.JSON(http.MethodPost, api+"/channels/"+id+"/remove", nil); code != 409 || out["reason"] != "managed_channel" {
		t.Fatalf("remove %d %v", code, out)
	}
	code, entry := e.Operator.JSON(http.MethodPost, api+"/channels/"+id+"/test", map[string]any{"recipient": "ops@example.org"})
	if code != 200 || entry["status"] != "sent" {
		t.Fatalf("test send %d %v", code, entry)
	}
	if m := relay.WaitMessage(t, "ops@example.org"); !m.TLS || m.From != "tangra@example.org" || !strings.Contains(m.Data, "test message") {
		t.Fatalf("relay message %+v", m)
	}

	// A relay without STARTTLS (a changed configuration applied at start):
	// the send fails, naming the reason, and no mail command is ever sent.
	plain := StartRelay(t, false)
	res, err := e.Notif.Channels.EnsurePlatformChannel(context.Background(), e.PlatformID, &notify.PlatformEmail{Host: "localhost", Port: plain.Port, TLS: "starttls", From: "tangra@example.org"})
	if err != nil || res != notify.PlatformUpdated {
		t.Fatalf("reconfigure %q %v", res, err)
	}
	code, entry = e.Operator.JSON(http.MethodPost, api+"/channels/"+id+"/test", map[string]any{"recipient": "ops@example.org"})
	if code != 200 || entry["status"] != "failed" || !strings.Contains(entry["error"].(string), "relay offers no STARTTLS") {
		t.Fatalf("plaintext relay %d %v", code, entry)
	}
	for _, c := range plain.Commands() {
		if c == "MAIL" || c == "RCPT" || c == "DATA" || c == "AUTH" {
			t.Fatalf("plaintext fallback: %v", plain.Commands())
		}
	}
	if len(plain.Messages()) != 0 {
		t.Fatal("message accepted in plaintext")
	}
}
