package taskexec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schedulerv1 "github.com/go-tangra/go-tangra-scheduler/sdk/v4/api/proto/scheduler/v1"
	sched "github.com/go-tangra/go-tangra-scheduler/sdk/v4/pkg/taskexec"

	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
	"github.com/go-tangra/go-tangra/v4/authn"
	"github.com/go-tangra/go-tangra/v4/identity"
)

const (
	tenant  = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	channel = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c99"
)

type fakeSender struct {
	subj  authz.Subjects
	in    notify.CustomInput
	calls int
	view  notify.LogView
	err   error
}

func (f *fakeSender) SendCustom(_ context.Context, subj authz.Subjects, in notify.CustomInput) (notify.LogView, error) {
	f.calls++
	f.subj, f.in = subj, in
	return f.view, f.err
}

func request(payload string) sched.Request {
	return sched.Request{ExecutionID: "exec-1", Type: TypeSendTestEmail, TenantID: tenant, Payload: json.RawMessage(payload), Attempt: 1, MaxAttempts: 2}
}

func newExec(t *testing.T, s Sender) *Executor {
	t.Helper()
	e, err := New(s, "example.org", "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestNewValidatesIdentity(t *testing.T) {
	for _, tc := range [][2]string{{"", "scheduler"}, {"example.org", ""}, {"Example.ORG", "scheduler"}, {"example.org", "Sched uler"}} {
		if _, err := New(&fakeSender{}, tc[0], tc[1]); err == nil {
			t.Errorf("%q: accepted", tc)
		}
	}
	if _, err := New(nil, "example.org", "scheduler"); err == nil {
		t.Error("nil sender accepted")
	}
}

func TestSendTestEmailSent(t *testing.T) {
	s := &fakeSender{view: notify.LogView{ID: "log-1", ChannelID: channel, Status: "sent"}}
	e := newExec(t, s)
	res := e.Handlers()[TypeSendTestEmail](context.Background(), request(`{"recipient":"Ann <ann@example.org>","subject":"Hi","body":"text","channelId":"`+channel+`"}`))
	if !res.Success || res.Permanent || res.Message != "Test email sent to ann@example.org via channel "+channel {
		t.Fatalf("result %+v", res)
	}
	if d, ok := res.Data.(map[string]string); !ok || d["log_id"] != "log-1" || d["channel_id"] != channel {
		t.Fatalf("data %+v", res.Data)
	}
	if s.subj.TenantID != tenant || s.subj.Service != "spiffe://example.org/svc/scheduler" || s.subj.UserID != "" {
		t.Fatalf("subjects %+v", s.subj)
	}
	want := notify.CustomInput{ChannelID: channel, Recipient: "ann@example.org", Subject: "Hi", Body: "text", CorrelationID: "exec-1"}
	if s.in != want {
		t.Fatalf("input %+v", s.in)
	}
	// Minimal payload: defaults come from SendCustom.
	res = e.Handlers()[TypeSendTestEmail](context.Background(), request(`{"recipient":"bob@example.org"}`))
	if !res.Success || s.in.Subject != "" || s.in.Body != "" || s.in.ChannelID != "" {
		t.Fatalf("minimal %+v %+v", res, s.in)
	}
}

func TestSendTestEmailInvalidPayload(t *testing.T) {
	s := &fakeSender{view: notify.LogView{Status: "sent"}}
	e := newExec(t, s)
	for name, payload := range map[string]string{
		"not an object":   `[1]`,
		"unknown field":   `{"recipient":"a@example.org","cc":"b@example.org"}`,
		"wrong type":      `{"recipient":42}`,
		"missing":         `{}`,
		"bad recipient":   `{"recipient":"not an address"}`,
		"two recipients":  `{"recipient":"a@example.org, b@example.org"}`,
		"bad channel":     `{"recipient":"a@example.org","channelId":"42"}`,
		"long subject":    `{"recipient":"a@example.org","subject":"` + strings.Repeat("s", 201) + `"}`,
		"crlf subject":    `{"recipient":"a@example.org","subject":"a\r\nBcc: x@example.org"}`,
		"long body":       `{"recipient":"a@example.org","body":"` + strings.Repeat("b", 10001) + `"}`,
		"trailing object": `{"recipient":"a@example.org"}{}`,
	} {
		res := e.Handlers()[TypeSendTestEmail](context.Background(), request(payload))
		if res.Success || !res.Permanent || strings.Contains(res.Message, "example.org") {
			t.Errorf("%s: %+v", name, res)
		}
	}
	// Non-ASCII subject of 200 characters is within bounds.
	if res := e.Handlers()[TypeSendTestEmail](context.Background(), request(`{"recipient":"a@example.org","subject":"`+strings.Repeat("é", 200)+`"}`)); !res.Success {
		t.Errorf("200 characters: %+v", res)
	}
	bad := request(`{"recipient":"a@example.org"}`)
	bad.TenantID = ""
	if res := e.Handlers()[TypeSendTestEmail](context.Background(), bad); res.Success || !res.Permanent {
		t.Errorf("no tenant: %+v", res)
	}
	if s.calls != 1 {
		t.Fatalf("sender called %d times", s.calls)
	}
}

func TestSendTestEmailOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		view      notify.LogView
		err       error
		success   bool
		permanent bool
		msg       string
	}{
		{"not found", notify.LogView{}, store.ErrNotFound, false, true, "channel not found"},
		{"wrong type", notify.LogView{}, notify.ErrTypeMismatch, false, true, "not an email channel"},
		{"disabled", notify.LogView{}, notify.ErrChannelDisabled, false, true, "disabled"},
		{"not configured", notify.LogView{}, notify.ErrEmailNotConfigured, false, true, "no default email channel"},
		{"invalid", notify.LogView{}, &notify.ValidationError{Msg: "recipient is not valid"}, false, true, "recipient is not valid"},
		{"rate limited", notify.LogView{}, notify.ErrRateLimited, false, false, "rate limit"},
		{"store down", notify.LogView{}, errors.New("db: connection refused 10.0.0.1"), false, false, "temporarily unavailable"},
		{"retryable delivery", notify.LogView{ID: "l", ChannelID: channel, Status: "failed", Error: "451 try later", Retryable: true}, nil, false, false, "451 try later"},
		{"permanent delivery", notify.LogView{ID: "l", ChannelID: channel, Status: "failed", Error: "550 no such user"}, nil, false, true, "550 no such user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newExec(t, &fakeSender{view: tc.view, err: tc.err})
			res := e.Handlers()[TypeSendTestEmail](context.Background(), request(`{"recipient":"a@example.org"}`))
			if res.Success != tc.success || res.Permanent != tc.permanent || !strings.Contains(res.Message, tc.msg) || strings.Contains(res.Message, "10.0.0.1") {
				t.Fatalf("%+v", res)
			}
		})
	}
}

