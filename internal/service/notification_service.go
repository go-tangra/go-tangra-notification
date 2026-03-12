package service

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-notification/internal/authz"
	"github.com/go-tangra/go-tangra-notification/internal/data"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/notificationlog"
	"github.com/go-tangra/go-tangra-notification/internal/metrics"
	channelPkg "github.com/go-tangra/go-tangra-notification/pkg/channel"
	"github.com/go-tangra/go-tangra-notification/pkg/renderer"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

const (
	defaultPageSize     uint32 = 50
	maxPageSize         uint32 = 500
	maxVariables        int    = 100
	maxVariableKeyLen   int    = 128
	maxVariableValueLen int    = 8192
)

// M6: Strict UUID v4 pattern for validating resource IDs at service layer
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// clampPageSize returns a page size within safe bounds.
func clampPageSize(pageSize uint32) uint32 {
	if pageSize == 0 {
		return defaultPageSize
	}
	if pageSize > maxPageSize {
		return maxPageSize
	}
	return pageSize
}

// M8: maxTrackedTenants caps the number of tenants tracked by the rate limiter
// to prevent unbounded memory growth from many distinct tenant IDs.
const maxTrackedTenants = 10000

// sendRateLimiter limits the rate of notifications per tenant.
type sendRateLimiter struct {
	mu      sync.Mutex
	tenants map[uint32][]time.Time
	limit   int
	window  time.Duration
}

func newSendRateLimiter(limit int, window time.Duration) *sendRateLimiter {
	rl := &sendRateLimiter{
		tenants: make(map[uint32][]time.Time),
		limit:   limit,
		window:  window,
	}
	// Background cleanup of inactive tenants to prevent unbounded memory growth
	go rl.cleanupLoop()
	return rl
}

func (r *sendRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-r.window)
		for tenantID, times := range r.tenants {
			valid := times[:0]
			for _, t := range times {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(r.tenants, tenantID)
			} else {
				r.tenants[tenantID] = valid
			}
		}
		r.mu.Unlock()
	}
}

func (r *sendRateLimiter) allow(tenantID uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)

	// Prune old entries
	times := r.tenants[tenantID]
	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= r.limit {
		r.tenants[tenantID] = valid
		return false
	}

	// M8: Reject if too many tenants are tracked to prevent memory exhaustion
	if _, exists := r.tenants[tenantID]; !exists && len(r.tenants) >= maxTrackedTenants {
		return false
	}

	r.tenants[tenantID] = append(valid, now)
	return true
}

// validateVariables checks that the variables map is within safe bounds.
func validateVariables(vars map[string]string) error {
	if len(vars) > maxVariables {
		return notificationpb.ErrorInvalidTemplate("too many variables (max %d)", maxVariables)
	}
	for k, v := range vars {
		if len(k) > maxVariableKeyLen {
			return notificationpb.ErrorInvalidTemplate("variable key too long (max %d chars)", maxVariableKeyLen)
		}
		if len(v) > maxVariableValueLen {
			return notificationpb.ErrorInvalidTemplate("variable value too long (max %d chars)", maxVariableValueLen)
		}
	}
	return nil
}

type NotificationService struct {
	notificationpb.UnimplementedNotificationServiceServer

	log          *log.Helper
	channelRepo  *data.ChannelRepo
	templateRepo *data.TemplateRepo
	notifLogRepo *data.NotificationLogRepo
	engine       *authz.Engine
	collector    *metrics.Collector
	rateLimiter  *sendRateLimiter
}

func NewNotificationService(
	ctx *bootstrap.Context,
	channelRepo *data.ChannelRepo,
	templateRepo *data.TemplateRepo,
	notifLogRepo *data.NotificationLogRepo,
	engine *authz.Engine,
	collector *metrics.Collector,
) *NotificationService {
	return &NotificationService{
		log:          ctx.NewLoggerHelper("notification/service/notification"),
		channelRepo:  channelRepo,
		templateRepo: templateRepo,
		notifLogRepo: notifLogRepo,
		engine:       engine,
		collector:    collector,
		rateLimiter:  newSendRateLimiter(60, time.Minute), // 60 sends/minute/tenant
	}
}

