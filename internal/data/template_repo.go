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

func (r *TemplateRepo) Create(ctx context.Context, tenantID uint32, name string, channelType template.ChannelType, subject, body, variables string, isDefault bool, createdBy *uint32) (*ent.Template, error) {
	id := uuid.New().String()

	if isDefault {
		r.unsetDefaults(ctx, tenantID, channelType)
	}

	builder := r.entClient.Client().Template.Create().
		SetID(id).
		SetTenantID(tenantID).
		SetName(name).
		SetChannelType(channelType).
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

	return entity, nil
}

func (r *TemplateRepo) GetByID(ctx context.Context, id string) (*ent.Template, error) {
	entity, err := r.entClient.Client().Template.Query().
		Where(template.IDEQ(id)).
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

func (r *TemplateRepo) ListByTenant(ctx context.Context, tenantID uint32, channelType *template.ChannelType, page, pageSize uint32) ([]*ent.Template, int, error) {
	query := r.entClient.Client().Template.Query().
		Where(template.TenantIDEQ(tenantID))

	if channelType != nil {
		query = query.Where(template.ChannelTypeEQ(*channelType))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		r.log.Errorf("count templates failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("count templates failed")
	}

	if page > 0 && pageSize > 0 {
		offset := int((page - 1) * pageSize)
		query = query.Offset(offset).Limit(int(pageSize))
	}

	entities, err := query.
		Order(ent.Desc(template.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list templates failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list templates failed")
	}

	return entities, total, nil
}

func (r *TemplateRepo) Update(ctx context.Context, id string, tenantID uint32, name, subject, body, variables *string, isDefault *bool, updatedBy *uint32) (*ent.Template, error) {
	builder := r.entClient.Client().Template.UpdateOneID(id).
		SetUpdateTime(time.Now())

	if name != nil {
		builder.SetName(*name)
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
		if *isDefault {
			existing, err := r.GetByID(ctx, id)
			if err != nil {
				return nil, err
			}
			if existing != nil {
				r.unsetDefaults(ctx, tenantID, existing.ChannelType)
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
			return nil, notificationpb.ErrorTemplateNotFound("template not found")
		}
		if ent.IsConstraintError(err) {
			return nil, notificationpb.ErrorTemplateAlreadyExists("template with this name already exists")
		}
		r.log.Errorf("update template failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("update template failed")
	}

	return entity, nil
}

func (r *TemplateRepo) Delete(ctx context.Context, id string) error {
	err := r.entClient.Client().Template.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return notificationpb.ErrorTemplateNotFound("template not found")
		}
		r.log.Errorf("delete template failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("delete template failed")
	}
	return nil
}

func (r *TemplateRepo) ToProto(entity *ent.Template) *notificationpb.NotificationTemplate {
	if entity == nil {
		return nil
	}

	proto := &notificationpb.NotificationTemplate{
		Id:          entity.ID,
		TenantId:    derefUint32(entity.TenantID),
		Name:        entity.Name,
		ChannelType: templateChannelTypeToProto(entity.ChannelType),
		Subject:     entity.Subject,
		Body:        entity.Body,
		Variables:   entity.Variables,
		IsDefault:   entity.IsDefault,
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

func (r *TemplateRepo) unsetDefaults(ctx context.Context, tenantID uint32, channelType template.ChannelType) {
	_, err := r.entClient.Client().Template.Update().
		Where(
			template.TenantIDEQ(tenantID),
			template.ChannelTypeEQ(channelType),
			template.IsDefaultEQ(true),
		).
		SetIsDefault(false).
		Save(ctx)
	if err != nil {
		r.log.Warnf("failed to unset default templates: %v", err)
	}
}
