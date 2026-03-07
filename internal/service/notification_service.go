package service

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-notification/internal/data"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/notificationlog"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/template"
	channelPkg "github.com/go-tangra/go-tangra-notification/pkg/channel"
	"github.com/go-tangra/go-tangra-notification/pkg/renderer"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type NotificationService struct {
	notificationpb.UnimplementedNotificationServiceServer

	log             *log.Helper
	channelRepo     *data.ChannelRepo
	templateRepo    *data.TemplateRepo
	notifLogRepo    *data.NotificationLogRepo
}

func NewNotificationService(
	ctx *bootstrap.Context,
	channelRepo *data.ChannelRepo,
	templateRepo *data.TemplateRepo,
	notifLogRepo *data.NotificationLogRepo,
) *NotificationService {
	return &NotificationService{
		log:          ctx.NewLoggerHelper("notification/service/notification"),
		channelRepo:  channelRepo,
		templateRepo: templateRepo,
		notifLogRepo: notifLogRepo,
	}
}

func (s *NotificationService) SendNotification(ctx context.Context, req *notificationpb.SendNotificationRequest) (*notificationpb.SendNotificationResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	createdBy := getUserIDAsUint32(ctx)

	// 1. Load template
	tmpl, err := s.templateRepo.GetByID(ctx, req.TemplateId)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, notificationpb.ErrorTemplateNotFound("template not found: %s", req.TemplateId)
	}

	// 2. Resolve channel
	var ch *ent.Channel
	if req.ChannelId != nil {
		ch, err = s.channelRepo.GetByID(ctx, *req.ChannelId)
		if err != nil {
			return nil, err
		}
		if ch == nil {
			return nil, notificationpb.ErrorChannelNotFound("channel not found: %s", *req.ChannelId)
		}
	} else {
		// Use default channel for the template's channel type
		channelType := templateChannelTypeToChannelType(tmpl.ChannelType)
		ch, err = s.channelRepo.GetDefaultByType(ctx, tenantID, channelType)
		if err != nil {
			return nil, err
		}
		if ch == nil {
			return nil, notificationpb.ErrorChannelNotFound("no default channel configured for type %s", tmpl.ChannelType)
		}
	}

	if !ch.Enabled {
		return nil, notificationpb.ErrorChannelDisabled("channel %q is disabled", ch.Name)
	}

	// 3. Render template
	isHTML := tmpl.ChannelType == template.ChannelTypeEMAIL
	vars := req.Variables
	if vars == nil {
		vars = map[string]string{}
	}

	renderedSubject, renderedBody, err := renderer.RenderSubjectAndBody(tmpl.Subject, tmpl.Body, vars, isHTML)
	if err != nil {
		return nil, notificationpb.ErrorRenderFailed("template render failed: %v", err)
	}

	// 4. Create log entry (PENDING)
	logChannelType := templateToLogChannelType(tmpl.ChannelType)
	logEntry, err := s.notifLogRepo.Create(ctx, tenantID, ch.ID, logChannelType, tmpl.ID, req.Recipient, renderedSubject, renderedBody, createdBy)
	if err != nil {
		return nil, err
	}

	// 5. Create sender and send
	sender, err := s.createSender(ch)
	if err != nil {
		logEntry, _ = s.notifLogRepo.MarkFailed(ctx, logEntry.ID, err.Error())
		return &notificationpb.SendNotificationResponse{
			Notification: s.notifLogRepo.ToProto(logEntry),
		}, nil
	}

	msg := &channelPkg.Message{
		Recipient: req.Recipient,
		Subject:   renderedSubject,
		Body:      renderedBody,
	}

	if err := sender.Send(ctx, msg); err != nil {
		s.log.Errorf("send notification failed: %v", err)
		logEntry, _ = s.notifLogRepo.MarkFailed(ctx, logEntry.ID, err.Error())
		return &notificationpb.SendNotificationResponse{
			Notification: s.notifLogRepo.ToProto(logEntry),
		}, nil
	}

	// 6. Mark sent
	logEntry, err = s.notifLogRepo.MarkSent(ctx, logEntry.ID)
	if err != nil {
		return nil, err
	}

	return &notificationpb.SendNotificationResponse{
		Notification: s.notifLogRepo.ToProto(logEntry),
	}, nil
}

