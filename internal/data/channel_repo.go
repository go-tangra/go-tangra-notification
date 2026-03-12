package data

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/go-tangra/go-tangra-common/grpcx"
	"github.com/go-tangra/go-tangra-common/middleware/mtls"
	"github.com/go-tangra/go-tangra-notification/internal/crypto"
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
	// H4: Reject zero tenant_id to prevent cross-tenant data leaks (allow platform admins and mTLS clients)
	if tenantID == 0 && !grpcx.IsPlatformAdmin(ctx) && mtls.GetClientID(ctx) == "" {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}

	id := uuid.New().String()

	// H4: Encrypt config at rest
	encryptedConfig, err := crypto.Encrypt(config)
	if err != nil {
		r.log.Errorf("encrypt channel config failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("create channel failed")
	}

	// M5: Use transaction to ensure unsetDefaults + create are atomic
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf("start transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("create channel failed")
	}
	defer tx.Rollback()

	if isDefault {
		if _, err := tx.Channel.Update().
			Where(
				channel.TenantIDEQ(tenantID),
				channel.TypeEQ(channelType),
				channel.IsDefaultEQ(true),
			).
			SetIsDefault(false).
			Save(ctx); err != nil {
			r.log.Errorf("failed to unset default channels: %v", err)
			return nil, notificationpb.ErrorInternalServerError("create channel failed")
		}
	}

	builder := tx.Channel.Create().
		SetID(id).
		SetTenantID(tenantID).
		SetName(name).
		SetType(channelType).
		SetConfig(encryptedConfig).
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

	if err := tx.Commit(); err != nil {
		r.log.Errorf("commit transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("create channel failed")
	}

	// Decrypt config in the returned entity for callers
	entity.Config = config

	return entity, nil
}

func (r *ChannelRepo) GetByID(ctx context.Context, tenantID uint32, id string) (*ent.Channel, error) {
	entity, err := r.entClient.Client().Channel.Query().
		Where(channel.IDEQ(id), channel.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		r.log.Errorf("get channel failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("get channel failed")
	}
	// H4: Decrypt config from storage
	r.decryptConfig(entity)
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
	// H4: Decrypt config from storage
	r.decryptConfig(entity)
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

	// M2: Always apply pagination limit to prevent unbounded queries
	if page == 0 {
		page = 1
	}
	// H5: Compute offset as int to avoid uint32 overflow with large page values
	offset := int(page-1) * int(pageSize)
	query = query.Offset(offset).Limit(int(pageSize))

	entities, err := query.
		Order(ent.Desc(channel.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list channels failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list channels failed")
	}

	// H4: Decrypt configs from storage
	for _, e := range entities {
		r.decryptConfig(e)
	}

	return entities, total, nil
}

// ListByTenantAndIDs lists channels filtered to a set of accessible IDs with pagination.
func (r *ChannelRepo) ListByTenantAndIDs(ctx context.Context, tenantID uint32, ids []string, channelType *channel.Type, page, pageSize uint32) ([]*ent.Channel, int, error) {
	query := r.entClient.Client().Channel.Query().
		Where(channel.TenantIDEQ(tenantID), channel.IDIn(ids...))

	if channelType != nil {
		query = query.Where(channel.TypeEQ(*channelType))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		r.log.Errorf("count channels failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("count channels failed")
	}

	if page == 0 {
		page = 1
	}
	// H5: Compute offset as int to avoid uint32 overflow with large page values
	offset := int(page-1) * int(pageSize)
	query = query.Offset(offset).Limit(int(pageSize))

	entities, err := query.
		Order(ent.Desc(channel.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list channels failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list channels failed")
	}

	for _, e := range entities {
		r.decryptConfig(e)
	}

	return entities, total, nil
}

func (r *ChannelRepo) Update(ctx context.Context, id string, tenantID uint32, name, config *string, enabled, isDefault *bool, updatedBy *uint32) (*ent.Channel, error) {
	// Verify the channel belongs to this tenant before updating (H7: tenant isolation)
	existing, err := r.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, notificationpb.ErrorChannelNotFound("channel not found")
	}

	// M5: Use transaction to ensure unsetDefaults + update are atomic
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf("start transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("update channel failed")
	}
	defer tx.Rollback()

	if isDefault != nil && *isDefault {
		if _, err := tx.Channel.Update().
			Where(
				channel.TenantIDEQ(tenantID),
				channel.TypeEQ(existing.Type),
				channel.IsDefaultEQ(true),
			).
			SetIsDefault(false).
			Save(ctx); err != nil {
			r.log.Errorf("failed to unset default channels: %v", err)
			return nil, notificationpb.ErrorInternalServerError("update channel failed")
		}
	}

	// M1: Use Update().Where() for atomic tenant-scoped update (no TOCTOU)
	builder := tx.Channel.Update().
		Where(channel.IDEQ(id), channel.TenantIDEQ(tenantID)).
		SetUpdateTime(time.Now())

	if name != nil {
		builder.SetName(*name)
	}
	if config != nil {
		// H4: Encrypt config at rest
		encryptedConfig, err := crypto.Encrypt(*config)
		if err != nil {
			r.log.Errorf("encrypt channel config failed: %v", err)
			return nil, notificationpb.ErrorInternalServerError("update channel failed")
		}
		builder.SetConfig(encryptedConfig)
	}
	if enabled != nil {
		builder.SetEnabled(*enabled)
	}
	if isDefault != nil {
		builder.SetIsDefault(*isDefault)
	}
	if updatedBy != nil {
		builder.SetUpdateBy(*updatedBy)
	}

	affected, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, notificationpb.ErrorChannelAlreadyExists("channel with this name already exists")
		}
		r.log.Errorf("update channel failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("update channel failed")
	}
	if affected == 0 {
		return nil, notificationpb.ErrorChannelNotFound("channel not found")
	}

	if err := tx.Commit(); err != nil {
		r.log.Errorf("commit transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("update channel failed")
	}

	// Re-fetch to return the updated entity with decrypted config
	return r.GetByID(ctx, tenantID, id)
}

func (r *ChannelRepo) Delete(ctx context.Context, tenantID uint32, id string) error {
	count, err := r.entClient.Client().Channel.Delete().
		Where(channel.IDEQ(id), channel.TenantIDEQ(tenantID)).
		Exec(ctx)
	if err != nil {
		r.log.Errorf("delete channel failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("delete channel failed")
	}
	if count == 0 {
		return notificationpb.ErrorChannelNotFound("channel not found")
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
		Config:    redactConfig(entity.Config),
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

// sensitiveKeys are JSON keys whose values should be redacted in API responses.
var sensitiveKeys = map[string]bool{
	"password":    true,
	"api_key":     true,
	"bot_token":   true,
	"webhook_url": true,
	"secret":      true,
	"token":       true,
}

// redactConfig strips sensitive fields from the channel config JSON before
// returning it in API responses. Handles nested objects recursively.
// Returns "{}" on parse failure to avoid leaking raw secrets.
func redactConfig(configJSON string) string {
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return "{}"
	}

	redactMapRecursive(cfg)

	redacted, err := json.Marshal(cfg)
	if err != nil {
		return "{}"
	}
	return string(redacted)
}

func redactMapRecursive(m map[string]interface{}) {
	for key, val := range m {
		if sensitiveKeys[key] {
			m[key] = "******"
		} else if nested, ok := val.(map[string]interface{}); ok {
			redactMapRecursive(nested)
		}
	}
}

// decryptConfig decrypts the channel config in-place if encryption is enabled.
// Falls back to plaintext for legacy data that was stored before encryption.
func (r *ChannelRepo) decryptConfig(entity *ent.Channel) {
	if entity == nil {
		return
	}
	decrypted, err := crypto.Decrypt(entity.Config)
	if err != nil {
		r.log.Warnf("failed to decrypt channel config %s (using as-is): %v", entity.ID, err)
		return
	}
	entity.Config = decrypted
}
