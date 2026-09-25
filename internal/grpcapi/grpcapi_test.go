package grpcapi

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/memstore"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stream"
	"github.com/go-tangra/go-tangra-notification/sdk/v4/pkg/notifyclient"
	"github.com/go-tangra/go-tangra/v4/authn"
	"github.com/go-tangra/go-tangra/v4/identity"
)

const (
	tA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	uA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c77"
)

type fakeProvider struct {
	sent []channel.Message
	fail error
}

func (f *fakeProvider) Type() string                   { return "email" }
func (f *fakeProvider) Validate(sealed.Settings) error { return nil }
func (f *fakeProvider) Secret() []string               { return []string{"password"} }
func (f *fakeProvider) Send(_ context.Context, _ sealed.Settings, m channel.Message) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, m)
	return nil
}

type fakeLimiter struct{ n int }

func (l *fakeLimiter) Limited(context.Context, string, string, int, time.Time) (bool, error) {
	l.n++
	return l.n > 100, nil
}

// peerInterceptor stamps the calling service identity (the Freya authn middleware in production).
func peerInterceptor(spiffe string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		if spiffe == "" {
			return h(ctx, req)
		}
		id, _ := identity.ParseSPIFFEID(spiffe)
		return h(authn.WithPeer(ctx, authn.PeerIdentity{ID: id, ServiceName: "warden"}), req)
	}
}

type fx struct {
	ms     *memstore.Store
	aw     *audit.Writer
	email  *fakeProvider
	hub    *stream.Hub
	ch     *notify.Channels
	tp     *notify.Templates
	client *notifyclient.Client
	raw    *grpc.ClientConn
}

func newFx(t *testing.T, spiffe string) *fx {
	t.Helper()
	ms := memstore.New()
	aw := audit.NewWriter(ms, nil)
	t.Cleanup(aw.Close)
	az := authz.New(ms, aw)
	env, _ := sealed.NewEnvelope(bytes.Repeat([]byte{3}, 32))
	email := &fakeProvider{}
	reg := channel.NewRegistry(email, channel.Nop{Kind: "sms"})
	ch := notify.NewChannels(ms, env, reg, az, aw)
	tp := notify.NewTemplates(ms, az, aw)
	snd := notify.NewSender(ms, ch, tp, az, aw, &fakeLimiter{}, notify.Limits{PerTenant: 100, PerSender: 100})
	hub := stream.NewHub(stream.NewMemory(), stream.Config{}, nil)
	t.Cleanup(hub.Close)
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnaryInterceptor(peerInterceptor(spiffe)))
	notificationv1.RegisterNotifierServer(srv, &NotifierServer{Sender: snd, Audit: aw})
	notificationv1.RegisterEventsServer(srv, &EventsServer{Hub: hub, Audit: aw})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufconn", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &fx{ms: ms, aw: aw, email: email, hub: hub, ch: ch, tp: tp, client: notifyclient.New(conn), raw: conn}
}

func admin() authz.Subjects {
	return authz.Subjects{TenantID: tA, UserID: uA, Roles: []string{"admin"}}
}