func (s *NotificationService) GetNotification(ctx context.Context, req *notificationpb.GetNotificationRequest) (*notificationpb.GetNotificationResponse, error) {
	entity, err := s.notifLogRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, notificationpb.ErrorNotificationNotFound("notification not found")
	}

	return &notificationpb.GetNotificationResponse{
		Notification: s.notifLogRepo.ToProto(entity),
	}, nil
}

func (s *NotificationService) ListNotifications(ctx context.Context, req *notificationpb.ListNotificationsRequest) (*notificationpb.ListNotificationsResponse, error) {
	tenantID := getTenantIDFromContext(ctx)

	var page, pageSize uint32
	if req.Page != nil {
		page = *req.Page
	}
	if req.PageSize != nil {
		pageSize = *req.PageSize
	}

	var channelType *notificationlog.ChannelType
	if req.ChannelType != nil && *req.ChannelType != notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
		ct := protoToLogChannelType(*req.ChannelType)
		channelType = &ct
	}

	var status *notificationlog.Status
	if req.Status != nil && *req.Status != notificationpb.DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED {
		st := protoToLogStatus(*req.Status)
		status = &st
	}

	entities, total, err := s.notifLogRepo.ListByTenant(ctx, tenantID, channelType, status, req.Recipient, page, pageSize)
	if err != nil {
		return nil, err
	}

	notifications := make([]*notificationpb.NotificationLog, 0, len(entities))
	for _, e := range entities {
		notifications = append(notifications, s.notifLogRepo.ToProto(e))
	}

	return &notificationpb.ListNotificationsResponse{
		Notifications: notifications,
		Total:         uint32(total),
	}, nil
}

func (s *NotificationService) createSender(ch *ent.Channel) (channelPkg.Sender, error) {
	switch ch.Type.String() {
	case "EMAIL":
		return channelPkg.NewEmailSender(ch.Config)
	default:
		return nil, fmt.Errorf("channel type %q is not yet implemented", ch.Type)
	}
}

func templateChannelTypeToChannelType(t template.ChannelType) ChannelType {
	switch t {
	case template.ChannelTypeEMAIL:
		return "EMAIL"
	case template.ChannelTypeSMS:
		return "SMS"
	case template.ChannelTypeSLACK:
		return "SLACK"
	case template.ChannelTypeSSE:
		return "SSE"
	default:
		return "EMAIL"
	}
}

func templateToLogChannelType(t template.ChannelType) notificationlog.ChannelType {
	switch t {
	case template.ChannelTypeEMAIL:
		return notificationlog.ChannelTypeEMAIL
	case template.ChannelTypeSMS:
		return notificationlog.ChannelTypeSMS
	case template.ChannelTypeSLACK:
		return notificationlog.ChannelTypeSLACK
	case template.ChannelTypeSSE:
		return notificationlog.ChannelTypeSSE
	default:
		return notificationlog.ChannelTypeEMAIL
	}
}

func protoToLogChannelType(t notificationpb.ChannelType) notificationlog.ChannelType {
	switch t {
	case notificationpb.ChannelType_CHANNEL_TYPE_EMAIL:
		return notificationlog.ChannelTypeEMAIL
	case notificationpb.ChannelType_CHANNEL_TYPE_SMS:
		return notificationlog.ChannelTypeSMS
	case notificationpb.ChannelType_CHANNEL_TYPE_SLACK:
		return notificationlog.ChannelTypeSLACK
	case notificationpb.ChannelType_CHANNEL_TYPE_SSE:
		return notificationlog.ChannelTypeSSE
	default:
		return notificationlog.ChannelTypeEMAIL
	}
}

func protoToLogStatus(s notificationpb.DeliveryStatus) notificationlog.Status {
	switch s {
	case notificationpb.DeliveryStatus_DELIVERY_STATUS_PENDING:
		return notificationlog.StatusPENDING
	case notificationpb.DeliveryStatus_DELIVERY_STATUS_SENT:
		return notificationlog.StatusSENT
	case notificationpb.DeliveryStatus_DELIVERY_STATUS_FAILED:
		return notificationlog.StatusFAILED
	default:
		return notificationlog.StatusPENDING
	}
}
