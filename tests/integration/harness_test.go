//go:build integration

// Package integration boots the platform (gateway and auth as subprocesses
// built from the sibling modules) and the notification module in-process
// against real TimescaleDB, Valkey (TLS), OpenFGA and Mailpit containers.
// Every service holds an SVID from one shared test CA; browsers reach the
// module only through the gateway edge, exactly as in production.
package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/go-tangra/go-tangra-notification/v4/internal/app"
	"github.com/go-tangra/go-tangra-notification/v4/internal/config"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
	"github.com/go-tangra/go-tangra/v4"
	fconfig "github.com/go-tangra/go-tangra/v4/config"
	"github.com/go-tangra/go-tangra/v4/discovery"
	"github.com/go-tangra/go-tangra/v4/freyatest/testutil"
	"github.com/go-tangra/go-tangra/v4/transport/edge"
)

const (
	trustDomain = "example.org"
	password    = "correct horse battery staple 42"
)

// Env is the running platform plus helpers.
type Env struct {
	T          *testing.T
	CA         *testutil.CA
	Notif      *app.App
	Base       string // gateway edge: https://127.0.0.1:port
	Mail       string // mailpit API
	MailHost   string // mailpit SMTP host
	MailPort   int    // mailpit SMTP port
	Operator   *Session
	PlatformID string
	Cancel     context.CancelFunc
	valkey     testcontainers.Container
	mailpit    testcontainers.Container
	authBin    string
	authCfg    string
	logs       map[string]string
	sessions   []*Session
}

// Session is one signed-in browser (its own cookie jar) at the gateway edge.
type Session struct {
	Env    *Env
	Client *http.Client
	Email  string
	UserID string
	Tenant string // slug
}

