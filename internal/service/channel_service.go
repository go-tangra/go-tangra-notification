package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/authz"
	"github.com/go-tangra/go-tangra-notification/internal/data"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"
	"github.com/go-tangra/go-tangra-notification/internal/metrics"
	channelPkg "github.com/go-tangra/go-tangra-notification/pkg/channel"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

// ChannelType is a type alias for use in service-layer filter params
type ChannelType = channel.Type

type ChannelService struct {
	notificationpb.UnimplementedNotificationChannelServiceServer

	log            *log.Helper
	channelRepo    *data.ChannelRepo
	permissionRepo *data.PermissionRepo
	engine         *authz.Engine
	collector      *metrics.Collector
}

func NewChannelService(
	ctx *bootstrap.Context,
	channelRepo *data.ChannelRepo,
	permissionRepo *data.PermissionRepo,
	engine *authz.Engine,
	collector *metrics.Collector,
) *ChannelService {
	return &ChannelService{
		log:            ctx.NewLoggerHelper("notification/service/channel"),
		channelRepo:    channelRepo,
		permissionRepo: permissionRepo,
		engine:         engine,
		collector:      collector,
	}
}

func (s *ChannelService) CreateChannel(ctx context.Context, req *notificationpb.CreateChannelRequest) (*notificationpb.CreateChannelResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	createdBy := getUserIDAsUint32(ctx)

	// H1: Require authentication for channel creation
	if createdBy == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	if req.Type == notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
		return nil, notificationpb.ErrorInvalidChannelType("channel type is required")
	}

	// M3: Validate name length
	if len(req.Name) > 255 {
		return nil, notificationpb.ErrorInvalidChannelConfig("channel name too long (max 255 characters)")
	}

	// M7: Enforce config size limit
	if len(req.Config) > 8192 {
		return nil, notificationpb.ErrorInvalidChannelConfig("config too large (max 8192 bytes)")
	}

	// Validate config JSON
	if !json.Valid([]byte(req.Config)) {
		return nil, notificationpb.ErrorInvalidChannelConfig("config must be valid JSON")
	}

	// M5: Validate config for channel type at creation time (sanitize error)
	channelType := protoToChannelType(req.Type)
	if channelType == "EMAIL" {
		if _, err := channelPkg.ParseEmailConfig(req.Config); err != nil {
			s.log.Warnf("invalid email config on create: %v", err)
			return nil, notificationpb.ErrorInvalidChannelConfig("invalid email configuration")
		}
	}

	entity, err := s.channelRepo.Create(ctx, tenantID, req.Name, channelType, req.Config, req.Enabled, req.IsDefault, createdBy)
	if err != nil {
		return nil, err
	}

	// M1/M11: Auto-grant OWNER — if grant fails, delete the orphaned channel and return error.
	// NOTE: This is best-effort; if both grant AND compensating delete fail, the channel
	// is orphaned without permissions. A periodic cleanup job should detect such orphans.
	if _, err := s.engine.Grant(ctx, authz.PermissionTuple{
		TenantID:     tenantID,
		ResourceType: authz.ResourceTypeChannel,
		ResourceID:   entity.ID,
		Relation:     authz.RelationOwner,
		SubjectType:  authz.SubjectTypeUser,
		SubjectID:    fmt.Sprintf("%d", *createdBy),
		GrantedBy:    createdBy,
	}); err != nil {
		s.log.Errorf("failed to grant OWNER on channel %s, rolling back: %v", entity.ID, err)
		if delErr := s.channelRepo.Delete(ctx, tenantID, entity.ID); delErr != nil {
			s.log.Errorf("failed to rollback channel %s after grant failure: %v", entity.ID, delErr)
		}
		return nil, notificationpb.ErrorInternalServerError("failed to set up channel permissions")
	}

	s.collector.ChannelCreated(string(entity.Type))

	return &notificationpb.CreateChannelResponse{
		Channel: s.channelRepo.ToProto(entity),
	}, nil
}

