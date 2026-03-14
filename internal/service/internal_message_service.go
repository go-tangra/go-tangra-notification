package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/timeutil"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"github.com/tx7do/kratos-transport/transport/sse"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/client"
	"github.com/go-tangra/go-tangra-notification/internal/data"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type InternalMessageService struct {
	notificationpb.UnimplementedInternalMessageServiceServer

	log *log.Helper

	internalMessageRepo          *data.InternalMessageRepo
	internalMessageCategoryRepo  *data.InternalMessageCategoryRepo
	internalMessageRecipientRepo *data.InternalMessageRecipientRepo
	adminClient                  *client.AdminClient

	sseServer *sse.Server
	userToken *data.UserTokenCacheRepo
}

func NewInternalMessageService(
	ctx *bootstrap.Context,
	internalMessageRepo *data.InternalMessageRepo,
	internalMessageCategoryRepo *data.InternalMessageCategoryRepo,
	internalMessageRecipientRepo *data.InternalMessageRecipientRepo,
	adminClient *client.AdminClient,
	sseServer *sse.Server,
	userToken *data.UserTokenCacheRepo,
) *InternalMessageService {
	return &InternalMessageService{
		log:                          ctx.NewLoggerHelper("internal-message/service/notification-service"),
		internalMessageRepo:          internalMessageRepo,
		internalMessageCategoryRepo:  internalMessageCategoryRepo,
		internalMessageRecipientRepo: internalMessageRecipientRepo,
		adminClient:                  adminClient,
		sseServer:                    sseServer,
		userToken:                    userToken,
	}
}

func (s *InternalMessageService) ListMessage(ctx context.Context, req *paginationV1.PagingRequest) (*notificationpb.ListInternalMessageResponse, error) {
	resp, err := s.internalMessageRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	categorySet := make(map[uint32]*string)

	for _, v := range resp.Items {
		if v.CategoryId != nil {
			categorySet[v.GetCategoryId()] = nil
		}
	}

	ids := make([]uint32, 0, len(categorySet))
	for id := range categorySet {
		ids = append(ids, id)
	}

	categories, err := s.internalMessageCategoryRepo.ListCategoriesByIds(ctx, ids)
	if err == nil {
		for _, c := range categories {
			categorySet[c.GetId()] = c.Name
		}

		for k, v := range categorySet {
			if v == nil {
				continue
			}
			for i := 0; i < len(resp.Items); i++ {
				if resp.Items[i].CategoryId != nil && resp.Items[i].GetCategoryId() == k {
					resp.Items[i].CategoryName = v
				}
			}
		}
	}

	return resp, nil
}

func (s *InternalMessageService) GetMessage(ctx context.Context, req *notificationpb.GetInternalMessageRequest) (*notificationpb.InternalMessage, error) {
	resp, err := s.internalMessageRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.CategoryId != nil {
		category, err := s.internalMessageCategoryRepo.Get(ctx, &notificationpb.GetInternalMessageCategoryRequest{
			QueryBy: &notificationpb.GetInternalMessageCategoryRequest_Id{Id: resp.GetCategoryId()},
		})
		if err == nil && category != nil {
			resp.CategoryName = category.Name
		} else {
			s.log.Warnf("Get internal message category failed: %v", err)
		}
	}

	return resp, nil
}

func (s *InternalMessageService) CreateMessage(ctx context.Context, req *notificationpb.CreateInternalMessageRequest) (*notificationpb.InternalMessage, error) {
	if req.Data == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	uid := getUserIDAsUint32(ctx)
	if uid != nil {
		req.Data.CreatedBy = uid
	}

	return s.internalMessageRepo.Create(ctx, req)
}

