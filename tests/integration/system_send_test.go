//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"io"
	"mime/quotedprintable"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/go-tangra/go-tangra-notification/sdk/v4/pkg/notifyclient"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
	"github.com/go-tangra/go-tangra/v4/freyatest/testutil"
)

// meshClient dials the module's gRPC port as another platform service:
// its SVID from the harness CA (the module verifies it and applies the
// mesh policy); the test skips verifying the module's own certificate.
func (e *Env) meshClient(service string) *notifyclient.Client {
	e.T.Helper()
	crt := e.CA.MustIssue(service, testutil.IssueOptions{})
	cfg := &tls.Config{Certificates: []tls.Certificate{crt}, InsecureSkipVerify: true, MinVersion: tls.VersionTLS13} //nolint:gosec // test client; the server side verifies
	conn, err := grpc.NewClient(e.Notif.Cfg.Server.GRPCAddr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		e.T.Fatal(err)
	}
	e.T.Cleanup(func() { _ = conn.Close() })
	return notifyclient.New(conn)
}

// TestSystemSendRedactionScan (US2, T033, SC-003): auth sends auth.invite
// over the mesh; the recipient gets the link through the relay, while the
// stored log row, every audit row, the module's log output and the log
// entry read through the API carry no trace of the token. warden may not
// send auth's template.
func TestSystemSendRedactionScan(t *testing.T) {
	relay := StartRelay(t, true)
	e := StartPlatform(t, relayMod(relay))
	ctx := context.Background()
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	token := "NOTIF-TOKEN-" + hex.EncodeToString(raw)
	link := "https://platform.example.org/console/invite/accept?token=" + token + "&lang=en"

	auth := e.meshClient("auth")
	var res notifyclient.Result
	var err error
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) { // the policy and identity settle right after start
		if res, err = auth.SendKey(ctx, e.PlatformID, "auth.invite", "alice@example.org", map[string]string{"link": link, "valid_for": "72 hours", "tenant": "Platform"}, "corr-017"); err == nil && !res.Retryable {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil || !res.Sent || res.LogID == "" {
		t.Fatalf("send %+v %v", res, err)
	}
	m := relay.WaitMessage(t, "alice@example.org")
	body, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(m.Data)))
	if !m.TLS || !strings.Contains(string(body), token) {
		t.Fatalf("recipient did not get the link over TLS: %+v", m)
	}

	// warden may not send auth's template: refused and audited.
	if _, err := e.meshClient("warden").SendKey(ctx, e.PlatformID, "auth.invite", "eve@example.org", map[string]string{"link": link, "valid_for": "1 hour"}, ""); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("warden → auth.invite: %v", err)
	}
	if n := len(e.AuditRows(e.PlatformID, "access_refused")); n == 0 {
		t.Fatal("namespace refusal not audited")
	}

	// Scan everything the module stored or logged.
	e.Notif.Audit.Flush()
	var stored []string
	err = e.Notif.Store.Tx(ctx, store.Scope{System: true}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT rendered_subject || ' ' || rendered_body || ' ' || error || ' ' || COALESCE(template_key, '') FROM notification_log UNION ALL SELECT reason || ' ' || details::text FROM notification_audit_events")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			stored = append(stored, s)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stored, "\n")
	if strings.Contains(joined, token) || !strings.Contains(joined, "[redacted]") || !strings.Contains(joined, "auth.invite") {
		t.Fatalf("stored rows: %q", joined)
	}
	logs, _ := os.ReadFile(e.logs["notification"])
	if strings.Contains(string(logs), token) {
		t.Fatal("token in the module log")
	}
	code, entry := e.Operator.JSON(http.MethodGet, api+"/notifications/"+res.LogID, nil)
	if code != 200 || entry["template_key"] != "auth.invite" || strings.Contains(entry["rendered_body"].(string), token) || !strings.Contains(entry["rendered_body"].(string), "[redacted]") {
		t.Fatalf("log entry %d %v", code, entry)
	}
}
