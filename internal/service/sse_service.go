package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"github.com/tx7do/kratos-transport/transport/sse"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/data"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type SSEService struct {
	notificationpb.UnimplementedNotificationSSEServiceServer

	log       *log.Helper
	sseServer *sse.Server
	userToken *data.UserTokenCacheRepo
}

func NewSSEService(
	ctx *bootstrap.Context,
	sseServer *sse.Server,
	userToken *data.UserTokenCacheRepo,
) *SSEService {
	return &SSEService{
		log:       ctx.NewLoggerHelper("sse/service/notification-service"),
		sseServer: sseServer,
		userToken: userToken,
	}
}

func (s *SSEService) PublishEvent(ctx context.Context, req *notificationpb.PublishEventRequest) (*emptypb.Empty, error) {
	if req.GetUserId() == 0 {
		return nil, notificationpb.ErrorBadRequest("user_id is required")
	}

	s.publishToUser(ctx, req.GetUserId(), req.GetEventType(), []byte(req.GetData()))

	return &emptypb.Empty{}, nil
}

func (s *SSEService) PublishBulkEvent(ctx context.Context, req *notificationpb.PublishBulkEventRequest) (*emptypb.Empty, error) {
	if len(req.GetUserIds()) == 0 {
		return nil, notificationpb.ErrorBadRequest("user_ids is required")
	}

	data := []byte(req.GetData())
	for _, userId := range req.GetUserIds() {
		s.publishToUser(ctx, userId, req.GetEventType(), data)
	}

	return &emptypb.Empty{}, nil
}

// publishToUser publishes an SSE event to all active streams for a user.
func (s *SSEService) publishToUser(ctx context.Context, userId uint32, eventType string, eventData []byte) {
	if s.sseServer == nil {
		s.log.Warn("SSE server not available, skipping publish")
		return
	}

	tokens := s.userToken.GetAccessTokens(ctx, userId)
	for _, token := range tokens {
		s.sseServer.Publish(ctx, sse.StreamID(token), &sse.Event{
			ID:    []byte(uuid.New().String()),
			Data:  eventData,
			Event: []byte(eventType),
		})
	}
}
