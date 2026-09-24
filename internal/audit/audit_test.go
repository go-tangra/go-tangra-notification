package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

type memIns struct {
	mu   sync.Mutex
	rows []store.AuditRow
	err  error
	n    int
}

func (m *memIns) InsertAuditRows(_ context.Context, rows []store.AuditRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	if m.err != nil {
		return m.err
	}
	m.rows = append(m.rows, rows...)
	return nil
}

func (m *memIns) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.rows) }

func TestValidate(t *testing.T) {
	ok := Event{Type: ChannelCreated, TenantID: "t", ActorKind: "user", ActorID: "u", SubjectKind: "channel", SubjectID: "c", Outcome: "ok"}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	bad := []Event{
		{Type: "secret_created", TenantID: "t", ActorKind: "user", SubjectKind: "channel", Outcome: "ok"},
		{Type: ChannelCreated, ActorKind: "user", SubjectKind: "channel", Outcome: "ok"},
		{Type: ChannelCreated, TenantID: "t", ActorKind: "user", SubjectKind: "channel", Outcome: "maybe"},
		{Type: ChannelCreated, TenantID: "t", ActorKind: "recipient", SubjectKind: "channel", Outcome: "ok"},
		{Type: ChannelCreated, TenantID: "t", ActorKind: "user", SubjectKind: "secret", Outcome: "ok"},
	}
	for i, e := range bad {
		if err := Validate(e); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	for _, name := range []string{"channel_tested", "message_published", "stream_refused", "backup_exported_with_credentials"} {
		if !Known(name) {
			t.Errorf("%s unknown", name)
		}
	}
	if Known("share_created") {
		t.Error("warden vocabulary leaked")
	}
}

func TestRowRedaction(t *testing.T) {
	e := Event{Type: ChannelUpdated, TenantID: "t", ActorKind: "user", ActorID: "u", SubjectKind: "channel", SubjectID: "c", Outcome: "ok", Details: map[string]any{
		"password":      "NOTIF-MARKER-PW-1",
		"smtp_secret":   "x",
		"api_key":       "k",
		"rendered_body": "hello",
		"content":       "hi",
		"settings":      map[string]any{"host": "h"},
		"host":          "smtp.example.org",
		"note":          "NOTIF-MARKER-BODY-2 inside",
		"long":          strings.Repeat("a", 300),
		"nested":        map[string]any{"token": "t", "fine": "yes", "list": []any{"NOTIF-MARKER-PW-3", "ok"}},
		"jwt":           "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
	}}
	row, err := Row(e, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(row.Details, &d); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"password", "smtp_secret", "api_key", "rendered_body", "content", "settings", "note", "jwt"} {
		if d[k] != "[REDACTED]" {
			t.Errorf("%s = %v, want redacted", k, d[k])
		}
	}
	if d["host"] != "smtp.example.org" {
		t.Errorf("host altered: %v", d["host"])
	}
	if len(d["long"].(string)) != 256 {
		t.Errorf("long not truncated: %d", len(d["long"].(string)))
	}
	nested := d["nested"].(map[string]any)
	if nested["token"] != "[REDACTED]" || nested["fine"] != "yes" || nested["list"].([]any)[0] != "[REDACTED]" || nested["list"].([]any)[1] != "ok" {
		t.Errorf("nested %v", nested)
	}
	if strings.Contains(string(row.Details), "NOTIF-MARKER") {
		t.Fatal("marker stored")
	}
	if _, err := Row(Event{Type: "nope"}, time.Now()); err == nil {
		t.Fatal("invalid event produced a row")
	}
	if _, err := Row(Event{Type: ChannelCreated, TenantID: "t", ActorKind: "user", SubjectKind: "channel", Outcome: "ok", Details: map[string]any{"f": func() {}}}, time.Now()); err == nil {
		t.Fatal("unmarshalable details accepted")
	}
}

func TestSafeString(t *testing.T) {
	if SafeString("NOTIF-MARKER-PW-abc") || SafeString("-----BEGIN RSA PRIVATE KEY-----") || !SafeString("plain") || SafeString("x NOTIF-MARKER-BODY-1") {
		t.Fatal("marker detection")
	}
}

func TestWriterBatchAndFlush(t *testing.T) {
	ins := &memIns{}
	w := NewWriter(ins, nil)
	for i := 0; i < 250; i++ {
		if err := w.Emit(Event{Type: NotificationSent, TenantID: "t", ActorKind: "user", ActorID: "u", SubjectKind: "notification", SubjectID: "n", Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	if ins.count() != 250 {
		t.Fatalf("stored %d", ins.count())
	}
	if err := w.Emit(Event{Type: "bogus"}); err == nil {
		t.Fatal("invalid event accepted")
	}
	w.Close()
	w.Close()
	if err := w.Emit(Event{Type: NotificationSent, TenantID: "t", ActorKind: "user", SubjectKind: "notification", Outcome: "ok"}); err == nil || w.Dropped() != 1 {
		t.Fatalf("emit after close: %v dropped=%d", err, w.Dropped())
	}
	w.Flush()
}

func TestWriterErrorsAndOverflow(t *testing.T) {
	ins := &memIns{err: errors.New("db down")}
	var got []error
	var mu sync.Mutex
	w := newWriter(ins, func(err error) { mu.Lock(); got = append(got, err); mu.Unlock() }, 2)
	w.start()
	for i := 0; i < 5; i++ {
		_ = w.Emit(Event{Type: NotificationSent, TenantID: "t", ActorKind: "user", SubjectKind: "notification", Outcome: "ok"})
	}
	w.Flush()
	w.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("no errors reported")
	}
	if w.Dropped() == 0 {
		t.Fatal("no overflow drop")
	}
}

func TestWriterTicker(t *testing.T) {
	ins := &memIns{}
	w := newWriter(ins, nil, 10)
	w.tick = 10 * time.Millisecond
	w.start()
	defer w.Close()
	_ = w.Emit(Event{Type: NotificationSent, TenantID: "t", ActorKind: "user", SubjectKind: "notification", Outcome: "ok"})
	deadline := time.Now().Add(2 * time.Second)
	for ins.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ins.count() != 1 {
		t.Fatal("ticker did not flush")
	}
}
