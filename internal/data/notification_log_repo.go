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
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/notificationlog"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type NotificationLogRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *log.Helper
}

func NewNotificationLogRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *NotificationLogRepo {
	return &NotificationLogRepo{
		log:       ctx.NewLoggerHelper("notification/repo/notification_log"),
		entClient: entClient,
	}
}

// maxRenderedBodySize limits stored rendered bodies to 1MB to prevent storage abuse.
const maxRenderedBodySize = 1 << 20

func (r *NotificationLogRepo) Create(ctx context.Context, tenantID uint32, channelID string, channelType notificationlog.ChannelType, templateID, recipient, renderedSubject, renderedBody string, createdBy *uint32) (*ent.NotificationLog, error) {
	// H4: Reject zero tenant_id to prevent cross-tenant data leaks (allow platform admins and mTLS clients)
	if tenantID == 0 && !grpcx.IsPlatformAdmin(ctx) && mtls.GetClientID(ctx) == "" {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}

	id := uuid.New().String()

	// M8: Truncate oversized rendered body
	if len(renderedBody) > maxRenderedBodySize {
		renderedBody = renderedBody[:maxRenderedBodySize]
	}

	builder := r.entClient.Client().NotificationLog.Create().
		SetID(id).
		SetTenantID(tenantID).
		SetChannelID(channelID).
		SetChannelType(channelType).
		SetTemplateID(templateID).
		SetRecipient(recipient).
		SetRenderedSubject(renderedSubject).
		SetRenderedBody(renderedBody).
		SetStatus(notificationlog.StatusPENDING).
		SetCreateTime(time.Now())

	if createdBy != nil {
		builder.SetCreateBy(*createdBy)
	}

	entity, err := builder.Save(ctx)
	if err != nil {
		r.log.Errorf("create notification log failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("create notification log failed")
	}

	return entity, nil
}

func (r *NotificationLogRepo) MarkSent(ctx context.Context, tenantID uint32, id string) (*ent.NotificationLog, error) {
	// M4: Atomically scope the update by tenant_id to prevent TOCTOU
	affected, err := r.entClient.Client().NotificationLog.Update().
		Where(notificationlog.IDEQ(id), notificationlog.TenantIDEQ(tenantID)).
		SetStatus(notificationlog.StatusSENT).
		SetSentAt(time.Now()).
		Save(ctx)
	if err != nil {
		r.log.Errorf("mark notification sent failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("mark notification sent failed")
	}
	if affected == 0 {
		return nil, notificationpb.ErrorInternalServerError("mark notification sent failed")
	}
	// Re-fetch to return the updated entity
	return r.GetByID(ctx, tenantID, id)
}

func (r *NotificationLogRepo) MarkFailed(ctx context.Context, tenantID uint32, id string, errMsg string) (*ent.NotificationLog, error) {
	// Truncate error message to prevent unbounded storage
	if len(errMsg) > 2048 {
		errMsg = errMsg[:2048]
	}

	// M4: Atomically scope the update by tenant_id to prevent TOCTOU
	affected, err := r.entClient.Client().NotificationLog.Update().
		Where(notificationlog.IDEQ(id), notificationlog.TenantIDEQ(tenantID)).
		SetStatus(notificationlog.StatusFAILED).
		SetErrorMessage(errMsg).
		Save(ctx)
	if err != nil {
		r.log.Errorf("mark notification failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("mark notification failed")
	}
	if affected == 0 {
		return nil, notificationpb.ErrorInternalServerError("mark notification failed")
	}
	// Re-fetch to return the updated entity
	return r.GetByID(ctx, tenantID, id)
}

func (r *NotificationLogRepo) GetByID(ctx context.Context, tenantID uint32, id string) (*ent.NotificationLog, error) {
	entity, err := r.entClient.Client().NotificationLog.Query().
		Where(notificationlog.IDEQ(id), notificationlog.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		r.log.Errorf("get notification log failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("get notification log failed")
	}
	return entity, nil
}

func (r *NotificationLogRepo) ListByTenant(ctx context.Context, tenantID uint32, channelType *notificationlog.ChannelType, status *notificationlog.Status, recipient *string, createdBy *uint32, page, pageSize uint32) ([]*ent.NotificationLog, int, error) {
	query := r.entClient.Client().NotificationLog.Query().
		Where(notificationlog.TenantIDEQ(tenantID))

	if channelType != nil {
		query = query.Where(notificationlog.ChannelTypeEQ(*channelType))
	}
	if status != nil {
		query = query.Where(notificationlog.StatusEQ(*status))
	}
	if recipient != nil {
		query = query.Where(notificationlog.RecipientContains(*recipient))
	}
	// H1: Filter by creator to prevent cross-user enumeration
	if createdBy != nil {
		query = query.Where(notificationlog.CreateByEQ(*createdBy))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		r.log.Errorf("count notification logs failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("count notification logs failed")
	}

	// M2: Always apply pagination limit to prevent unbounded queries
	if page == 0 {
		page = 1
	}
	// H5: Compute offset as int to avoid uint32 overflow with large page values
	offset := int(page-1) * int(pageSize)
	query = query.Offset(offset).Limit(int(pageSize))

	entities, err := query.
		Order(ent.Desc(notificationlog.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list notification logs failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list notification logs failed")
	}

	return entities, total, nil
}

// ToProto converts a NotificationLog entity to its proto representation.
// When includeContent is false, rendered_subject and rendered_body are omitted
// to avoid leaking sensitive data (e.g., OTPs, reset links) in list responses.
func (r *NotificationLogRepo) ToProto(entity *ent.NotificationLog, includeContent bool) *notificationpb.NotificationLog {
	if entity == nil {
		return nil
	}

	proto := &notificationpb.NotificationLog{
		Id:           entity.ID,
		TenantId:     derefUint32(entity.TenantID),
		ChannelId:    entity.ChannelID,
		ChannelType:  logChannelTypeToProto(entity.ChannelType),
		TemplateId:   entity.TemplateID,
		Recipient:    entity.Recipient,
		Status:       logStatusToProto(entity.Status),
		ErrorMessage: entity.ErrorMessage,
	}

	// Always include subject (not sensitive); only include body on detail views
	proto.RenderedSubject = entity.RenderedSubject
	if includeContent {
		proto.RenderedBody = entity.RenderedBody
	}

	if entity.CreateBy != nil {
		proto.CreatedBy = entity.CreateBy
	}
	if entity.CreateTime != nil && !entity.CreateTime.IsZero() {
		proto.CreateTime = timestamppb.New(*entity.CreateTime)
	}
	if entity.SentAt != nil && !entity.SentAt.IsZero() {
		proto.SentAt = timestamppb.New(*entity.SentAt)
	}

	return proto
}

func logChannelTypeToProto(t notificationlog.ChannelType) notificationpb.ChannelType {
	switch t {
	case notificationlog.ChannelTypeEMAIL:
		return notificationpb.ChannelType_CHANNEL_TYPE_EMAIL
	case notificationlog.ChannelTypeSMS:
		return notificationpb.ChannelType_CHANNEL_TYPE_SMS
	case notificationlog.ChannelTypeSLACK:
		return notificationpb.ChannelType_CHANNEL_TYPE_SLACK
	case notificationlog.ChannelTypeSSE:
		return notificationpb.ChannelType_CHANNEL_TYPE_SSE
	default:
		return notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED
	}
}

func logStatusToProto(s notificationlog.Status) notificationpb.DeliveryStatus {
	switch s {
	case notificationlog.StatusPENDING:
		return notificationpb.DeliveryStatus_DELIVERY_STATUS_PENDING
	case notificationlog.StatusSENT:
		return notificationpb.DeliveryStatus_DELIVERY_STATUS_SENT
	case notificationlog.StatusFAILED:
		return notificationpb.DeliveryStatus_DELIVERY_STATUS_FAILED
	default:
		return notificationpb.DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED
	}
}