func (s *ChannelService) GetChannel(ctx context.Context, req *notificationpb.GetChannelRequest) (*notificationpb.GetChannelResponse, error) {
	tenantID := getTenantIDFromContext(ctx)

	// Check READ permission
	userID := getUserIDAsUint32(ctx)
	if userID == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}
	result := s.engine.Check(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    fmt.Sprintf("%d", *userID),
		SubjectType:  authz.SubjectTypeUser,
		ResourceType: authz.ResourceTypeChannel,
		ResourceID:   req.Id,
		Permission:   authz.PermissionRead,
	})
	if !result.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to view this channel")
	}

	entity, err := s.channelRepo.GetByID(ctx, tenantID, req.Id)
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
	caller := getCallerIdentity(ctx)
	if caller == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	var page uint32
	if req.Page != nil {
		page = *req.Page
	}
	var pageSize uint32
	if req.PageSize != nil {
		pageSize = *req.PageSize
	}
	pageSize = clampPageSize(pageSize)

	var channelType *ChannelType
	if req.Type != nil && *req.Type != notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
		ct := protoToChannelType(*req.Type)
		channelType = &ct
	}

	// M4: Query accessible IDs first, then paginate within accessible set
	// Platform admins see all channels without permission filtering
	var entities []*ent.Channel
	var total int
	var err error
	if !caller.isClient && isPlatformAdmin(ctx) {
		entities, total, err = s.channelRepo.ListByTenant(ctx, tenantID, channelType, page, pageSize)
	} else {
		var accessibleIDs []string
		if caller.isClient {
			accessibleIDs, err = s.engine.ListClientAccessibleResources(ctx, tenantID, caller.clientID, authz.ResourceTypeChannel)
		} else {
			accessibleIDs, err = s.engine.ListAccessibleResources(ctx, tenantID, fmt.Sprintf("%d", caller.userID), authz.ResourceTypeChannel, authz.PermissionRead)
		}
		if err != nil {
			s.log.Errorf("failed to list accessible channels: %v", err)
			return nil, notificationpb.ErrorInternalServerError("failed to check permissions")
		}
		entities, total, err = s.channelRepo.ListByTenantAndIDs(ctx, tenantID, accessibleIDs, channelType, page, pageSize)
	}
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

	// Check WRITE permission
	if updatedBy == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}
	writeResult := s.engine.Check(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    fmt.Sprintf("%d", *updatedBy),
		SubjectType:  authz.SubjectTypeUser,
		ResourceType: authz.ResourceTypeChannel,
		ResourceID:   req.Id,
		Permission:   authz.PermissionWrite,
	})
	if !writeResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to update this channel")
	}

	// M4: Validate name length on update
	if req.Name != nil && len(*req.Name) > 255 {
		return nil, notificationpb.ErrorInvalidChannelConfig("channel name too long (max 255 characters)")
	}

	if req.Config != nil {
		// M7: Enforce config size limit
		if len(*req.Config) > 8192 {
			return nil, notificationpb.ErrorInvalidChannelConfig("config too large (max 8192 bytes)")
		}
		if !json.Valid([]byte(*req.Config)) {
			return nil, notificationpb.ErrorInvalidChannelConfig("config must be valid JSON")
		}
		// M5: Validate config for channel type at update time (sanitize error)
		existing, err := s.channelRepo.GetByID(ctx, tenantID, req.Id)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.Type == "EMAIL" {
			if _, err := channelPkg.ParseEmailConfig(*req.Config); err != nil {
				s.log.Warnf("invalid email config on update for channel %s: %v", req.Id, err)
				return nil, notificationpb.ErrorInvalidChannelConfig("invalid email configuration")
			}
		}
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
	tenantID := getTenantIDFromContext(ctx)

	// Check DELETE permission
	userID := getUserIDAsUint32(ctx)
	if userID == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}
	deleteResult := s.engine.Check(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    fmt.Sprintf("%d", *userID),
		SubjectType:  authz.SubjectTypeUser,
		ResourceType: authz.ResourceTypeChannel,
		ResourceID:   req.Id,
		Permission:   authz.PermissionDelete,
	})
	if !deleteResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to delete this channel")
	}

	// Look up the channel before deleting so we know its type for metrics
	entity, err := s.channelRepo.GetByID(ctx, tenantID, req.Id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, notificationpb.ErrorChannelNotFound("channel not found")
	}

	// M6: Clean up permissions first, then delete the channel.
	// This prevents orphaned permissions if the process crashes between operations.
	if err := s.permissionRepo.DeleteByResource(ctx, tenantID, authz.ResourceTypeChannel, req.Id); err != nil {
		s.log.Errorf("failed to clean up permissions for channel %s: %v", req.Id, err)
		return nil, notificationpb.ErrorInternalServerError("failed to delete channel")
	}

	if err := s.channelRepo.Delete(ctx, tenantID, req.Id); err != nil {
		return nil, err
	}

	s.collector.ChannelDeleted(string(entity.Type))

	return &emptypb.Empty{}, nil
}
