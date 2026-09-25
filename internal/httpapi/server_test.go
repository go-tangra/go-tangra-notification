package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/inbox"
	"github.com/go-tangra/go-tangra-notification/v4/internal/memstore"
	"github.com/go-tangra/go-tangra-notification/v4/internal/messages"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stats"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stream"
	"github.com/go-tangra/go-tangra-notification/v4/internal/transfer"
	"github.com/go-tangra/go-tangra/v4/freyatest/testrt"
	"github.com/go-tangra/go-tangra/v4/freyatest/testutil"
)

const (
	issuer = "https://localhost:8443"
	tA     = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	tB     = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c66"
	uA     = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c77"
	uB     = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c88"
	uC     = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c99"
)

type signer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newSigner() signer {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return signer{priv, pub}
}

func (s signer) mint(sub, tid string, roles []string) string {
	now := time.Now()
	c := authclient.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: sub, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)), ID: "j1"}, TenantID: tid, SessionID: "s1", Roles: roles, AMR: []string{"pwd"}}
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	t.Header["kid"] = "k1"
	out, _ := t.SignedString(s.priv)
	return out
}

func newTestServer(t *testing.T, opts ...Option) (*Server, signer) {
	t.Helper()
	sg := newSigner()
	v := authclient.New(authclient.Config{Issuer: issuer}, authclient.StaticKeys{"k1": sg.pub}, nil)
	if err := v.RefreshKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	rt := testrt.New(t, testutil.MustCA("example.org"), "notification")
	s, err := NewHandler(rt, append([]Option{WithVerifier(v)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return s, sg
}

func do(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://localhost"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func auth(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

// fakeProvider records sends and can fail.
type fakeProvider struct {
	kind string
	sent []channel.Message
	fail error
}

func (f *fakeProvider) Type() string { return f.kind }
func (f *fakeProvider) Validate(s sealed.Settings) error {
	if _, ok := s["host"]; !ok {
		return errors.New("host is required")
	}
	return nil
}
func (f *fakeProvider) Secret() []string { return []string{"password"} }
func (f *fakeProvider) Send(_ context.Context, _ sealed.Settings, m channel.Message) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, m)
	return nil
}

type fakeLimiter struct{ counts map[string]int }

func (l *fakeLimiter) Limited(_ context.Context, kind, subject string, limit int, _ time.Time) (bool, error) {
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	l.counts[kind+":"+subject]++
	return limit > 0 && l.counts[kind+":"+subject] > limit, nil
}

// fx is a fully wired server on the in-memory store.
type fx struct {
	s        *Server
	sg       signer
	ms       *memstore.Store
	aw       *audit.Writer
	az       *authz.Authz
	email    *fakeProvider
	msgs     *messages.Service
	hub      *stream.Hub
	kv       *stream.Memory
	ops      OpsDeps
	ch       *notify.Channels
	tp       *notify.Templates
	perms    map[string]bool // "user:perm" → held
	now      time.Time
	admin    string
	member   string
	memberC  string
	otherTen string
}

func newFx(t *testing.T) *fx {
	t.Helper()
	s, sg := newTestServer(t)
	ms := memstore.New()
	aw := audit.NewWriter(ms, nil)
	t.Cleanup(aw.Close)
	az := authz.New(ms, aw)
	env, _ := sealed.NewEnvelope(bytes.Repeat([]byte{3}, 32))
	email := &fakeProvider{kind: "email"}
	reg := channel.NewRegistry(email, channel.Nop{Kind: "sms"}, channel.Nop{Kind: "slack"}, channel.Nop{Kind: "sse"})
	f := &fx{s: s, sg: sg, ms: ms, aw: aw, az: az, email: email, now: time.Unix(1_700_000_000, 0), perms: map[string]bool{}}
	ms.Now = func() time.Time { return f.now }
	az.SetClock(func() time.Time { return f.now })
	ch := notify.NewChannels(ms, env, reg, az, aw)
	tp := notify.NewTemplates(ms, az, aw)
	f.ch, f.tp = ch, tp
	snd := notify.NewSender(ms, ch, tp, az, aw, &fakeLimiter{}, notify.Limits{PerTenant: 100, PerSender: 50})
	snd.SetClock(func() time.Time { return f.now })
	perms := PermissionFunc(func(_ context.Context, _, userID, perm string) bool { return f.perms[userID+":"+perm] })
	nd := NotifyDeps{Channels: ch, Templates: tp, Sender: snd, Authz: az, Perms: perms}
	s.RegisterChannels(nd)
	s.RegisterTemplates(nd)
	s.RegisterNotifications(nd)
	s.RegisterGrants(GrantDeps{Authz: az})
	f.kv = stream.NewMemory()
	f.hub = stream.NewHub(f.kv, stream.Config{StreamsPerUser: 2, StreamsPerTenant: 3}, nil)
	t.Cleanup(f.hub.Close)
	f.msgs = messages.New(ms, aw, messages.StaticDirectory{Active: map[string]bool{uA: true, uB: true, uC: true}}, f.hub)
	f.msgs.SetClock(func() time.Time { return f.now })
	ib := inbox.New(ms, aw)
	s.RegisterMessages(MessageDeps{Messages: f.msgs, Inbox: ib, Perms: perms})
	s.RegisterStream(StreamDeps{Hub: f.hub, Audit: aw})
	tr := transfer.New(ms, ch, tp, f.msgs, aw)
	f.ops = OpsDeps{Transfer: tr, MaxBytes: 1 << 20, Stats: stats.New(ms, f.hub), Audit: ms, Version: "test",
		Health: func(context.Context) any { return map[string]any{"database": "ok", "valkey": "ok"} }}
	s.RegisterOps(f.ops)
	f.admin = sg.mint(uA, tA, []string{"admin"})
	f.member = sg.mint(uB, tA, []string{"member"})
	f.memberC = sg.mint(uC, tA, []string{"member"})
	f.otherTen = sg.mint(uA, tB, []string{"admin"})
	f.perms[uA+":stats:read"] = true
	f.perms[uA+":messages:manage"] = true
	return f
}

func (f *fx) do(method, path, body, tok string) *httptest.ResponseRecorder {
	return do(f.s, method, path, body, auth(tok))
}

// call decodes a JSON response into v after checking the status.
func (f *fx) call(t *testing.T, method, path, body, tok string, want int, v any) {
	t.Helper()
	w := f.do(method, path, body, tok)
	if w.Code != want {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
	}
	if v != nil && w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatalf("%s %s: decode %v: %s", method, path, err, w.Body)
		}
	}
}