func (f *fx) seed(t *testing.T) (channelID, templateID string) {
	t.Helper()
	ctx := context.Background()
	c, err := f.ch.Create(ctx, admin(), notify.ChannelInput{Name: "relay", Type: "email", Settings: sealed.Settings{"host": "h", "port": float64(25), "from": "a@b.c", "password": "NOTIF-MARKER-PW-x"}, Enabled: true, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	tp, err := f.tp.Create(ctx, admin(), notify.TemplateInput{Name: "welcome", ChannelID: c.ID, Subject: "Hi {{.Name}}", Body: "<p>{{.Name}}</p>", Variables: []string{"Name"}})
	if err != nil {
		t.Fatal(err)
	}
	// Services send through tenant-wide use: grant the tenant sharer on the template.
	if _, err := az(f).Grant(ctx, admin(), authz.GrantInput{ResourceType: "template", ResourceID: tp.ID, SubjectType: "tenant", Relation: "sharer"}); err != nil {
		t.Fatal(err)
	}
	return c.ID, tp.ID
}

func az(f *fx) *authz.Authz { return authz.New(f.ms, f.aw) }

func code(err error) codes.Code { return status.Code(err) }

func TestNotifierSend(t *testing.T) {
	f := newFx(t, "spiffe://example.org/svc/warden")
	cid, tid := f.seed(t)
	ctx := context.Background()
	res, err := f.client.Send(ctx, tA, tid, "ana@example.org", map[string]string{"Name": "Ana"})
	if err != nil || res.GetStatus() != notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT || res.GetLogId() == "" || res.GetSentAt() == nil || len(f.email.sent) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	f.aw.Flush()
	logs := f.ms.AuditEvents(tA, "notification_sent")
	if len(logs) != 1 || logs[0].ActorKind != "service" || logs[0].ActorID != "spiffe://example.org/svc/warden" {
		t.Fatalf("audit %+v", logs)
	}
	// Provider failure → FAILED with a scrubbed error.
	f.email.fail = errors.New("relay refused NOTIF-MARKER-PW-x")
	res, err = f.client.Send(ctx, tA, tid, "ana@example.org", map[string]string{"Name": "Ana"})
	if err != nil || res.GetStatus() != notificationv1.DeliveryStatus_DELIVERY_STATUS_FAILED || strings.Contains(res.GetError(), "NOTIF-MARKER") {
		t.Fatalf("failed: %v %+v", err, res)
	}
	f.email.fail = nil
	// Malformed requests and domain errors map to codes.
	for name, tc := range map[string]struct {
		tenant, tmpl, rcpt string
		vars               map[string]string
		want               codes.Code
	}{
		"tenant":    {"nope", tid, "a@b.c", nil, codes.InvalidArgument},
		"template":  {tA, "nope", "a@b.c", nil, codes.InvalidArgument},
		"recipient": {tA, tid, "", nil, codes.InvalidArgument},
		"unknown":   {tA, uA, "a@b.c", nil, codes.NotFound},
		"variables": {tA, tid, "a@b.c", nil, codes.InvalidArgument},
		"bad rcpt":  {tA, tid, "not-an-address", map[string]string{"Name": "x"}, codes.InvalidArgument},
		"long rcpt": {tA, tid, strings.Repeat("a", 513), nil, codes.InvalidArgument},
		"other tnt": {"0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c66", tid, "a@b.c", nil, codes.NotFound},
	} {
		if _, err := f.client.Send(ctx, tc.tenant, tc.tmpl, tc.rcpt, tc.vars); code(err) != tc.want {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Disabled channel → FailedPrecondition; type mismatch → InvalidArgument; no provider → InvalidArgument.
	c, _ := f.ch.Get(ctx, admin(), cid)
	if _, err := f.ch.Update(ctx, admin(), cid, notify.ChannelInput{Name: c.Name, Type: "email", Settings: sealed.Settings{"host": "h", "port": float64(25), "from": "a@b.c"}, Enabled: false, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Send(ctx, tA, tid, "a@b.c", map[string]string{"Name": "x"}); code(err) != codes.FailedPrecondition {
		t.Fatalf("disabled: %v", err)
	}
	sms, _ := f.ch.Create(ctx, admin(), notify.ChannelInput{Name: "sms", Type: "sms", Settings: sealed.Settings{}, Enabled: true, IsDefault: true})
	raw := notificationv1.NewNotifierClient(f.raw)
	if _, err := raw.Send(ctx, &notificationv1.SendRequest{TenantId: tA, TemplateId: tid, ChannelId: sms.ID, Recipient: "a@b.c", Variables: map[string]string{"Name": "x"}}); code(err) != codes.InvalidArgument {
		t.Fatalf("mismatch: %v", err)
	}
	if _, err := raw.Send(ctx, &notificationv1.SendRequest{TenantId: tA, TemplateId: tid, ChannelId: "bad", Recipient: "a@b.c"}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad channel id: %v", err)
	}
	smsTpl, _ := f.tp.Create(ctx, admin(), notify.TemplateInput{Name: "sms", ChannelID: sms.ID, Subject: "s", Body: "b"})
	_, _ = az(f).Grant(ctx, admin(), authz.GrantInput{ResourceType: "template", ResourceID: smsTpl.ID, SubjectType: "tenant", Relation: "sharer"})
	if _, err := f.client.Send(ctx, tA, smsTpl.ID, "+1555", nil); code(err) != codes.InvalidArgument || !strings.Contains(err.Error(), "no_provider") {
		t.Fatalf("no provider: %v", err)
	}
	// Store outage → Unavailable.
	f.ms.FailOn("GetTemplate", errors.New("down"))
	if _, err := f.client.Send(ctx, tA, tid, "a@b.c", nil); code(err) != codes.Unavailable {
		t.Fatalf("outage: %v", err)
	}
	f.ms.FailOn("GetTemplate", nil)
	// Without a peer identity every call is Unauthenticated.
	anon := newFx(t, "")
	if _, err := anon.client.Send(ctx, tA, tid, "a@b.c", nil); code(err) != codes.Unauthenticated {
		t.Fatalf("anon: %v", err)
	}
	if _, err := anon.client.SendTest(ctx, tA, cid, "a@b.c"); code(err) != codes.Unauthenticated {
		t.Fatalf("anon test: %v", err)
	}
	if _, err := anon.client.Publish(ctx, tA, nil, true, "x", nil); code(err) != codes.Unauthenticated {
		t.Fatalf("anon publish: %v", err)
	}
}

func TestNotifierSendTest(t *testing.T) {
	f := newFx(t, "spiffe://example.org/svc/warden")
	cid, _ := f.seed(t)
	ctx := context.Background()
	// A service holds tenant sharer (use) on the default channel but not write → PermissionDenied.
	if _, err := f.client.SendTest(ctx, tA, cid, "ops@example.org"); code(err) != codes.PermissionDenied {
		t.Fatalf("no write: %v", err)
	}
	_, _ = az(f).Grant(ctx, admin(), authz.GrantInput{ResourceType: "channel", ResourceID: cid, SubjectType: "tenant", Relation: "editor"})
	res, err := f.client.SendTest(ctx, tA, cid, "ops@example.org")
	if err != nil || res.GetStatus() != notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT || len(f.email.sent) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	for name, tc := range map[string]struct {
		tenant, ch, rcpt string
		want             codes.Code
	}{
		"tenant":  {"x", cid, "a@b.c", codes.InvalidArgument},
		"channel": {tA, "x", "a@b.c", codes.InvalidArgument},
		"rcpt":    {tA, cid, "", codes.InvalidArgument},
		"unknown": {tA, uA, "a@b.c", codes.NotFound},
	} {
		if _, err := f.client.SendTest(ctx, tc.tenant, tc.ch, tc.rcpt); code(err) != tc.want {
			t.Errorf("%s: %v", name, err)
		}
	}
	sms, _ := f.ch.Create(ctx, admin(), notify.ChannelInput{Name: "sms", Type: "sms", Settings: sealed.Settings{}, Enabled: true})
	_, _ = az(f).Grant(ctx, admin(), authz.GrantInput{ResourceType: "channel", ResourceID: sms.ID, SubjectType: "tenant", Relation: "editor"})
	if _, err := f.client.SendTest(ctx, tA, sms.ID, "+1555"); code(err) != codes.InvalidArgument || !strings.Contains(err.Error(), "no_provider") {
		t.Fatalf("no provider: %v", err)
	}
}

func TestEventsPublish(t *testing.T) {
	f := newFx(t, "spiffe://example.org/svc/warden")
	ctx := context.Background()
	sub, _ := f.hub.Subscribe(ctx, tA, uA, "")
	defer sub.Close()
	id, err := f.client.Publish(ctx, tA, []string{uA}, false, "warden.secret", map[string]any{"id": "s1"})
	if err != nil || id == "" {
		t.Fatalf("%v %q", err, id)
	}
	select {
	case ev := <-sub.Events():
		if ev.ID != id || ev.Type != "warden.secret" || !strings.Contains(ev.Data, `"id":"s1"`) {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	if _, err := f.client.Publish(ctx, tA, []string{uA, "x"}, true, "broadcast", nil); err != nil {
		t.Fatalf("all ignores ids: %v", err)
	}
	for name, tc := range map[string]struct {
		tenant string
		users  []string
		all    bool
		typ    string
		data   any
		want   codes.Code
	}{
		"tenant":   {"x", []string{uA}, false, "t", nil, codes.InvalidArgument},
		"user id":  {tA, []string{"x"}, false, "t", nil, codes.InvalidArgument},
		"reserved": {tA, []string{uA}, false, "inbox", nil, codes.InvalidArgument},
		"type":     {tA, []string{uA}, false, "Bad Type", nil, codes.InvalidArgument},
		"targets":  {tA, nil, false, "t", nil, codes.InvalidArgument},
		"payload":  {tA, []string{uA}, false, "t", strings.Repeat("x", 70<<10), codes.InvalidArgument},
	} {
		if _, err := f.client.Publish(ctx, tc.tenant, tc.users, tc.all, tc.typ, tc.data); code(err) != tc.want {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := f.client.Publish(ctx, tA, []string{uA}, false, "t", make(chan int)); err == nil {
		t.Fatal("unmarshalable data")
	}
	f.aw.Flush()
	if n := len(f.ms.AuditEvents(tA, "event_published")); n != 2 {
		t.Fatalf("audit %d", n)
	}
	// Hub outage → Unavailable.
	f.hub.Close()
	closed := stream.NewHub(&downClient{}, stream.Config{}, nil)
	f2 := &EventsServer{Hub: closed}
	if _, err := f2.Publish(authn.WithPeer(ctx, authn.PeerIdentity{ID: mustID("spiffe://example.org/svc/warden")}), &notificationv1.PublishRequest{TenantId: tA, All: true, Type: "t"}); code(err) != codes.Unavailable {
		t.Fatalf("outage: %v", err)
	}
}

func mustID(s string) identity.SPIFFEID {
	id, _ := identity.ParseSPIFFEID(s)
	return id
}

type downClient struct{ stream.Client }

func (downClient) XAdd(context.Context, string, map[string]string, int64) (string, error) {
	return "", errors.New("down")
}
