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

	"github.com/go-freya/freya/authn"
	notificationv1 "github.com/go-freya/freya/services/notification/api/proto/notification/v1"
	"github.com/go-freya/freya/services/notification/internal/audit"
	"github.com/go-freya/freya/services/notification/internal/authz"
	"github.com/go-freya/freya/services/notification/internal/channel"
	"github.com/go-freya/freya/services/notification/internal/notify"
	"github.com/go-freya/freya/services/notification/internal/store"
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
	default:
		out.Status = notificationv1.DeliveryStatus_DELIVERY_STATUS_PENDING
	}
	if v.SentAt != nil {
		out.SentAt = timestamppb.New(*v.SentAt)
	}
	return out
}

// Send renders and delivers for the tenant (tenant-wide use required).
func (s *NotifierServer) Send(ctx context.Context, req *notificationv1.SendRequest) (*notificationv1.SendResponse, error) {
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