func (s *InternalMessageService) UpdateMessage(ctx context.Context, req *notificationpb.UpdateInternalMessageRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	uid := getUserIDAsUint32(ctx)
	if uid != nil {
		req.Data.UpdatedBy = uid
		if req.UpdateMask != nil {
			req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
		}
	}

	if err := s.internalMessageRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *InternalMessageService) DeleteMessage(ctx context.Context, req *notificationpb.DeleteInternalMessageRequest) (*emptypb.Empty, error) {
	if err := s.internalMessageRepo.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// RevokeMessage revokes a message
func (s *InternalMessageService) RevokeMessage(ctx context.Context, req *notificationpb.RevokeMessageRequest) (*emptypb.Empty, error) {
	if err := s.internalMessageRepo.Delete(ctx, req.GetMessageId()); err != nil {
		s.log.Errorf("delete internal message failed: [%d]", req.GetMessageId())
	}

	if err := s.internalMessageRecipientRepo.RevokeMessage(ctx, req); err != nil {
		s.log.Errorf("delete internal message inbox failed: [%d][%d]", req.GetMessageId(), req.GetUserId())
		return &emptypb.Empty{}, err
	}

	return &emptypb.Empty{}, nil
}

// SendMessage sends a message and publishes SSE notifications
func (s *InternalMessageService) SendMessage(ctx context.Context, req *notificationpb.SendMessageRequest) (*notificationpb.SendMessageResponse, error) {
	uid := getUserIDAsUint32(ctx)
	var senderUserId uint32
	if uid != nil {
		senderUserId = *uid
	}

	now := time.Now()

	msg, err := s.internalMessageRepo.Create(ctx, &notificationpb.CreateInternalMessageRequest{
		Data: &notificationpb.InternalMessage{
			Title:      req.Title,
			Content:    trans.Ptr(req.GetContent()),
			Status:     trans.Ptr(notificationpb.InternalMessage_PUBLISHED),
			Type:       trans.Ptr(req.GetType()),
			CategoryId: req.CategoryId,
			CreatedBy:  trans.Ptr(senderUserId),
			CreatedAt:  timeutil.TimeToTimestamppb(&now),
		},
	})
	if err != nil {
		s.log.Errorf("create internal message failed: %s", err)
		return nil, err
	}

	if req.GetTargetAll() {
		users, err := s.adminClient.ListUsers(ctx)
		if err != nil {
			s.log.Errorf("send message failed, list users failed, %s", err)
		} else {
			s.log.Infof("SendMessage: targetAll=true, ListUsers returned %d users for message %d", len(users.GetItems()), msg.GetId())
			for _, user := range users.Items {
				s.log.Infof("SendMessage: sending notification to user %d (username=%s) for message %d", user.GetId(), user.GetUsername(), msg.GetId())
				if err := s.sendNotification(ctx, msg.GetId(), user.GetId(), senderUserId, &now, msg.GetTitle(), msg.GetContent()); err != nil {
					s.log.Warnf("failed to send notification to user %d: %v", user.GetId(), err)
				}
			}
		}
	} else {
		if req.RecipientUserId != nil {
			if err := s.sendNotification(ctx, msg.GetId(), req.GetRecipientUserId(), senderUserId, &now, msg.GetTitle(), msg.GetContent()); err != nil {
				s.log.Warnf("failed to send notification to user %d: %v", req.GetRecipientUserId(), err)
			}
		} else if len(req.TargetUserIds) != 0 {
			for _, targetUID := range req.TargetUserIds {
				if err := s.sendNotification(ctx, msg.GetId(), targetUID, senderUserId, &now, msg.GetTitle(), msg.GetContent()); err != nil {
					s.log.Warnf("failed to send notification to user %d: %v", targetUID, err)
				}
			}
		}
	}

	return &notificationpb.SendMessageResponse{
		MessageId: msg.GetId(),
	}, nil
}

// sendNotification sends a notification to a single user via SSE
func (s *InternalMessageService) sendNotification(ctx context.Context, messageId uint32, recipientUserId uint32, senderUserId uint32, now *time.Time, title, content string) error {
	recipient := &notificationpb.InternalMessageRecipient{
		MessageId:       trans.Ptr(messageId),
		RecipientUserId: trans.Ptr(recipientUserId),
		Status:          trans.Ptr(notificationpb.InternalMessageRecipient_SENT),
		CreatedBy:       trans.Ptr(senderUserId),
		CreatedAt:       timeutil.TimeToTimestamppb(now),
		Title:           trans.Ptr(title),
		Content:         trans.Ptr(content),
	}

	entity, err := s.internalMessageRecipientRepo.Create(ctx, recipient)
	if err != nil {
		s.log.Errorf("send message failed, send to user failed, %s", err)
		return err
	}
	recipient.Id = entity.Id
	s.log.Infof("sendNotification: created recipient record id=%d for user %d, message %d", entity.GetId(), recipientUserId, messageId)

	if s.sseServer == nil {
		s.log.Warnf("sendNotification: sseServer is nil, skipping SSE publish for user %d", recipientUserId)
		return nil
	}

	recipientJson, _ := json.Marshal(recipient)

	recipientStreamIds := s.userToken.GetAccessTokens(ctx, recipientUserId)
	s.log.Infof("sendNotification: user %d has %d active SSE streams", recipientUserId, len(recipientStreamIds))
	for _, streamId := range recipientStreamIds {
		s.sseServer.Publish(ctx, sse.StreamID(streamId), &sse.Event{
			ID:    []byte(uuid.New().String()),
			Data:  recipientJson,
			Event: []byte("notification"),
		})
	}

	return nil
}
