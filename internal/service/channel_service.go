package service

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/data"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

// ChannelType is a type alias for use in service-layer filter params
type ChannelType = channel.Type

type ChannelService struct {
	notificationpb.UnimplementedNotificationChannelServiceServer

	log         *log.Helper
	channelRepo *data.ChannelRepo
}

func NewChannelService(
	ctx *bootstrap.Context,
	channelRepo *data.ChannelRepo,
) *ChannelService {
	return &ChannelService{
		log:         ctx.NewLoggerHelper("notification/service/channel"),
		channelRepo: channelRepo,
	}
}

func (s *ChannelService) CreateChannel(ctx context.Context, req *notificationpb.CreateChannelRequest) (*notificationpb.CreateChannelResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	createdBy := getUserIDAsUint32(ctx)

	if req.Type == notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
		return nil, notificationpb.ErrorInvalidChannelType("channel type is required")
	}

	// Validate config JSON
	if !json.Valid([]byte(req.Config)) {
		return nil, notificationpb.ErrorInvalidChannelConfig("config must be valid JSON")
	}

	channelType := protoToChannelType(req.Type)
	entity, err := s.channelRepo.Create(ctx, tenantID, req.Name, channelType, req.Config, req.Enabled, req.IsDefault, createdBy)
	if err != nil {
		return nil, err
	}

	return &notificationpb.CreateChannelResponse{
		Channel: s.channelRepo.ToProto(entity),
	}, nil
}

func (s *ChannelService) GetChannel(ctx context.Context, req *notificationpb.GetChannelRequest) (*notificationpb.GetChannelResponse, error) {
	entity, err := s.channelRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, notificationpb.ErrorChannelNotFound("channel not found")
	}

	return &notificationpb.GetChannelResponse{
		Channel: s.channelRepo.ToProto(entity),
	}, nil
}

func (s *ChannelService) ListChannels(ctx context.Context, req *notificationpb.ListChannelsRequest) (*notificationpb.ListChannelsResponse, error) {
	tenantID := getTenantIDFromContext(ctx)

	var page, pageSize uint32
	if req.Page != nil {
		page = *req.Page
	}
	if req.PageSize != nil {
		pageSize = *req.PageSize
	}

	var channelType *ChannelType
	if req.Type != nil && *req.Type != notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
		ct := protoToChannelType(*req.Type)
		channelType = &ct
	}

	entities, total, err := s.channelRepo.ListByTenant(ctx, tenantID, channelType, page, pageSize)
	if err != nil {
		return nil, err
	}

	channels := make([]*notificationpb.NotificationChannel, 0, len(entities))
	for _, e := range entities {
		channels = append(channels, s.channelRepo.ToProto(e))
	}

	return &notificationpb.ListChannelsResponse{
		Channels: channels,
		Total:    uint32(total),
	}, nil
}

func (s *ChannelService) UpdateChannel(ctx context.Context, req *notificationpb.UpdateChannelRequest) (*notificationpb.UpdateChannelResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	updatedBy := getUserIDAsUint32(ctx)

	if req.Config != nil && !json.Valid([]byte(*req.Config)) {
		return nil, notificationpb.ErrorInvalidChannelConfig("config must be valid JSON")
	}

	entity, err := s.channelRepo.Update(ctx, req.Id, tenantID, req.Name, req.Config, req.Enabled, req.IsDefault, updatedBy)
	if err != nil {
		return nil, err
	}

	return &notificationpb.UpdateChannelResponse{
		Channel: s.channelRepo.ToProto(entity),
	}, nil
}

func (s *ChannelService) DeleteChannel(ctx context.Context, req *notificationpb.DeleteChannelRequest) (*emptypb.Empty, error) {
	if err := s.channelRepo.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
