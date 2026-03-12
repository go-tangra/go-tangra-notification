package data

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/go-tangra/go-tangra-common/grpcx"
	"github.com/go-tangra/go-tangra-common/middleware/mtls"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/template"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type TemplateRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *log.Helper
}

func NewTemplateRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *TemplateRepo {
	return &TemplateRepo{
		log:       ctx.NewLoggerHelper("notification/repo/template"),
		entClient: entClient,
	}
}

func (r *TemplateRepo) Create(ctx context.Context, tenantID uint32, name, channelID, subject, body, variables string, isDefault bool, createdBy *uint32) (*ent.Template, error) {
	// H4: Reject zero tenant_id to prevent cross-tenant data leaks (allow platform admins and mTLS clients)
	if tenantID == 0 && !grpcx.IsPlatformAdmin(ctx) && mtls.GetClientID(ctx) == "" {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}

	id := uuid.New().String()

	// M5: Use transaction to ensure unsetDefaults + create are atomic
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf("start transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("create template failed")
	}
	defer tx.Rollback()

	if isDefault {
		if _, err := tx.Template.Update().
			Where(
				template.TenantIDEQ(tenantID),
				template.ChannelIDEQ(channelID),
				template.IsDefaultEQ(true),
			).
			SetIsDefault(false).
			Save(ctx); err != nil {
			r.log.Errorf("failed to unset default templates: %v", err)
			return nil, notificationpb.ErrorInternalServerError("create template failed")
		}
	}

	builder := tx.Template.Create().
		SetID(id).
		SetTenantID(tenantID).
		SetName(name).
		SetChannelID(channelID).
		SetSubject(subject).
		SetBody(body).
		SetVariables(variables).
		SetIsDefault(isDefault).
		SetCreateTime(time.Now())

	if createdBy != nil {
		builder.SetCreateBy(*createdBy)
	}

	entity, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, notificationpb.ErrorTemplateAlreadyExists("template with this name already exists")
		}
		r.log.Errorf("create template failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("create template failed")
	}

	if err := tx.Commit(); err != nil {
		r.log.Errorf("commit transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("create template failed")
	}

	return entity, nil
}

func (r *TemplateRepo) GetByID(ctx context.Context, tenantID uint32, id string) (*ent.Template, error) {
	entity, err := r.entClient.Client().Template.Query().
		Where(template.IDEQ(id), template.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		r.log.Errorf("get template failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("get template failed")
	}
	return entity, nil
}

func (r *TemplateRepo) ListByTenant(ctx context.Context, tenantID uint32, channelID *string, page, pageSize uint32) ([]*ent.Template, int, error) {
	query := r.entClient.Client().Template.Query().
		Where(template.TenantIDEQ(tenantID))

	if channelID != nil {
		query = query.Where(template.ChannelIDEQ(*channelID))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		r.log.Errorf("count templates failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("count templates failed")
	}

	// M2: Always apply pagination limit to prevent unbounded queries
	if page == 0 {
		page = 1
	}
	// H5: Compute offset as int to avoid uint32 overflow with large page values
	offset := int(page-1) * int(pageSize)
	query = query.Offset(offset).Limit(int(pageSize))

	entities, err := query.
		Order(ent.Desc(template.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list templates failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list templates failed")
	}

	return entities, total, nil
}

// ListByTenantAndIDs lists templates filtered to a set of accessible IDs with pagination.
func (r *TemplateRepo) ListByTenantAndIDs(ctx context.Context, tenantID uint32, ids []string, channelID *string, page, pageSize uint32) ([]*ent.Template, int, error) {
	query := r.entClient.Client().Template.Query().
		Where(template.TenantIDEQ(tenantID), template.IDIn(ids...))

	if channelID != nil {
		query = query.Where(template.ChannelIDEQ(*channelID))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		r.log.Errorf("count templates failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("count templates failed")
	}

	if page == 0 {
		page = 1
	}
	// H5: Compute offset as int to avoid uint32 overflow with large page values
	offset := int(page-1) * int(pageSize)
	query = query.Offset(offset).Limit(int(pageSize))

	entities, err := query.
		Order(ent.Desc(template.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list templates failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list templates failed")
	}

	return entities, total, nil
}

func (r *TemplateRepo) Update(ctx context.Context, id string, tenantID uint32, name, subject, body, variables *string, channelID *string, isDefault *bool, updatedBy *uint32) (*ent.Template, error) {
	// Verify the template belongs to this tenant before updating (H8: tenant isolation)
	existing, err := r.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, notificationpb.ErrorTemplateNotFound("template not found")
	}

	// M5: Use transaction to ensure unsetDefaults + update are atomic
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf("start transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("update template failed")
	}
	defer tx.Rollback()

	if isDefault != nil && *isDefault {
		// Use the new channel_id if provided, otherwise the existing one
		defaultChannelID := existing.ChannelID
		if channelID != nil {
			defaultChannelID = *channelID
		}
		if _, err := tx.Template.Update().
			Where(
				template.TenantIDEQ(tenantID),
				template.ChannelIDEQ(defaultChannelID),
				template.IsDefaultEQ(true),
			).
			SetIsDefault(false).
			Save(ctx); err != nil {
			r.log.Errorf("failed to unset default templates: %v", err)
			return nil, notificationpb.ErrorInternalServerError("update template failed")
		}
	}

	// M1: Use Update().Where() for atomic tenant-scoped update (no TOCTOU)
	builder := tx.Template.Update().
		Where(template.IDEQ(id), template.TenantIDEQ(tenantID)).
		SetUpdateTime(time.Now())

	if name != nil {
		builder.SetName(*name)
	}
	if channelID != nil {
		builder.SetChannelID(*channelID)
	}
	if subject != nil {
		builder.SetSubject(*subject)
	}
	if body != nil {
		builder.SetBody(*body)
	}
	if variables != nil {
		builder.SetVariables(*variables)
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
			return nil, notificationpb.ErrorTemplateAlreadyExists("template with this name already exists")
		}
		r.log.Errorf("update template failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("update template failed")
	}
	if affected == 0 {
		return nil, notificationpb.ErrorTemplateNotFound("template not found")
	}

	if err := tx.Commit(); err != nil {
		r.log.Errorf("commit transaction failed: %v", err)
		return nil, notificationpb.ErrorInternalServerError("update template failed")
	}

	// Re-fetch to return the updated entity
	return r.GetByID(ctx, tenantID, id)
}

func (r *TemplateRepo) Delete(ctx context.Context, tenantID uint32, id string) error {
	count, err := r.entClient.Client().Template.Delete().
		Where(template.IDEQ(id), template.TenantIDEQ(tenantID)).
		Exec(ctx)
	if err != nil {
		r.log.Errorf("delete template failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("delete template failed")
	}
	if count == 0 {
		return notificationpb.ErrorTemplateNotFound("template not found")
	}
	return nil
}

func (r *TemplateRepo) ToProto(entity *ent.Template) *notificationpb.NotificationTemplate {
	if entity == nil {
		return nil
	}

	proto := &notificationpb.NotificationTemplate{
		Id:        entity.ID,
		TenantId:  derefUint32(entity.TenantID),
		Name:      entity.Name,
		ChannelId: entity.ChannelID,
		Subject:   entity.Subject,
		Body:      entity.Body,
		Variables: entity.Variables,
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