func Start(t *testing.T) *Env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	ca := testutil.MustCA(trustDomain)
	pgHost, pgPorts := container(t, testcontainers.ContainerRequest{Image: "timescale/timescaledb:latest-pg16", ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{"POSTGRES_PASSWORD": "test", "POSTGRES_DB": "auth"}, WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(2 * time.Minute)})
	pg := pgHost + ":" + pgPorts["5432/tcp"]
	adminAuth := "postgres://postgres:test@" + pg + "/auth?sslmode=disable"
	for i := 0; i < 30; i++ {
		conn, err := pgx.Connect(ctx, adminAuth)
		if err == nil {
			for _, q := range []string{"CREATE ROLE auth_app LOGIN PASSWORD 'app' NOBYPASSRLS", "CREATE ROLE gateway_app LOGIN PASSWORD 'app'", "CREATE ROLE notification_app LOGIN PASSWORD 'app' NOBYPASSRLS",
				"CREATE DATABASE gateway", "CREATE DATABASE notification"} {
				_, _ = conn.Exec(ctx, q)
			}
			_ = conn.Close(ctx)
			break
		}
		time.Sleep(time.Second)
	}
	certPath, keyPath := selfSigned(t, dir)
	vkc := startContainer(t, testcontainers.ContainerRequest{Image: "valkey/valkey:8", ExposedPorts: []string{"6379/tcp"},
		Files:      []testcontainers.ContainerFile{{HostFilePath: certPath, ContainerFilePath: "/tls/server.crt", FileMode: 0o644}, {HostFilePath: keyPath, ContainerFilePath: "/tls/server.key", FileMode: 0o644}},
		Cmd:        []string{"valkey-server", "--tls-port", "6379", "--port", "0", "--tls-cert-file", "/tls/server.crt", "--tls-key-file", "/tls/server.key", "--tls-ca-cert-file", "/tls/server.crt", "--tls-auth-clients", "no", "--requirepass", "test"},
		WaitingFor: wait.ForListeningPort("6379/tcp")})
	vkHost, _ := vkc.Host(ctx)
	vkPort, _ := vkc.MappedPort(ctx, "6379/tcp")
	valkeyAddr := vkHost + ":" + vkPort.Port()
	fgaHost, fgaPorts := container(t, testcontainers.ContainerRequest{Image: "openfga/openfga:v1.20.0", ExposedPorts: []string{"8080/tcp"},
		Cmd: []string{"run", "--authn-method=preshared", "--authn-preshared-keys=test-key", "--playground-enabled=false"}, WaitingFor: wait.ForHTTP("/healthz").WithPort("8080/tcp")})
	mpc := startContainer(t, testcontainers.ContainerRequest{Image: "axllent/mailpit:latest", ExposedPorts: []string{"1025/tcp", "8025/tcp"}, WaitingFor: wait.ForListeningPort("8025/tcp")})
	mpHost, _ := mpc.Host(ctx)
	mpSMTP, _ := mpc.MappedPort(ctx, "1025/tcp")
	mpAPI, _ := mpc.MappedPort(ctx, "8025/tcp")
	mpPorts := map[string]string{"1025/tcp": mpSMTP.Port(), "8025/tcp": mpAPI.Port()}

	kek := make([]byte, 32)
	_, _ = rand.Read(kek)
	kekPath := filepath.Join(dir, "kek.b64")
	_ = os.WriteFile(kekPath, []byte(base64.StdEncoding.EncodeToString(kek)), 0o600)
	nkek := make([]byte, 32)
	_, _ = rand.Read(nkek)
	nkekPath := filepath.Join(dir, "notification-kek.b64")
	_ = os.WriteFile(nkekPath, []byte(base64.StdEncoding.EncodeToString(nkek)), 0o600)
	authHTTP, authGRPC := freePort(t), freePort(t)
	gwEdge, gwGRPC := freePort(t), freePort(t)
	notifGRPC := freePort(t)
	logs := map[string]string{"auth": filepath.Join(dir, "auth.log"), "gateway": filepath.Join(dir, "gateway.log")}

	// --- auth (subprocess, gateway mode)
	authBin, gwBin := buildService(t, "auth", "./cmd/authsvc"), buildService(t, "gateway", "./cmd/gatewaysvc")
	svidDir := filepath.Join(dir, "svid")
	authCert, authKey, bundle, err := ca.WriteSVID(svidDir, "auth", ca.MustIssue("auth", testutil.IssueOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	authCfg := filepath.Join(dir, "auth.yaml")
	authYAML := fmt.Sprintf(`service_name: auth
trust_domain: %s
env: test
identity:
  provider: file
  file: { cert: %s, key: %s, bundle: %s }
authz: { source: file, path: %s }
server: { grpc_addr: %s, http_addr: %s }
admin: { addr: 127.0.0.1:0 }
discovery:
  static:
    gateway: ["%s"]
gateway: { enabled: true, service: gateway }
issuer: https://%s
db:
  dsn: postgres://auth_app:app@%s/auth?sslmode=disable
  migrate_dsn: %s
valkey: { addresses: ["%s"], password: test, ca_file: %s }
openfga: { url: http://%s:%s, preshared_key: test-key, allow_plaintext: true }
kek: { source: file, path: %s }
email: { transport: smtp, host: %s, port: %s, from: auth@example.org, allow_plaintext: true }
`, trustDomain, authCert, authKey, bundle, abs(t, "../../../auth/deploy/policy.yaml"), authGRPC, authHTTP, gwGRPC, gwEdge,
		pg, adminAuth, valkeyAddr, certPath, fgaHost, fgaPorts["8080/tcp"], kekPath, mpHost, mpPorts["1025/tcp"])
	if err := os.WriteFile(authCfg, []byte(authYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	// --- gateway (subprocess)
	gwCert, gwKey, _, err := ca.WriteSVID(filepath.Join(dir, "svid-gw"), "gateway", ca.MustIssue("gateway", testutil.IssueOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	gwCfg := filepath.Join(dir, "gateway.yaml")
	gwYAML := fmt.Sprintf(`service_name: gateway
trust_domain: %s
env: test
identity:
  provider: file
  file: { cert: %s, key: %s, bundle: %s }
authz: { source: file, path: %s }
server: { grpc_addr: %s }
admin: { addr: 127.0.0.1:0 }
discovery:
  static:
    auth: ["%s"]
edge:
  addr: %s
  allowed_origins: ["https://%s"]
  rate_limit: { per_second: 500, burst: 1000 }
public_origin: https://%s
db:
  dsn: postgres://gateway_app:app@%s/gateway?sslmode=disable
  migrate_dsn: postgres://postgres:test@%s/gateway?sslmode=disable
valkey: { addresses: ["%s"], password: test, ca_file: %s }
auth: { service: auth, issuer: https://%s, audience: gateway }
leases: { ttl: 2s, renew: 500ms }
forward: { body_bytes: 1048576, streams_per_client: 32, stream_max: 10m, module_timeout: 30s }
operators: { roles: [operator] }
limits:
  max_request_bytes: 16842752
`, trustDomain, gwCert, gwKey, bundle, abs(t, "../../../gateway/deploy/policy.yaml"), gwGRPC, authGRPC, gwEdge, gwEdge, gwEdge, pg, pg, valkeyAddr, certPath, gwEdge)
	if err := os.WriteFile(gwCfg, []byte(gwYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	// Allow-list both modules before anything registers.
	boot := exec.Command(gwBin, "bootstrap", "-config", gwCfg,
		"-allow", "spiffe://"+trustDomain+"/svc/auth=/api/v1,/authorize,/.well-known,/console;auth",
		"-allow", "spiffe://"+trustDomain+"/svc/notification=/api/notification,/ui;notification")
	if out, err := boot.CombinedOutput(); err != nil {
		t.Fatalf("gateway bootstrap: %v\n%s", err, out)
	}
	startProcess(t, gwBin, gwCfg, logs["gateway"])
	startProcess(t, authBin, authCfg, logs["auth"])

	// --- notification (in-process)
	ncfg := config.Default()
	ncfg.ServiceName, ncfg.TrustDomain, ncfg.Env = "notification", trustDomain, "test"
	ncfg.Server.GRPCAddr, ncfg.Server.HTTPAddr, ncfg.Admin.Addr = notifGRPC, "127.0.0.1:0", "127.0.0.1:0"
	// A test policy: the production rules plus notification→notification, because the
	// harness dials the module's own gRPC as itself to exercise the service RPCs.
	basePolicy, _ := os.ReadFile(abs(t, "../../deploy/policy.yaml"))
	selfRule := "\n  - id: self-rpc\n    from: [\"spiffe://example.org/svc/notification\"]\n    to: [\"notification\"]\n" +
		"    operations: [\"/notification.v1.Notifier/Send\", \"/notification.v1.Notifier/SendTest\", \"/notification.v1.Events/Publish\"]\n    effect: allow\n"
	notifPolicy := filepath.Join(dir, "notification-policy.yaml")
	_ = os.WriteFile(notifPolicy, append(basePolicy, []byte(selfRule)...), 0o600)
	ncfg.Authz = fconfig.Authz{Source: fconfig.AuthzFile, Path: notifPolicy}
	ncfg.Config.Limits.MaxRequestBytes = 16842752
	ncfg.DB.DSN = "postgres://notification_app:app@" + pg + "/notification?sslmode=disable"
	ncfg.DB.MigrateDSN = "postgres://postgres:test@" + pg + "/notification?sslmode=disable"
	ncfg.Valkey = config.Valkey{Addresses: []string{valkeyAddr}, Password: "test", CAFile: certPath}
	ncfg.KEK = config.KEK{Source: "file", Path: nkekPath}
	ncfg.SMTP = config.SMTP{AllowPlaintext: true, DialTimeoutSeconds: 10}
	ncfg.Scheduler = config.Scheduler{IntervalSeconds: 1, LeaseSeconds: 5}
	ncfg.Gateway = config.Gateway{Service: "gateway", Issuer: "https://" + gwEdge}
	ncfg.Limits.SendPerTenantPerMinute, ncfg.Limits.SendPerSenderPerMinute = 600, 600
	disc, err := discovery.NewStatic(map[string][]string{"auth": {authGRPC}, "gateway": {gwGRPC}, "notification": {notifGRPC}})
	if err != nil {
		t.Fatal(err)
	}
	prov := testutil.NewMemProvider(ca, ca.MustIssue("notification", testutil.IssueOptions{}))
	nlog := filepath.Join(dir, "notification.log")
	logs["notification"] = nlog
	nf, _ := os.Create(nlog)
	w, err := app.Build(ctx, ncfg, app.Options{Migrate: true, Logger: slog.NewTextHandler(nf, &slog.HandlerOptions{Level: slog.LevelDebug}), Register: app.Wire,
		Freya: []freya.Option{freya.WithIdentityProvider(prov), freya.WithDiscovery(disc)}})
	if err != nil {
		t.Fatalf("notification build: %v", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
		w.Close()
		_ = nf.Close()
	})
	env := &Env{T: t, CA: ca, Notif: w, Base: "https://" + gwEdge, Mail: fmt.Sprintf("http://%s:%s", mpHost, mpPorts["8025/tcp"]), MailHost: mpHost, MailPort: atoi(mpPorts["1025/tcp"]),
		Cancel: cancel, valkey: vkc, mailpit: mpc, authBin: authBin, authCfg: authCfg, logs: logs}
	t.Cleanup(func() {
		if t.Failed() {
			for name, p := range logs {
				b, _ := os.ReadFile(p)
				if len(b) > 8000 {
					b = b[len(b)-8000:]
				}
				t.Logf("%s log tail:\n%s", name, b)
			}
		}
	})
	env.waitReady()
	return env
}

// StartPlatform boots the stack and signs the bootstrap operator in.
func StartPlatform(t *testing.T) *Env {
	t.Helper()
	e := Start(t)
	tid, accept := e.bootstrapAuth("ops@example.org")
	e.PlatformID = tid
	op := e.NewSession("ops@example.org", "platform")
	op.acceptInvitation(accept, "Ops")
	if code, body := op.SignIn(); code != 200 {
		t.Fatalf("operator sign-in → %d %v", code, body)
	}
	e.Operator = op
	e.SeedGrants()
	op.WaitAuthorized("/api/notification/v1/stats")
	return e
}

// WaitAuthorized polls a protected module route until the platform accepts
// the session there (verifier synced, grants visible); fails after 60 s.
func (s *Session) WaitAuthorized(path string) {
	s.Env.T.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var code int
	var body map[string]any
	for time.Now().Before(deadline) {
		code, body = s.JSON(http.MethodGet, path, nil)
		if code != 401 && code != 403 && code != 503 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.Env.T.Fatalf("%s never authorized %s: %d %v", s.Email, path, code, body)
}

// NewSession creates an anonymous browser for a user of a tenant.
func (e *Env) NewSession(email, tenant string) *Session {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 150 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}}, //nolint:gosec // self-signed edge cert
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	s := &Session{Env: e, Client: client, Email: email, Tenant: tenant}
	e.sessions = append(e.sessions, s)
	return s
}

// SignIn signs the session in at the gateway edge (the auth module answers).
func (s *Session) SignIn() (int, map[string]any) {
	s.Env.T.Helper()
	s.prime()
	code, body := s.JSON(http.MethodPost, "/api/v1/signin", map[string]string{"tenant": s.Tenant, "email": s.Email, "password": password})
	if code == 200 {
		if _, me := s.JSON(http.MethodGet, "/gateway/v1/me", nil); me["user_id"] != nil {
			s.UserID, _ = me["user_id"].(string)
		}
	}
	return code, body
}

// prime fetches the CSRF cookie.
func (s *Session) prime() {
	resp, err := s.Client.Get(s.Env.Base + "/gateway/v1/me")
	if err != nil {
		s.Env.T.Fatal(err)
	}
	_ = resp.Body.Close()
}

func (s *Session) csrf() string {
	u, _ := url.Parse(s.Env.Base)
	for _, c := range s.Client.Jar.Cookies(u) {
		if c.Name == edge.CSRFCookie {
			return c.Value
		}
	}
	return ""
}

// Raw performs a request with an arbitrary body and returns the response.
func (s *Session) Raw(method, path string, body []byte, contentType string, hdr ...string) *http.Response {
	s.Env.T.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, s.Env.Base+path, rd)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if method != http.MethodGet {
		if s.csrf() == "" {
			s.prime()
		}
		req.Header.Set(edge.CSRFHeader, s.csrf())
		req.Header.Set("Origin", s.Env.Base)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		s.Env.T.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// JSON performs a browser-style JSON request and decodes an object body.
func (s *Session) JSON(method, path string, body any, hdr ...string) (int, map[string]any) {
	s.Env.T.Helper()
	var raw []byte
	ct := ""
	if body != nil {
		raw, _ = json.Marshal(body)
		ct = "application/json"
	}
	resp := s.Raw(method, path, raw, ct, hdr...)
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// JSONList is JSON for endpoints returning an array.
func (s *Session) JSONList(method, path string, body any) (int, []map[string]any) {
	s.Env.T.Helper()
	var raw []byte
	ct := ""
	if body != nil {
		raw, _ = json.Marshal(body)
		ct = "application/json"
	}
	resp := s.Raw(method, path, raw, ct)
	defer resp.Body.Close()
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// Token mints a platform access token from the live session.
func (s *Session) Token() string {
	s.Env.T.Helper()
	code, body := s.JSON(http.MethodPost, "/api/v1/session/token", nil)
	if code != 200 {
		s.Env.T.Fatalf("token → %d %v", code, body)
	}
	return body["access_token"].(string)
}

// acceptInvitation completes an invitation link (from mail or bootstrap).
func (s *Session) acceptInvitation(acceptURL, displayName string) {
	s.Env.T.Helper()
	u, err := url.Parse(acceptURL)
	if err != nil {
		s.Env.T.Fatal(err)
	}
	s.prime()
	if code, body := s.JSON(http.MethodPost, "/api/v1/invitations/accept", map[string]string{"token": u.Query().Get("token"), "display_name": displayName, "password": password}); code/100 != 2 {
		s.Env.T.Fatalf("accept invitation → %d %v", code, body)
	}
}

var acceptLinkRE = regexp.MustCompile(`https://\S+/console/invite/accept\?token=[A-Za-z0-9_-]+`)

// CreateTenant has the operator create a customer tenant and returns its
// signed-in owner and the tenant id.
func (e *Env) CreateTenant(slug, ownerEmail string) (*Session, string) {
	e.T.Helper()
	code, body := e.Operator.JSON(http.MethodPost, "/api/v1/operator/tenants", map[string]string{"slug": slug, "display_name": strings.ToUpper(slug[:1]) + slug[1:], "owner_email": ownerEmail})
	if code != 201 {
		e.T.Fatalf("create tenant → %d %v", code, body)
	}
	var tid string
	if tv, ok := body["tenant"].(map[string]any); ok {
		tid, _ = tv["id"].(string)
	}
	if tid == "" {
		e.T.Fatalf("create tenant: no tenant id in %v", body)
	}
	link := acceptLinkRE.FindString(e.LastMail(ownerEmail))
	if link == "" {
		e.T.Fatalf("no invitation link mailed to %s", ownerEmail)
	}
	owner := e.NewSession(ownerEmail, slug)
	owner.acceptInvitation(link, "Owner")
	if code, body := owner.SignIn(); code != 200 {
		e.T.Fatalf("owner sign-in → %d %v", code, body)
	}
	e.SeedGrants()
	owner.WaitAuthorized("/api/notification/v1/stats")
	return owner, tid
}

// Invite has admin invite a member with the given role slugs into admin's
// tenant and returns the member's signed-in session.
func (e *Env) Invite(admin *Session, email string, roles ...string) *Session {
	e.T.Helper()
	code, list := admin.JSONList(http.MethodGet, "/api/v1/admin/roles", nil)
	if code != 200 {
		e.T.Fatalf("list roles → %d", code)
	}
	var ids []string
	for _, r := range list {
		for _, want := range roles {
			if r["slug"] == want {
				ids = append(ids, r["id"].(string))
			}
		}
	}
	if len(ids) != len(roles) {
		e.T.Fatalf("roles %v not all found in %v", roles, list)
	}
	if code, body := admin.JSON(http.MethodPost, "/api/v1/admin/invitations", map[string]any{"email": email, "role_ids": ids}); code != 202 {
		e.T.Fatalf("invite → %d %v", code, body)
	}
	link := acceptLinkRE.FindString(e.LastMail(email))
	if link == "" {
		e.T.Fatalf("no invitation link mailed to %s", email)
	}
	s := e.NewSession(email, admin.Tenant)
	s.acceptInvitation(link, strings.Split(email, "@")[0])
	if code, body := s.SignIn(); code != 200 {
		e.T.Fatalf("member sign-in → %d %v", code, body)
	}
	s.WaitAuthorized("/api/notification/v1/inbox")
	return s
}

// SeedGrants registers the module's permissions and built-in role grants now
// (the service also does it periodically).
func (e *Env) SeedGrants() {
	e.T.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := e.Notif.SeedPermissions(context.Background())
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			e.T.Fatalf("seed permissions: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// LastMail returns the text of the newest message to an address (waits up to 20 s).
func (e *Env) LastMail(to string) string {
	e.T.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(e.Mail + "/api/v1/search?query=" + url.QueryEscape("to:"+to))
		if err == nil {
			var list struct {
				Messages []struct{ ID string }
			}
			_ = json.NewDecoder(resp.Body).Decode(&list)
			_ = resp.Body.Close()
			if len(list.Messages) > 0 {
				r2, err := http.Get(e.Mail + "/api/v1/message/" + list.Messages[0].ID)
				if err == nil {
					var m struct{ Text string }
					_ = json.NewDecoder(r2.Body).Decode(&m)
					_ = r2.Body.Close()
					return m.Text
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	e.T.Fatalf("no mail for %s", to)
	return ""
}

// MailCount counts messages to an address right now.
func (e *Env) MailCount(to string) int {
	resp, err := http.Get(e.Mail + "/api/v1/search?query=" + url.QueryEscape("to:"+to))
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var list struct {
		Messages []struct{ ID string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	return len(list.Messages)
}

// AuditCount counts module audit events (flushing the writer first).
func (e *Env) AuditCount(tenantID, eventType, outcome string) int {
	e.T.Helper()
	e.Notif.Audit.Flush()
	var n int
	_ = e.Notif.Store.Tx(context.Background(), store.Scope{System: true}, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM notification_audit_events WHERE tenant_id = $1 AND ($2 = '' OR event_type = $2) AND ($3 = '' OR outcome = $3)", tenantID, eventType, outcome).Scan(&n)
	})
	return n
}

// AuditRows returns the audit rows of a type, newest first.
func (e *Env) AuditRows(tenantID, eventType string) []store.AuditRow {
	e.T.Helper()
	e.Notif.Audit.Flush()
	var rows []store.AuditRow
	_ = e.Notif.Store.Tx(context.Background(), store.Scope{System: true}, func(tx pgx.Tx) error {
		var err error
		rows, err = store.QueryAudit(context.Background(), tx, tenantID, eventType, "", time.Time{}, time.Now().Add(time.Hour), time.Time{}, 200)
		return err
	})
	return rows
}

// Pause / Unpause freeze a dependency container (Valkey or Mailpit): requests
// hang until the client timeout and the module fails closed.
func (e *Env) pause(c testcontainers.Container, on bool) {
	e.T.Helper()
	cli, err := testcontainers.NewDockerClientWithOpts(context.Background())
	if err != nil {
		e.T.Fatal(err)
	}
	defer cli.Close()
	if on {
		_, err = cli.ContainerPause(context.Background(), c.GetContainerID(), client.ContainerPauseOptions{})
	} else {
		_, err = cli.ContainerUnpause(context.Background(), c.GetContainerID(), client.ContainerUnpauseOptions{})
	}
	if err != nil {
		e.T.Fatal(err)
	}
}

// ValkeyStop / ValkeyStart pause and resume the Valkey container.
func (e *Env) ValkeyStop()  { e.pause(e.valkey, true) }
func (e *Env) ValkeyStart() { e.pause(e.valkey, false) }

// MailStop / MailStart pause and resume Mailpit (SMTP deliveries fail).
func (e *Env) MailStop()  { e.pause(e.mailpit, true) }
func (e *Env) MailStart() { e.pause(e.mailpit, false) }

// LogContains reports whether a service log mentions s.
func (e *Env) LogContains(service, s string) bool {
	b, _ := os.ReadFile(e.logs[service])
	return strings.Contains(string(b), s)
}

func (e *Env) waitReady() {
	e.T.Helper()
	anon := e.NewSession("", "")
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if e.Notif.Freya.Ready() {
			// Auth reached through the gateway (401), the module registered (401
			// on a protected route rather than 404), and the verifier synced.
			if r1, err := anon.Client.Get(e.Base + "/api/v1/session"); err == nil {
				_ = r1.Body.Close()
				if r2, err := anon.Client.Get(e.Base + "/api/notification/v1/stats"); err == nil {
					_ = r2.Body.Close()
					if r1.StatusCode == 401 && r2.StatusCode == 401 {
						return
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	e.T.Fatalf("platform did not become ready")
}

// bootstrapAuth runs `authsvc bootstrap` and returns the platform tenant id
// and the operator invitation accept URL.
func (e *Env) bootstrapAuth(operatorEmail string) (tenantID, acceptURL string) {
	e.T.Helper()
	out, err := exec.Command(e.authBin, "bootstrap", "-config", e.authCfg, "-operator-email", operatorEmail).CombinedOutput()
	if err != nil {
		e.T.Fatalf("auth bootstrap: %v: %s", err, out)
	}
	var res struct {
		TenantID  string `json:"tenant_id"`
		AcceptURL string `json:"accept_url"`
	}
	if i := bytes.LastIndex(out, []byte("\n{\n")); i >= 0 {
		out = out[i+1:]
	}
	if err := json.Unmarshal(out, &res); err != nil {
		e.T.Fatalf("bootstrap output %q: %v", out, err)
	}
	return res.TenantID, res.AcceptURL
}

// ---- containers and processes

func container(t *testing.T, req testcontainers.ContainerRequest) (host string, ports map[string]string) {
	t.Helper()
	c := startContainer(t, req)
	ctx := context.Background()
	host, _ = c.Host(ctx)
	ports = map[string]string{}
	for _, p := range req.ExposedPorts {
		mp, err := c.MappedPort(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		ports[p] = mp.Port()
	}
	return host, ports
}

func startContainer(t *testing.T, req testcontainers.ContainerRequest) testcontainers.Container {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Skipf("testcontainers unavailable (%s): %v", req.Image, err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	return c
}

func selfSigned(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "valkey"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, DNSNames: []string{"localhost"}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	certPath, keyPath = filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	_ = os.WriteFile(certPath, pemBlock("CERTIFICATE", der), 0o644)
	_ = os.WriteFile(keyPath, pemBlock("EC PRIVATE KEY", kb), 0o644)
	return
}

func pemBlock(typ string, der []byte) []byte {
	b64 := base64.StdEncoding.EncodeToString(der)
	var buf bytes.Buffer
	buf.WriteString("-----BEGIN " + typ + "-----\n")
	for len(b64) > 64 {
		buf.WriteString(b64[:64] + "\n")
		b64 = b64[64:]
	}
	buf.WriteString(b64 + "\n-----END " + typ + "-----\n")
	return buf.Bytes()
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

var (
	buildMu   sync.Mutex
	built     = map[string]string{}
	buildErrs = map[string]error{}
)

// buildService compiles a sibling service once per test binary.
func buildService(t *testing.T, name, pkg string) string {
	t.Helper()
	buildMu.Lock()
	defer buildMu.Unlock()
	if p, ok := built[name]; ok {
		return p
	}
	if err, ok := buildErrs[name]; ok {
		t.Fatal(err)
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("%ssvc-notification-%d", name, os.Getpid()))
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = abs(t, "../../../"+name)
	if b, err := cmd.CombinedOutput(); err != nil {
		buildErrs[name] = fmt.Errorf("build %s: %v\n%s", name, err, b)
		t.Fatal(buildErrs[name])
	}
	built[name] = out
	return out
}

func abs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// startProcess runs a service binary; it is stopped with the test.
func startProcess(t *testing.T, bin, cfg, logPath string) *exec.Cmd {
	t.Helper()
	logf, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-config", cfg)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
		_ = logf.Close()
	})
	return cmd
}

// TestHarnessBoots proves the platform comes up: the operator holds every
// module permission through the owner role, an anonymous browser is refused,
// a member lacks stats:read but reaches the inbox, and health is green.
func TestHarnessBoots(t *testing.T) {
	e := StartPlatform(t)
	code, body := e.Operator.JSON(http.MethodGet, "/api/notification/v1/stats", nil)
	if code != 200 {
		t.Fatalf("operator stats → %d %v", code, body)
	}
	anon := e.NewSession("", "")
	if code, _ := anon.JSON(http.MethodGet, "/api/notification/v1/stats", nil); code != 401 {
		t.Fatalf("anonymous → %d", code)
	}
	if tok := e.Operator.Token(); len(tok) < 40 {
		t.Fatal("token")
	}
	owner, tid := e.CreateTenant("acme", "owner@acme.test")
	if code, _ := owner.JSON(http.MethodGet, "/api/notification/v1/stats", nil); code != 200 {
		t.Fatalf("owner stats → %d", code)
	}
	member := e.Invite(owner, "bob@acme.test", "member")
	if code, _ := member.JSON(http.MethodGet, "/api/notification/v1/stats", nil); code != 403 {
		t.Fatalf("member stats → %d (want 403)", code)
	}
	if code, _ := member.JSON(http.MethodGet, "/api/notification/v1/inbox", nil); code != 200 {
		t.Fatalf("member inbox → %d", code)
	}
	if code, _ := member.JSON(http.MethodPost, "/api/notification/v1/channels", map[string]any{"name": "x", "type": "email", "settings": map[string]any{}}); code != 403 {
		t.Fatalf("member create channel → %d (want 403)", code)
	}
	if tid == "" || e.AuditCount(tid, "", "") != 0 {
		t.Fatalf("tenant %q audit %d", tid, e.AuditCount(tid, "", ""))
	}
	code, health := owner.JSON(http.MethodGet, "/api/notification/v1/health", nil)
	if code != 200 || health["status"] != "ok" || health["database"] != "ok" || health["valkey"] != "ok" {
		t.Fatalf("health %d %v", code, health)
	}
}

func getenv(k string) string { return os.Getenv(k) }