func (s *NotificationService) SendNotification(ctx context.Context, req *notificationpb.SendNotificationRequest) (*notificationpb.SendNotificationResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	caller := getCallerIdentity(ctx)

	// H3: Fail-closed — require authentication for sending notifications
	if caller == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	// Keep createdBy for the notification log record
	createdBy := getUserIDAsUint32(ctx)

	// Rate limit per tenant
	if !s.rateLimiter.allow(tenantID) {
		return nil, notificationpb.ErrorServiceUnavailable("notification rate limit exceeded, try again later")
	}

	// M10: Validate template_id and channel_id format
	if !uuidPattern.MatchString(req.TemplateId) {
		return nil, notificationpb.ErrorInvalidTemplate("invalid template ID format")
	}
	if req.ChannelId != nil && !uuidPattern.MatchString(*req.ChannelId) {
		return nil, notificationpb.ErrorInvalidChannelConfig("invalid channel ID format")
	}

	// Validate variables map size
	if req.Variables != nil {
		if err := validateVariables(req.Variables); err != nil {
			return nil, err
		}
	}

	// 1. Load template (tenant-scoped)
	tmpl, err := s.templateRepo.GetByID(ctx, tenantID, req.TemplateId)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, notificationpb.ErrorTemplateNotFound("template not found")
	}

	// 2. Resolve channel from template's channel_id (or override from request)
	var ch *ent.Channel
	if req.ChannelId != nil {
		ch, err = s.channelRepo.GetByID(ctx, tenantID, *req.ChannelId)
		if err != nil {
			return nil, err
		}
		if ch == nil {
			return nil, notificationpb.ErrorChannelNotFound("channel not found")
		}
	} else {
		// Use the channel referenced by the template
		ch, err = s.channelRepo.GetByID(ctx, tenantID, tmpl.ChannelID)
		if err != nil {
			return nil, err
		}
		if ch == nil {
			return nil, notificationpb.ErrorChannelNotFound("template references a channel that no longer exists")
		}
	}

	// M4: Channel-type-aware recipient validation
	switch ch.Type {
	case channel.TypeEMAIL:
		parsedAddr, err := mail.ParseAddress(req.Recipient)
		if err != nil {
			return nil, notificationpb.ErrorInvalidRecipient("invalid recipient email address")
		}
		req.Recipient = parsedAddr.Address
	default:
		// For non-email channels, just enforce a reasonable length
		if len(req.Recipient) == 0 || len(req.Recipient) > 512 {
			return nil, notificationpb.ErrorInvalidRecipient("invalid recipient")
		}
	}

	// Check USE permission on template
	var templateCheck, channelCheck authz.CheckContext
	if caller.isClient {
		templateCheck = authz.CheckContext{
			TenantID:     tenantID,
			SubjectID:    caller.clientID,
			SubjectType:  authz.SubjectTypeClient,
			ResourceType: authz.ResourceTypeTemplate,
			ResourceID:   tmpl.ID,
			Permission:   authz.PermissionUse,
		}
		channelCheck = authz.CheckContext{
			TenantID:     tenantID,
			SubjectID:    caller.clientID,
			SubjectType:  authz.SubjectTypeClient,
			ResourceType: authz.ResourceTypeChannel,
			ResourceID:   ch.ID,
			Permission:   authz.PermissionUse,
		}
	} else {
		uid := fmt.Sprintf("%d", caller.userID)
		templateCheck = authz.CheckContext{
			TenantID:     tenantID,
			SubjectID:    uid,
			SubjectType:  authz.SubjectTypeUser,
			ResourceType: authz.ResourceTypeTemplate,
			ResourceID:   tmpl.ID,
			Permission:   authz.PermissionUse,
		}
		channelCheck = authz.CheckContext{
			TenantID:     tenantID,
			SubjectID:    uid,
			SubjectType:  authz.SubjectTypeUser,
			ResourceType: authz.ResourceTypeChannel,
			ResourceID:   ch.ID,
			Permission:   authz.PermissionUse,
		}
	}

	useResult := s.engine.Check(ctx, templateCheck)
	if !useResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to use this template")
	}

	// Check USE permission on channel
	chUseResult := s.engine.Check(ctx, channelCheck)
	if !chUseResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to use this channel")
	}

	if !ch.Enabled {
		return nil, notificationpb.ErrorChannelDisabled("channel %q is disabled", ch.Name)
	}

	// 3. Render template
	isHTML := ch.Type == channel.TypeEMAIL
	vars := req.Variables
	if vars == nil {
		vars = map[string]string{}
	}

	renderedSubject, renderedBody, err := renderer.RenderSubjectAndBody(tmpl.Subject, tmpl.Body, vars, isHTML)
	if err != nil {
		s.log.Errorf("template render failed: %v", err)
		// M9: Use generic error to avoid leaking internal architecture
		return nil, notificationpb.ErrorInternalServerError("failed to process notification")
	}

	// 4. Create log entry (PENDING)
	logChannelType := channelTypeToLogChannelType(ch.Type)
	logEntry, err := s.notifLogRepo.Create(ctx, tenantID, ch.ID, logChannelType, tmpl.ID, req.Recipient, renderedSubject, renderedBody, createdBy)
	if err != nil {
		return nil, err
	}

	s.collector.NotificationCreated(string(notificationlog.StatusPENDING), string(ch.Type))

	// 5. Create sender and send
	sender, err := s.createSender(ch)
	if err != nil {
		logEntry, _ = s.notifLogRepo.MarkFailed(ctx, tenantID, logEntry.ID, "notification delivery failed")
		s.collector.NotificationStatusChanged(string(notificationlog.StatusPENDING), string(notificationlog.StatusFAILED))
		return &notificationpb.SendNotificationResponse{
			Notification: s.notifLogRepo.ToProto(logEntry, true),
		}, nil
	}

	msg := &channelPkg.Message{
		Recipient: req.Recipient,
		Subject:   renderedSubject,
		Body:      renderedBody,
	}

	if err := sender.Send(ctx, msg); err != nil {
		s.log.Errorf("send notification failed: %v", err)
		logEntry, _ = s.notifLogRepo.MarkFailed(ctx, tenantID, logEntry.ID, "notification delivery failed")
		s.collector.NotificationStatusChanged(string(notificationlog.StatusPENDING), string(notificationlog.StatusFAILED))
		return &notificationpb.SendNotificationResponse{
			Notification: s.notifLogRepo.ToProto(logEntry, true),
		}, nil
	}

	// 6. Mark sent
	logEntry, err = s.notifLogRepo.MarkSent(ctx, tenantID, logEntry.ID)
	if err != nil {
		return nil, err
	}

	s.collector.NotificationStatusChanged(string(notificationlog.StatusPENDING), string(notificationlog.StatusSENT))

	return &notificationpb.SendNotificationResponse{
		Notification: s.notifLogRepo.ToProto(logEntry, true),
	}, nil
}

