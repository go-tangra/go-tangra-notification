//go:build integration

package integration

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

const api = "/api/notification/v1"

// EmailChannel creates an email channel against Mailpit and returns its id.
func (e *Env) EmailChannel(s *Session, name string, def bool) string {
	e.T.Helper()
	code, out := s.JSON(http.MethodPost, api+"/channels", map[string]any{"name": name, "type": "email", "enabled": true, "is_default": def,
		"settings": map[string]any{"host": e.MailHost, "port": e.MailPort, "tls": "none", "from": "Freya <noreply@example.org>"}})
	if code != 201 {
		e.T.Fatalf("channel %s → %d %v", name, code, out)
	}
	return out["id"].(string)
}

// SecretChannel creates an sms channel (no provider yet) whose api_key
// carries a marker; the marker must never leave the sealed column.
func (e *Env) SecretChannel(s *Session, name string) string {
	e.T.Helper()
	code, out := s.JSON(http.MethodPost, api+"/channels", map[string]any{"name": name, "type": "sms", "enabled": true,
		"settings": map[string]any{"account": "acme", "api_key": "NOTIF-MARKER-PW-" + name}})
	if code != 201 {
		e.T.Fatalf("channel %s → %d %v", name, code, out)
	}
	return out["id"].(string)
}

// Template creates a template on a channel and returns its id.
func (e *Env) Template(s *Session, name, channelID string) string {
	e.T.Helper()
	code, out := s.JSON(http.MethodPost, api+"/templates", map[string]any{"name": name, "channel_id": channelID, "subject": "Hello {{.Name}}",
		"body": "<p>Hi {{.Name}}, NOTIF-MARKER-BODY-" + name + "</p>", "variables": []string{"Name"}})
	if code != 201 {
		e.T.Fatalf("template %s → %d %v", name, code, out)
	}
	return out["id"].(string)
}

// Grant creates a grant.
func (e *Env) Grant(s *Session, rtype, rid, stype, sid, rel string, exp *time.Time) (int, map[string]any) {
	e.T.Helper()
	body := map[string]any{"resource_type": rtype, "resource_id": rid, "subject_type": stype, "subject_id": sid, "relation": rel}
	if exp != nil {
		body["expires_at"] = exp.UTC().Format(time.RFC3339)
	}
	return s.JSON(http.MethodPost, api+"/grants", body)
}

// Items returns the "items" array of a listing response.
func Items(body map[string]any) []map[string]any {
	raw, _ := body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// SSE is one open stream through the gateway.
type SSE struct {
	resp   *http.Response
	events chan SSEEvent
	done   chan struct{}
}

// SSEEvent is one parsed frame.
type SSEEvent struct{ ID, Type, Data string }

// Stream opens the live stream (Last-Event-ID optional) and parses frames.
func (s *Session) Stream(lastID string) (*SSE, int) {
	s.Env.T.Helper()
	var hdr []string
	if lastID != "" {
		hdr = []string{"Last-Event-ID", lastID}
	}
	resp := s.Raw(http.MethodGet, api+"/stream", nil, "", hdr...)
	st := &SSE{resp: resp, events: make(chan SSEEvent, 256), done: make(chan struct{})}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		close(st.done)
		return st, resp.StatusCode
	}
	go func() {
		defer close(st.done)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		var ev SSEEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.Type != "" || ev.Data != "" {
					st.events <- ev
				}
				ev = SSEEvent{}
			case strings.HasPrefix(line, "id: "):
				ev.ID = line[4:]
			case strings.HasPrefix(line, "event: "):
				ev.Type = line[7:]
			case strings.HasPrefix(line, "data: "):
				ev.Data = line[6:]
			}
		}
	}()
	return st, 200
}

// Next waits for the next event of a type ("" = any) within d.
func (st *SSE) Next(typ string, d time.Duration) (SSEEvent, bool) {
	deadline := time.After(d)
	for {
		select {
		case ev := <-st.events:
			if typ == "" || ev.Type == typ {
				return ev, true
			}
		case <-deadline:
			return SSEEvent{}, false
		case <-st.done:
			return SSEEvent{}, false
		}
	}
}

// Close ends the stream.
func (st *SSE) Close() {
	_ = st.resp.Body.Close()
	select {
	case <-st.done:
	case <-time.After(5 * time.Second):
	}
}

// ScanForMaterial dumps every module table, the audit hypertable and every
// captured service log and fails when any marker appears (SC-002).
func (e *Env) ScanForMaterial(t *testing.T, markers []string) {
	t.Helper()
	e.Notif.Audit.Flush()
	var dump strings.Builder
	err := e.Notif.Store.Tx(context.Background(), store.Scope{System: true}, func(tx pgx.Tx) error {
		for _, table := range []string{"channels", "templates", "grants", "message_categories", "messages", "inbox", "notification_log", "notification_audit_events"} {
			rows, err := tx.Query(context.Background(), "SELECT to_jsonb(t) FROM "+table+" t") // #nosec G202 -- fixed table names
			if err != nil {
				return err
			}
			for rows.Next() {
				var js []byte
				if err := rows.Scan(&js); err != nil {
					rows.Close()
					return err
				}
				dump.WriteString(table + ": " + string(js) + "\n")
			}
			rows.Close()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range e.logs {
		b, _ := os.ReadFile(p)
		dump.WriteString(fmt.Sprintf("log %s: %s\n", name, b))
	}
	text := dump.String()
	for _, m := range markers {
		if i := strings.Index(text, m); i >= 0 {
			start := max(i-120, 0)
			t.Fatalf("marker %q found in a dump: …%s…", m, text[start:min(len(text), i+80)])
		}
	}
}

// ScanTablesOnly is ScanForMaterial without the logs (rendered bodies are
// stored on purpose; they must never reach a log).
func (e *Env) ScanLogsFor(t *testing.T, markers []string) {
	t.Helper()
	for name, p := range e.logs {
		b, _ := os.ReadFile(p)
		for _, m := range markers {
			if strings.Contains(string(b), m) {
				t.Fatalf("marker %q found in the %s log", m, name)
			}
		}
	}
}