func (f *fx) channel(t *testing.T, name string, def bool) map[string]any {
	t.Helper()
	var out map[string]any
	f.call(t, "POST", Prefix+"/channels", `{"name":"`+name+`","type":"email","settings":{"host":"relay","port":587,"from":"noreply@example.org","password":"NOTIF-MARKER-PW-`+name+`"},"enabled":true,"is_default":`+boolStr(def)+`}`, f.admin, 201, &out)
	return out
}

func (f *fx) template(t *testing.T, name, channelID string) map[string]any {
	t.Helper()
	var out map[string]any
	f.call(t, "POST", Prefix+"/templates", `{"name":"`+name+`","channel_id":"`+channelID+`","subject":"Hello {{.Name}}","body":"<p>Hi {{.Name}}</p>","variables":["Name"]}`, f.admin, 201, &out)
	return out
}

// audit flushes the writer and counts events of a type in tenant A.
func (f *fx) audit(et string) int {
	f.aw.Flush()
	return len(f.ms.AuditEvents(tA, et))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestDeclaredRoutesMountedAndAuthenticated(t *testing.T) {
	s, sg := newTestServer(t)
	if len(s.Declared()) != 46 {
		t.Fatalf("declared %d", len(s.Declared()))
	}
	tok := sg.mint("u1", "t1", []string{"member"})
	forged := newSigner().mint("u1", "t1", nil)
	if w := do(s, "GET", Prefix+"/stats", "", nil); w.Code != 401 || !strings.Contains(w.Body.String(), "unauthenticated") || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("no token: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", Prefix+"/stats", "", auth(forged)); w.Code != 401 {
		t.Fatalf("forged: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", Prefix+"/stats", "", auth(tok)); w.Code != 501 || !strings.Contains(w.Body.String(), "not_implemented") {
		t.Fatalf("declared: %d %s", w.Code, w.Body)
	}
	w := do(s, "GET", Prefix+"/stats", "", auth(tok))
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Frame-Options": "DENY"} {
		if w.Header().Get(k) != v {
			t.Errorf("%s = %q", k, w.Header().Get(k))
		}
	}
	s.MustHandle("GET", Prefix+"/stats", func(w http.ResponseWriter, r *http.Request) {
		id, err := Caller(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		WriteJSON(w, 200, map[string]any{"user": id.UserID, "tenant": id.TenantID, "roles": id.Roles, "rid": RequestID(r)})
	})
	w = do(s, "GET", Prefix+"/stats", "", map[string]string{"Authorization": "Bearer " + tok, "X-Request-Id": "r1"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"tenant":"t1"`) || !strings.Contains(w.Body.String(), `"member"`) || !strings.Contains(w.Body.String(), `"rid":"r1"`) {
		t.Fatalf("identity: %d %s", w.Code, w.Body)
	}
	if len(s.Implemented()) != 1 || len(s.Missing()) != len(s.Declared())-1 {
		t.Fatalf("implemented %v", s.Implemented())
	}
	if err := s.HandleFunc("GET", Prefix+"/nope", nil); err == nil {
		t.Fatal("expected undeclared error")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		s.MustHandle("GET", "/nope", nil)
	}()
	if w := do(s, "GET", Prefix+"/nope", "", auth(tok)); w.Code != 404 {
		t.Fatalf("404: %d", w.Code)
	}
	if w := do(s, "PATCH", Prefix+"/stats", "", auth(tok)); w.Code != 405 {
		t.Fatalf("405: %d", w.Code)
	}
	// No public routes exist: the document refuses them and the server has none.
	if len(s.public) != 0 || s.IsPublic("GET", Prefix+"/health") {
		t.Fatal("public routes")
	}
	rt := testrt.New(t, testutil.MustCA("example.org"), "notification")
	bare, err := NewHandler(rt)
	if err != nil {
		t.Fatal(err)
	}
	if w := do(bare, "GET", Prefix+"/stats", "", nil); w.Code != 401 {
		t.Fatalf("bare: %d", w.Code)
	}
}

func TestValidation(t *testing.T) {
	s, sg := newTestServer(t)
	h := auth(sg.mint("u1", "t1", nil))
	for _, tc := range []struct {
		method, path, body, reason string
	}{
		{"POST", Prefix + "/channels", `{"name":"x","type":"email","settings":{},"extra":1}`, "validation_failed"},
		{"POST", Prefix + "/channels", `{"name":"x","type":"fax","settings":{}}`, "validation_failed"},
		{"POST", Prefix + "/channels", `{"name":`, "malformed_body"},
		{"POST", Prefix + "/channels", `{"name":"` + strings.Repeat("x", 101) + `","type":"email","settings":{}}`, "validation_failed"},
		{"GET", Prefix + "/channels/not-a-uuid", "", "validation_failed"},
		{"GET", Prefix + "/channels?limit=1000", "", "validation_failed"},
		{"GET", Prefix + "/notifications?status=lost", "", "validation_failed"},
		{"POST", Prefix + "/templates", `{"name":"t","channel_id":"` + tA + `","subject":"a\nb","body":""}`, "validation_failed"},
		{"POST", Prefix + "/templates", `{"name":"t","channel_id":"` + tA + `","subject":"a","body":"","variables":["1bad"]}`, "validation_failed"},
		{"POST", Prefix + "/notifications/send", `{"template_id":"` + tA + `","recipient":"a\r\nb"}`, "validation_failed"},
		{"POST", Prefix + "/grants", `{"resource_type":"folder","resource_id":"` + tA + `","subject_type":"user","relation":"owner"}`, "validation_failed"},
		{"GET", Prefix + "/grants", "", "validation_failed"},
		{"GET", Prefix + "/access/check?resource_type=channel&resource_id=" + tA + "&action=fly", "", "validation_failed"},
		{"POST", Prefix + "/messages", `{"title":"","content":"c","recipients":{"all":true}}`, "validation_failed"},
		{"POST", Prefix + "/messages", `{"title":"t","content":"c","type":"broadcast","recipients":{"all":true}}`, "validation_failed"},
		{"POST", Prefix + "/inbox/status", `{"ids":[],"status":"read"}`, "validation_failed"},
		{"POST", Prefix + "/inbox/status", `{"ids":["` + tA + `"],"status":"deleted"}`, "validation_failed"},
		{"POST", Prefix + "/backup/import?mode=merge", `{}`, "validation_failed"},
		{"GET", Prefix + "/inbox?status=archived", "", "validation_failed"},
	} {
		want := 422
		if tc.reason == "malformed_body" {
			want = 400
		}
		if w := do(s, tc.method, tc.path, tc.body, h); w.Code != want || !strings.Contains(w.Body.String(), tc.reason) {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	// Valid → reaches the (unimplemented) handler.
	if w := do(s, "POST", Prefix+"/channels", `{"name":"ok","type":"email","settings":{"host":"h","port":25,"from":"a@b"}}`, h); w.Code != 501 {
		t.Fatalf("valid: %d %s", w.Code, w.Body)
	}
	// JSON bodies above 64 KiB are refused; the import route accepts 16 MiB and refuses above.
	big := `{"name":"` + strings.Repeat("x", MaxBodyBytes) + `","type":"email","settings":{}}`
	if w := do(s, "POST", Prefix+"/channels", big, h); w.Code != 413 || !strings.Contains(w.Body.String(), "body_too_large") {
		t.Fatalf("json limit: %d %s", w.Code, w.Body)
	}
	var seen int64
	s.MustHandle("POST", Prefix+"/backup/import", func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		if err := DecodeJSON(r, &v, 16<<20); err != nil {
			Fail(w, r, nil, err)
			return
		}
		seen = int64(len(v["items"].(string)))
		w.WriteHeader(200)
	})
	payload := `{"items":"` + strings.Repeat("y", 1<<20) + `"}`
	if w := do(s, "POST", Prefix+"/backup/import", payload, h); w.Code != 200 || seen != 1<<20 {
		t.Fatalf("binary route: %d %s (%d)", w.Code, w.Body, seen)
	}
	huge := `{"items":"` + strings.Repeat("y", 16<<20) + `"}`
	if w := do(s, "POST", Prefix+"/backup/import", huge, h); w.Code != 413 {
		t.Fatalf("binary limit: %d %s", w.Code, w.Body)
	}
}

func TestErrorsNeverLeak(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	for _, tc := range []struct {
		err    error
		status int
		reason string
	}{
		{errors.New("pgx: connection refused at 10.0.0.1"), 503, "temporarily_unavailable"},
		{store.ErrNotFound, 404, "not_found"},
		{store.ErrConflict, 409, "conflict"},
		{ErrForbidden, 403, "forbidden"},
		{&http.MaxBytesError{}, 413, "body_too_large"},
		{context.DeadlineExceeded, 503, "temporarily_unavailable"},
	} {
		r := httptest.NewRequest("GET", "/x", nil)
		w := httptest.NewRecorder()
		Fail(w, r, log, tc.err)
		if w.Code != tc.status || w.Body.String() != `{"reason":"`+tc.reason+`"}`+"\n" {
			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
		}
	}
	if ErrForbidden.Error() != "forbidden" {
		t.Fatal("Error()")
	}
	// Domain errors map with their detail.
	srv, _ := newTestServer(t)
	for _, tc := range []struct {
		err    error
		status int
		reason string
	}{
		{&notify.ValidationError{Msg: "bad", Detail: map[string]any{"variable": "X"}}, 422, `"variable":"X"`},
		{&notify.InUseError{What: "templates", Count: 2}, 409, `"templates":2`},
		{authz.ErrForbidden, 403, "forbidden"},
		{authz.ErrNotFound, 404, "not_found"},
		{authz.ErrInput, 422, "validation_failed"},
		{authz.ErrAboveGranter, 403, "relation_above_granter"},
		{notify.ErrChannelDisabled, 422, "channel_disabled"},
		{notify.ErrRateLimited, 429, "rate_limited"},
		{notify.ErrTypeMismatch, 422, "validation_failed"},
		{channel.ErrNoProvider, 422, "no_provider"},
		{channel.ErrRecipient, 422, "validation_failed"},
		{&messages.ValidationError{Msg: "bad", Detail: map[string]any{"field": "title"}}, 422, `"field":"title"`},
		{messages.ErrState, 409, "conflict"},
		{messages.ErrInUse, 409, "conflict"},
		{inbox.ErrInput, 422, "validation_failed"},
	} {
		r := httptest.NewRequest("GET", "/x", nil)
		w := httptest.NewRecorder()
		srv.fail(w, r, messageError(tc.err))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.reason) {
			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
		}
	}
	de := &DetailError{Err: ErrValidation, Detail: map[string]any{"a": 1}}
	if de.Error() != "validation_failed" || !errors.Is(de, ErrValidation) {
		t.Fatal("detail error")
	}
}

func TestDecodeJSON(t *testing.T) {
	var v struct{ A int }
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"A":1}`))
	if err := DecodeJSON(r, &v, 0); err != nil || v.A != 1 {
		t.Fatal(err)
	}
	for _, body := range []string{`{"B":1}`, `{"A":1} x`, `{`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := DecodeJSON(r, &v, 0); !errors.Is(err, ErrMalformed) {
			t.Errorf("%q: %v", body, err)
		}
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"A":1234567890}`))
	if err := DecodeJSON(r, &v, 4); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("limit: %v", err)
	}
}

func TestRemoteServing(t *testing.T) {
	s, _ := newTestServer(t, WithRemote(fstest.MapFS{
		"mf-manifest.json": {Data: []byte(`{"id":"notification"}`)},
		"assets/a.js":      {Data: []byte("1")},
		"dir/index.html":   {Data: []byte("x")},
	}))
	if w := do(s, "GET", "/ui/mf-manifest.json", "", nil); w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("manifest: %d %s", w.Code, w.Header())
	}
	if w := do(s, "GET", "/ui/assets/a.js", "", nil); w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d", w.Code)
	}
	for _, p := range []string{"/ui/", "/ui/dir/", "/ui/missing.js"} {
		if w := do(s, "GET", p, "", nil); w.Code != 404 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
}

func TestExtensionInt(t *testing.T) {
	for _, v := range []any{float64(3), 3, int64(3)} {
		if n, ok := extensionInt(v); !ok || n != 3 {
			t.Errorf("%T", v)
		}
	}
	if _, ok := extensionInt("3"); ok {
		t.Error("string accepted")
	}
	if !extensionBool(true) || extensionBool("true") {
		t.Error("bool")
	}
}

func TestLimitParam(t *testing.T) {
	for q, want := range map[string]int{"": 0, "limit=7": 7, "limit=0": 0, "limit=x": 0} {
		r := httptest.NewRequest("GET", "/?"+q, nil)
		if got := limitParam(r); got != want {
			t.Errorf("%q: %d", q, got)
		}
	}
}