func TestDescriptorsAndSchema(t *testing.T) {
	ds := Descriptors()
	if len(ds) != 1 {
		t.Fatalf("%d descriptors", len(ds))
	}
	d := ds[0]
	if d.Type != TypeSendTestEmail || d.DisplayName != "Send test email" || d.Description == "" || d.DefaultCron != "" || d.DefaultMaxRetry != 1 || d.Platform {
		t.Fatalf("descriptor %+v", d)
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(d.PayloadSchema))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource("payload.json", doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("payload.json")
	if err != nil {
		t.Fatal(err)
	}
	valid := func(raw string) bool {
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		return schema.Validate(v) == nil
	}
	for _, ok := range []string{`{"recipient":"a@example.org"}`, `{"recipient":"a@example.org","subject":"s","body":"b","channelId":"` + channel + `"}`} {
		if !valid(ok) {
			t.Errorf("refused %s", ok)
		}
	}
	for _, bad := range []string{`{}`, `{"recipient":"nope"}`, `{"recipient":"a@example.org","x":1}`, `{"recipient":"a@example.org","channelId":"42"}`,
		`{"recipient":"a@example.org","subject":"a\nb"}`, `{"recipient":"a@example.org","subject":"` + strings.Repeat("s", 201) + `"}`} {
		if valid(bad) {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestCaller(t *testing.T) {
	caller := Caller("example.org")
	if _, ok := caller(context.Background()); ok {
		t.Fatal("no peer accepted")
	}
	ctx := authn.WithPeer(context.Background(), authn.PeerIdentity{ID: identity.ForService("example.org", "scheduler")})
	if name, ok := caller(ctx); !ok || name != "scheduler" {
		t.Fatalf("scheduler: %q %v", name, ok)
	}
	ctx = authn.WithPeer(context.Background(), authn.PeerIdentity{ID: identity.ForService("other.org", "scheduler")})
	if _, ok := caller(ctx); ok {
		t.Fatal("foreign trust domain accepted")
	}
}

// TestServerEndToEnd: the SDK server refuses callers other than the
// scheduler and runs the handler for it.
func TestServerEndToEnd(t *testing.T) {
	s := &fakeSender{view: notify.LogView{ID: "log-1", ChannelID: channel, Status: "sent"}}
	e := newExec(t, s)
	srv := e.Server(Caller("example.org"), nil)
	req := &schedulerv1.ExecuteTaskRequest{TaskType: TypeSendTestEmail, TenantId: tenant, Payload: []byte(`{"recipient":"a@example.org"}`), ExecutionId: "e"}
	ctx := authn.WithPeer(context.Background(), authn.PeerIdentity{ID: identity.ForService("example.org", "ipam")})
	if _, err := srv.ExecuteTask(ctx, req); err == nil {
		t.Fatal("ipam may execute")
	}
	ctx = authn.WithPeer(context.Background(), authn.PeerIdentity{ID: identity.ForService("example.org", "scheduler")})
	res, err := srv.ExecuteTask(ctx, req)
	if err != nil || !res.GetSuccess() || string(res.GetResultData()) != `{"channel_id":"`+channel+`","log_id":"log-1"}` {
		t.Fatalf("%+v %v", res, err)
	}
}
