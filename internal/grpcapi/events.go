package grpcapi

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stream"
)

// EventsServer implements notification.v1.Events.
type EventsServer struct {
	notificationv1.UnimplementedEventsServer
	Hub   *stream.Hub
	Audit *audit.Writer
}

// Publish pushes one live event to users of the tenant.
func (s *EventsServer) Publish(ctx context.Context, req *notificationv1.PublishRequest) (*notificationv1.PublishResponse, error) {
	subj, err := caller(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	ids := req.GetUserIds()
	if !req.GetAll() {
		for _, u := range ids {
			if !uuidRE.MatchString(u) {
				return nil, status.Error(codes.InvalidArgument, "user ids must be uuids")
			}
		}
	} else {
		ids = nil
	}
	var data any
	if len(req.GetData()) > 0 {
		data = req.GetData()
	}
	id, err := s.Hub.PublishID(ctx, subj.TenantID, ids, req.GetAll(), req.GetType(), data, true)
	if err != nil {
		switch {
		case errors.Is(err, stream.ErrType), errors.Is(err, stream.ErrReserved), errors.Is(err, stream.ErrTargets), errors.Is(err, stream.ErrPayload):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Unavailable, "temporarily_unavailable")
	}
	if s.Audit != nil {
		_ = s.Audit.Emit(audit.Event{Type: audit.EventPublished, TenantID: subj.TenantID, ActorKind: "service", ActorID: subj.Service, SubjectKind: "system", SubjectID: id, Outcome: "ok",
			Details: map[string]any{"type": req.GetType(), "all": req.GetAll(), "users": len(ids)}})
	}
	return &notificationv1.PublishResponse{EventId: id}, nil
}
