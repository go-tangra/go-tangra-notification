package data

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type ChannelRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *log.Helper
}

func NewChannelRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *ChannelRepo {
	return &ChannelRepo{
		log:       ctx.NewLoggerHelper("notification/repo/channel"),
		entClient: entClient,
	}
}

func (r *ChannelRepo) Create(ctx context.Context, tenantID uint32, name string, channelType channel.Type, config string, enabled, isDefault bool, createdBy *uint32) (*ent.Channel, error) {
	id := uuid.New().String()

	if isDefault {
		r.unsetDefaults(ctx, tenantID, channelType)
	}

	builder := r.entClient.Client().Channel.Create().
		SetID(id).
		SetTenantID(tenantID).
		SetName(name).
		SetType(channelType).
		SetConfig(config).
		SetEnabled(enabled).
		SetIsDefault(isDefault).
		SetCreateTime(time.Now())

	if createdBy != nil {
		builder.SetCreateBy(*createdBy)
	}

	entity, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, notificationpb.ErrorChannelAlreadyExists("channel with this name already exists")
		}
		r.log.Errorf("create channel failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("create channel failed")
	}

	return entity, nil
}

func (r *ChannelRepo) GetByID(ctx context.Context, id string) (*ent.Channel, error) {
	entity, err := r.entClient.Client().Channel.Query().
		Where(channel.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		r.log.Errorf("get channel failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("get channel failed")
	}
	return entity, nil
}

func (r *ChannelRepo) GetDefaultByType(ctx context.Context, tenantID uint32, channelType channel.Type) (*ent.Channel, error) {
	entity, err := r.entClient.Client().Channel.Query().
		Where(
			channel.TenantIDEQ(tenantID),
			channel.TypeEQ(channelType),
			channel.IsDefaultEQ(true),
			channel.EnabledEQ(true),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		r.log.Errorf("get default channel failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("get default channel failed")
	}
	return entity, nil
}

func (r *ChannelRepo) ListByTenant(ctx context.Context, tenantID uint32, channelType *channel.Type, page, pageSize uint32) ([]*ent.Channel, int, error) {
	query := r.entClient.Client().Channel.Query().
		Where(channel.TenantIDEQ(tenantID))

	if channelType != nil {
		query = query.Where(channel.TypeEQ(*channelType))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		r.log.Errorf("count channels failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("count channels failed")
	}

	if page > 0 && pageSize > 0 {
		offset := int((page - 1) * pageSize)
		query = query.Offset(offset).Limit(int(pageSize))
	}

	entities, err := query.
		Order(ent.Desc(channel.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list channels failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list channels failed")
	}

	return entities, total, nil
}

func (r *ChannelRepo) Update(ctx context.Context, id string, tenantID uint32, name, config *string, enabled, isDefault *bool, updatedBy *uint32) (*ent.Channel, error) {
	builder := r.entClient.Client().Channel.UpdateOneID(id).
		SetUpdateTime(time.Now())

	if name != nil {
		builder.SetName(*name)
	}
	if config != nil {
		builder.SetConfig(*config)
	}
	if enabled != nil {
		builder.SetEnabled(*enabled)
	}
	if isDefault != nil {
		if *isDefault {
			// Need to get the entity to know its type
			existing, err := r.GetByID(ctx, id)
			if err != nil {
				return nil, err
			}
			if existing != nil {
				r.unsetDefaults(ctx, tenantID, existing.Type)
			}
		}
		builder.SetIsDefault(*isDefault)
	}
	if updatedBy != nil {
		builder.SetUpdateBy(*updatedBy)
	}

	entity, err := builder.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, notificationpb.ErrorChannelNotFound("channel not found")
		}
		if ent.IsConstraintError(err) {
			return nil, notificationpb.ErrorChannelAlreadyExists("channel with this name already exists")
		}
		r.log.Errorf("update channel failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("update channel failed")
	}

	return entity, nil
}

func (r *ChannelRepo) Delete(ctx context.Context, id string) error {
	err := r.entClient.Client().Channel.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return notificationpb.ErrorChannelNotFound("channel not found")
		}
		r.log.Errorf("delete channel failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("delete channel failed")
	}
	return nil
}

func (r *ChannelRepo) ToProto(entity *ent.Channel) *notificationpb.NotificationChannel {
	if entity == nil {
		return nil
	}

	proto := &notificationpb.NotificationChannel{
		Id:        entity.ID,
		TenantId:  derefUint32(entity.TenantID),
		Name:      entity.Name,
		Type:      channelTypeToProto(entity.Type),
		Config:    entity.Config,
		Enabled:   entity.Enabled,
		IsDefault: entity.IsDefault,
	}

	if entity.CreateBy != nil {
		proto.CreatedBy = entity.CreateBy
	}
	if entity.UpdateBy != nil {
		proto.UpdatedBy = entity.UpdateBy
	}
	if entity.CreateTime != nil && !entity.CreateTime.IsZero() {
		proto.CreateTime = timestamppb.New(*entity.CreateTime)
	}
	if entity.UpdateTime != nil && !entity.UpdateTime.IsZero() {
		proto.UpdateTime = timestamppb.New(*entity.UpdateTime)
	}

	return proto
}

func (r *ChannelRepo) unsetDefaults(ctx context.Context, tenantID uint32, channelType channel.Type) {
	_, err := r.entClient.Client().Channel.Update().
		Where(
			channel.TenantIDEQ(tenantID),
			channel.TypeEQ(channelType),
			channel.IsDefaultEQ(true),
		).
		SetIsDefault(false).
		Save(ctx)
	if err != nil {
		r.log.Warnf("failed to unset default channels: %v", err)
	}
}
