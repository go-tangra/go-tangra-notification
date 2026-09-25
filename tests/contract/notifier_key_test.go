package contract

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
	"github.com/go-tangra/go-tangra-notification/v4/internal/grpcapi"
	"github.com/go-tangra/go-tangra/v4/authn"
	"github.com/go-tangra/go-tangra/v4/identity"
)

// TestNotifierKeyFields proves the additive 017 fields (contracts
// notification-grpc.md): SendRequest.template_key = 7 (string) and
// SendResponse.retryable = 5 (bool); the existing field numbers are kept.
func TestNotifierKeyFields(t *testing.T) {
	msgs := notificationv1.File_notification_v1_notification_proto.Messages()
	for _, tc := range []struct {
		msg, field string
		num        protoreflect.FieldNumber
		kind       protoreflect.Kind
	}{
		{"SendRequest", "tenant_id", 1, protoreflect.StringKind},
		{"SendRequest", "template_id", 2, protoreflect.StringKind},
		{"SendRequest", "channel_id", 3, protoreflect.StringKind},
		{"SendRequest", "recipient", 4, protoreflect.StringKind},
		{"SendRequest", "correlation_id", 6, protoreflect.StringKind},
		{"SendRequest", "template_key", 7, protoreflect.StringKind},
		{"SendResponse", "log_id", 1, protoreflect.StringKind},
		{"SendResponse", "error", 3, protoreflect.StringKind},
		{"SendResponse", "retryable", 5, protoreflect.BoolKind},
	} {
		f := msgs.ByName(protoName(tc.msg)).Fields().ByName(protoName(tc.field))
		if f == nil || f.Number() != tc.num || f.Kind() != tc.kind {
			t.Errorf("%s.%s: %v", tc.msg, tc.field, f)
		}
	}
}

// TestNotifierTemplateRef proves exactly one of template_id / template_key
// is accepted, a key send takes no channel override and a malformed key is
// refused, all before anything is looked up.
func TestNotifierTemplateRef(t *testing.T) {
	id, err := identity.ParseSPIFFEID("spiffe://example.org/svc/auth")
	if err != nil {
		t.Fatal(err)
	}
	ctx := authn.WithPeer(context.Background(), authn.PeerIdentity{ID: id, ServiceName: "auth"})
	srv := &grpcapi.NotifierServer{}
	const tenant = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	for name, tc := range map[string]struct {
		req  *notificationv1.SendRequest
		want string
	}{
		"neither":  {&notificationv1.SendRequest{TenantId: tenant, Recipient: "a@b.c"}, "template_ref"},
		"both":     {&notificationv1.SendRequest{TenantId: tenant, TemplateId: tenant, TemplateKey: "auth.invite", Recipient: "a@b.c"}, "template_ref"},
		"override": {&notificationv1.SendRequest{TenantId: tenant, TemplateKey: "auth.invite", ChannelId: tenant, Recipient: "a@b.c"}, "channel_override"},
		"bad key":  {&notificationv1.SendRequest{TenantId: tenant, TemplateKey: "Auth.Invite", Recipient: "a@b.c"}, "template_key"},
		"no dot":   {&notificationv1.SendRequest{TenantId: tenant, TemplateKey: "authinvite", Recipient: "a@b.c"}, "template_key"},
	} {
		_, err := srv.Send(ctx, tc.req)
		if status.Code(err) != codes.InvalidArgument || status.Convert(err).Message() != tc.want {
			t.Errorf("%s: %v", name, err)
		}
	}
}
