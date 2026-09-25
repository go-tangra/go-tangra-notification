// Package grpcapi serves notification.v1 for other platform services on the
// Freya channel: the caller is an authenticated service (mTLS, policed by
// policy.yaml) acting for the tenant named in the request; every call is
// audited with the service identity.
package grpcapi

import (
	"context"
	"errors"
	"regexp"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/channel"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
	"github.com/go-tangra/go-tangra/v4/authn"
)

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// callerFunc resolves the service identity of a call (overridable in tests).
var callerFunc = func(ctx context.Context) (string, bool) {
	p, ok := authn.FromContext(ctx)
	if !ok {
		return "", false
	}
	return p.ID.String(), true
}

// caller returns the service subjects for the tenant named in the request.
func caller(ctx context.Context, tenantID string) (authz.Subjects, error) {
	id, ok := callerFunc(ctx)
	if !ok {
		return authz.Subjects{}, status.Error(codes.Unauthenticated, "service identity required")
	}
	if !uuidRE.MatchString(tenantID) {
		return authz.Subjects{}, status.Error(codes.InvalidArgument, "tenant_id must be a uuid")
	}
	return authz.ServiceSubjects(tenantID, id), nil
}

// NotifierServer implements notification.v1.Notifier.
type NotifierServer struct {
	notificationv1.UnimplementedNotifierServer
	Sender *notify.Sender
	Audit  *audit.Writer
}

func grpcError(err error) error {
	var ve *notify.ValidationError
	switch {
	case errors.As(err, &ve):
		return status.Error(codes.InvalidArgument, ve.Msg)
	case errors.Is(err, authz.ErrForbidden):
		return status.Error(codes.PermissionDenied, "forbidden")
	case errors.Is(err, authz.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return status.Error(codes.NotFound, "not_found")
	case errors.Is(err, notify.ErrChannelDisabled):
		return status.Error(codes.FailedPrecondition, "channel_disabled")
	case errors.Is(err, notify.ErrTypeMismatch):
		return status.Error(codes.InvalidArgument, "channel type differs from the template")
	case errors.Is(err, channel.ErrNoProvider):
		return status.Error(codes.InvalidArgument, "no_provider")
	case errors.Is(err, notify.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, "rate_limited")
	}
	return status.Error(codes.Unavailable, "temporarily_unavailable")
}

func toResponse(v notify.LogView) *notificationv1.SendResponse {
	out := &notificationv1.SendResponse{LogId: v.ID, Error: v.Error}
	switch v.Status {
	case "sent":
		out.Status = notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT
	case "failed":
		out.Status = notificationv1.DeliveryStatus_DELIVERY_STATUS_FAILED
		out.Retryable = v.Retryable
	default:
		out.Status = notificationv1.DeliveryStatus_DELIVERY_STATUS_PENDING
	}
	if v.SentAt != nil {
		out.SentAt = timestamppb.New(*v.SentAt)
	}
	return out
}

// Send renders and delivers for the tenant: by template_id (tenant-wide use
// required) or by template_key (a system template of the caller's own
// namespace, feature 017). Exactly one of the two is accepted.
func (s *NotifierServer) Send(ctx context.Context, req *notificationv1.SendRequest) (*notificationv1.SendResponse, error) {
	if (req.GetTemplateId() == "") == (req.GetTemplateKey() == "") {
		return nil, status.Error(codes.InvalidArgument, "template_ref")
	}
	if req.GetTemplateKey() != "" {
		return s.sendKey(ctx, req)
	}
	subj, err := caller(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	if !uuidRE.MatchString(req.GetTemplateId()) || (req.GetChannelId() != "" && !uuidRE.MatchString(req.GetChannelId())) || req.GetRecipient() == "" || len(req.GetRecipient()) > 512 {
		return nil, status.Error(codes.InvalidArgument, "malformed request")
	}
	v, err := s.Sender.Send(ctx, subj, notify.SendInput{TemplateID: req.GetTemplateId(), ChannelID: req.GetChannelId(), Recipient: req.GetRecipient(), Variables: req.GetVariables(), CorrelationID: req.GetCorrelationId()})
	if err != nil {
		return nil, grpcError(err)
	}
	if v.Status == "failed" && v.Error == "no_provider" {
		return nil, status.Error(codes.InvalidArgument, "no_provider")
	}
	return toResponse(v), nil
}

// sendKey sends a system template (contracts notification-grpc.md): no
// channel override, the key namespace is the service name of the verified
// caller identity (spiffe://<td>/svc/<name>); refusals are permanent except
// throttling.
func (s *NotifierServer) sendKey(ctx context.Context, req *notificationv1.SendRequest) (*notificationv1.SendResponse, error) {
	if req.GetChannelId() != "" {
		return nil, status.Error(codes.InvalidArgument, "channel_override")
	}
	if !notify.ValidKey(req.GetTemplateKey()) {
		return nil, status.Error(codes.InvalidArgument, "template_key")
	}
	subj, err := caller(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	if req.GetRecipient() == "" || len(req.GetRecipient()) > 512 {
		return nil, status.Error(codes.InvalidArgument, "malformed request")
	}
	service, _ := notify.ServiceFromSPIFFE(subj.Service) // "" refuses every key
	v, err := s.Sender.SendKey(ctx, subj, service, notify.KeyInput{Key: req.GetTemplateKey(), Recipient: req.GetRecipient(), Variables: req.GetVariables(), CorrelationID: req.GetCorrelationId()})
	switch {
	case errors.Is(err, notify.ErrKeyNamespace):
		return nil, status.Error(codes.PermissionDenied, "key_namespace")
	case errors.Is(err, notify.ErrUnknownKey):
		return nil, status.Error(codes.NotFound, "template_key")
	case errors.Is(err, notify.ErrEmailNotConfigured):
		return nil, status.Error(codes.FailedPrecondition, "email_not_configured")
	case errors.Is(err, notify.ErrRateLimited):
		return nil, status.Error(codes.ResourceExhausted, "throttled")
	case err != nil:
		return nil, grpcError(err)
	}
	return toResponse(v), nil
}

// SendTest delivers the built-in test message (write on the channel via a tenant grant).
func (s *NotifierServer) SendTest(ctx context.Context, req *notificationv1.SendTestRequest) (*notificationv1.SendResponse, error) {
	subj, err := caller(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	if !uuidRE.MatchString(req.GetChannelId()) || req.GetRecipient() == "" || len(req.GetRecipient()) > 512 {
		return nil, status.Error(codes.InvalidArgument, "malformed request")
	}
	v, err := s.Sender.SendTest(ctx, subj, req.GetChannelId(), req.GetRecipient(), "")
	if err != nil {
		return nil, grpcError(err)
	}
	if v.Status == "failed" && v.Error == "no_provider" {
		return nil, status.Error(codes.InvalidArgument, "no_provider")
	}
	return toResponse(v), nil
}
