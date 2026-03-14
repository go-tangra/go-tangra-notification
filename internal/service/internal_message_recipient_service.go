package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/data"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type InternalMessageRecipientService struct {
	notificationpb.UnimplementedInternalMessageRecipientServiceServer

	log *log.Helper

	internalMessageRepo          *data.InternalMessageRepo
	internalMessageRecipientRepo *data.InternalMessageRecipientRepo
}

func NewInternalMessageRecipientService(
	ctx *bootstrap.Context,
	internalMessageRepo *data.InternalMessageRepo,
	internalMessageRecipientRepo *data.InternalMessageRecipientRepo,
) *InternalMessageRecipientService {
	return &InternalMessageRecipientService{
		log:                          ctx.NewLoggerHelper("internal-message-recipient/service/notification-service"),
		internalMessageRepo:          internalMessageRepo,
		internalMessageRecipientRepo: internalMessageRecipientRepo,
	}
}

// ListUserInbox fetches user notifications with message content
func (s *InternalMessageRecipientService) ListUserInbox(ctx context.Context, req *paginationV1.PagingRequest) (*notificationpb.ListUserInboxResponse, error) {
	resp, err := s.internalMessageRecipientRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	for _, d := range resp.Items {
		if d.MessageId == nil {
			continue
		}

		msg, err := s.internalMessageRepo.Get(ctx, &notificationpb.GetInternalMessageRequest{
			QueryBy: &notificationpb.GetInternalMessageRequest_Id{
				Id: d.GetMessageId(),
			},
		})
		if err != nil {
			s.log.Errorf("list user inbox failed, get message failed: %s", err)
			continue
		}

		d.Title = msg.Title
		d.Content = msg.Content
	}

	return resp, nil
}

func (s *InternalMessageRecipientService) DeleteNotificationFromInbox(ctx context.Context, req *notificationpb.DeleteNotificationFromInboxRequest) (*emptypb.Empty, error) {
	err := s.internalMessageRecipientRepo.DeleteNotificationFromInbox(ctx, req)
	return &emptypb.Empty{}, err
}

// MarkNotificationAsRead marks notifications as read
func (s *InternalMessageRecipientService) MarkNotificationAsRead(ctx context.Context, req *notificationpb.MarkNotificationAsReadRequest) (*emptypb.Empty, error) {
	err := s.internalMessageRecipientRepo.MarkNotificationAsRead(ctx, req)
	return &emptypb.Empty{}, err
}

// MarkNotificationsStatus marks notifications to a specific status
func (s *InternalMessageRecipientService) MarkNotificationsStatus(ctx context.Context, req *notificationpb.MarkNotificationsStatusRequest) (*emptypb.Empty, error) {
	err := s.internalMessageRecipientRepo.MarkNotificationsStatus(ctx, req)
	return &emptypb.Empty{}, err
}