func (s *NotificationService) GetNotification(ctx context.Context, req *notificationpb.GetNotificationRequest) (*notificationpb.GetNotificationResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	userID := getUserIDAsUint32(ctx)
	if userID == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	entity, err := s.notifLogRepo.GetByID(ctx, tenantID, req.Id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, notificationpb.ErrorNotificationNotFound("notification not found")
	}

	// M6: Only allow viewing notifications created by the requesting user
	if entity.CreateBy == nil || *entity.CreateBy != *userID {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to view this notification")
	}

	return &notificationpb.GetNotificationResponse{
		Notification: s.notifLogRepo.ToProto(entity, true),
	}, nil
}

func (s *NotificationService) ListNotifications(ctx context.Context, req *notificationpb.ListNotificationsRequest) (*notificationpb.ListNotificationsResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	userID := getUserIDAsUint32(ctx)
	if userID == nil {
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

	// H1: Filter by creator to prevent cross-user enumeration of notifications
	entities, total, err := s.notifLogRepo.ListByTenant(ctx, tenantID, channelType, status, req.Recipient, userID, page, pageSize)
	if err != nil {
		return nil, err
	}

	notifications := make([]*notificationpb.NotificationLog, 0, len(entities))
	for _, e := range entities {
		notifications = append(notifications, s.notifLogRepo.ToProto(e, false))
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

func channelTypeToLogChannelType(t channel.Type) notificationlog.ChannelType {
	switch t {
	case channel.TypeEMAIL:
		return notificationlog.ChannelTypeEMAIL
	case channel.TypeSMS:
		return notificationlog.ChannelTypeSMS
	case channel.TypeSLACK:
		return notificationlog.ChannelTypeSLACK
	case channel.TypeSSE:
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
