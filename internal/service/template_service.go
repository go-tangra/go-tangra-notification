package service

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/authz"
	"github.com/go-tangra/go-tangra-notification/internal/data"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"
	"github.com/go-tangra/go-tangra-notification/internal/metrics"
	"github.com/go-tangra/go-tangra-notification/pkg/renderer"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type TemplateService struct {
	notificationpb.UnimplementedNotificationTemplateServiceServer

	log            *log.Helper
	templateRepo   *data.TemplateRepo
	channelRepo    *data.ChannelRepo
	permissionRepo *data.PermissionRepo
	engine         *authz.Engine
	collector      *metrics.Collector
	previewLimiter *sendRateLimiter
}

func NewTemplateService(
	ctx *bootstrap.Context,
	templateRepo *data.TemplateRepo,
	channelRepo *data.ChannelRepo,
	permissionRepo *data.PermissionRepo,
	engine *authz.Engine,
	collector *metrics.Collector,
) *TemplateService {
	return &TemplateService{
		log:            ctx.NewLoggerHelper("notification/service/template"),
		templateRepo:   templateRepo,
		channelRepo:    channelRepo,
		permissionRepo: permissionRepo,
		engine:         engine,
		collector:      collector,
		previewLimiter: newSendRateLimiter(30, time.Minute), // 30 previews/minute/tenant
	}
}

func (s *TemplateService) CreateTemplate(ctx context.Context, req *notificationpb.CreateTemplateRequest) (*notificationpb.CreateTemplateResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	caller := getCallerIdentity(ctx)
	if caller == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	if req.ChannelId == "" {
		return nil, notificationpb.ErrorInvalidChannelConfig("channel_id is required")
	}

	// Validate the referenced channel exists and belongs to this tenant
	ch, err := s.channelRepo.GetByID(ctx, tenantID, req.ChannelId)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, notificationpb.ErrorChannelNotFound("referenced channel not found")
	}

	// M3: Validate name length
	if len(req.Name) > 255 {
		return nil, notificationpb.ErrorInvalidTemplate("template name too long (max 255 characters)")
	}

	// H3: Enforce size limits on subject and body
	if len(req.Subject) > 1024 {
		return nil, notificationpb.ErrorInvalidTemplate("subject too long (max 1024 bytes)")
	}
	if len(req.Body) > 64*1024 {
		return nil, notificationpb.ErrorInvalidTemplate("body too long (max 64KB)")
	}

	// M14: Validate template syntax without executing (missingkey=zero)
	isHTML := ch.Type == channel.TypeEMAIL
	if err := renderer.ValidateTemplate(req.Subject, req.Body, isHTML); err != nil {
		return nil, notificationpb.ErrorInvalidTemplate("invalid template syntax")
	}

	// For client callers, use nil createdBy (system-created)
	var createdBy *uint32
	if !caller.isClient {
		uid := caller.userID
		createdBy = &uid
	}

	entity, err := s.templateRepo.Create(ctx, tenantID, req.Name, req.ChannelId, req.Subject, req.Body, req.Variables, req.IsDefault, createdBy)
	if err != nil {
		return nil, err
	}

	// M1/M11: Auto-grant OWNER — if grant fails, delete the orphaned template and return error.
	// For mTLS clients, grant to wildcard client (*) so all trusted services can use it.
	// For users, grant to the specific user.
	grantTuple := authz.PermissionTuple{
		TenantID:     tenantID,
		ResourceType: authz.ResourceTypeTemplate,
		ResourceID:   entity.ID,
		Relation:     authz.RelationOwner,
		GrantedBy:    createdBy,
	}
	if caller.isClient {
		grantTuple.SubjectType = authz.SubjectTypeClient
		grantTuple.SubjectID = authz.WildcardSubjectID
	} else {
		grantTuple.SubjectType = authz.SubjectTypeUser
		grantTuple.SubjectID = fmt.Sprintf("%d", caller.userID)
	}
	if _, err := s.engine.Grant(ctx, grantTuple); err != nil {
		s.log.Errorf("failed to grant OWNER on template %s, rolling back: %v", entity.ID, err)
		if delErr := s.templateRepo.Delete(ctx, tenantID, entity.ID); delErr != nil {
			s.log.Errorf("failed to rollback template %s after grant failure: %v", entity.ID, delErr)
		}
		return nil, notificationpb.ErrorInternalServerError("failed to set up template permissions")
	}

	s.collector.TemplateCreated(string(ch.Type))

	return &notificationpb.CreateTemplateResponse{
		Template: s.templateRepo.ToProto(entity),
	}, nil
}

func (s *TemplateService) GetTemplate(ctx context.Context, req *notificationpb.GetTemplateRequest) (*notificationpb.GetTemplateResponse, error) {
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
		ResourceType: authz.ResourceTypeTemplate,
		ResourceID:   req.Id,
		Permission:   authz.PermissionRead,
	})
	if !result.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to view this template")
	}

	entity, err := s.templateRepo.GetByID(ctx, tenantID, req.Id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, notificationpb.ErrorTemplateNotFound("template not found")
	}

	return &notificationpb.GetTemplateResponse{
		Template: s.templateRepo.ToProto(entity),
	}, nil
}

func (s *TemplateService) ListTemplates(ctx context.Context, req *notificationpb.ListTemplatesRequest) (*notificationpb.ListTemplatesResponse, error) {
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

	var channelID *string
	if req.ChannelId != nil && *req.ChannelId != "" {
		channelID = req.ChannelId
	}

	// M4: Query accessible IDs first, then paginate within accessible set
	// Platform admins see all templates without permission filtering
	var entities []*ent.Template
	var total int
	var err error
	if !caller.isClient && isPlatformAdmin(ctx) {
		entities, total, err = s.templateRepo.ListByTenant(ctx, tenantID, channelID, page, pageSize)
	} else {
		var accessibleIDs []string
		if caller.isClient {
			accessibleIDs, err = s.engine.ListClientAccessibleResources(ctx, tenantID, caller.clientID, authz.ResourceTypeTemplate)
		} else {
			accessibleIDs, err = s.engine.ListAccessibleResources(ctx, tenantID, fmt.Sprintf("%d", caller.userID), authz.ResourceTypeTemplate, authz.PermissionRead)
		}
		if err != nil {
			s.log.Errorf("failed to list accessible templates: %v", err)
			return nil, notificationpb.ErrorInternalServerError("failed to check permissions")
		}
		entities, total, err = s.templateRepo.ListByTenantAndIDs(ctx, tenantID, accessibleIDs, channelID, page, pageSize)
	}
	if err != nil {
		return nil, err
	}

	templates := make([]*notificationpb.NotificationTemplate, 0, len(entities))
	for _, e := range entities {
		templates = append(templates, s.templateRepo.ToProto(e))
	}

	return &notificationpb.ListTemplatesResponse{
		Templates: templates,
		Total:     uint32(total),
	}, nil
}

func (s *TemplateService) UpdateTemplate(ctx context.Context, req *notificationpb.UpdateTemplateRequest) (*notificationpb.UpdateTemplateResponse, error) {
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
		ResourceType: authz.ResourceTypeTemplate,
		ResourceID:   req.Id,
		Permission:   authz.PermissionWrite,
	})
	if !writeResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to update this template")
	}

	// M4: Validate name length on update
	if req.Name != nil && len(*req.Name) > 255 {
		return nil, notificationpb.ErrorInvalidTemplate("template name too long (max 255 characters)")
	}

	// H3: Enforce size limits on subject and body
	if req.Subject != nil && len(*req.Subject) > 1024 {
		return nil, notificationpb.ErrorInvalidTemplate("subject too long (max 1024 bytes)")
	}
	if req.Body != nil && len(*req.Body) > 64*1024 {
		return nil, notificationpb.ErrorInvalidTemplate("body too long (max 64KB)")
	}

	// Validate channel_id if being changed
	if req.ChannelId != nil {
		ch, err := s.channelRepo.GetByID(ctx, tenantID, *req.ChannelId)
		if err != nil {
			return nil, err
		}
		if ch == nil {
			return nil, notificationpb.ErrorChannelNotFound("referenced channel not found")
		}
	}

	// M14: Validate template syntax if content is being updated
	if req.Subject != nil || req.Body != nil {
		existing, err := s.templateRepo.GetByID(ctx, tenantID, req.Id)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, notificationpb.ErrorTemplateNotFound("template not found")
		}

		subj := existing.Subject
		bod := existing.Body
		if req.Subject != nil {
			subj = *req.Subject
		}
		if req.Body != nil {
			bod = *req.Body
		}

		// Resolve channel to determine if HTML rendering is needed
		channelID := existing.ChannelID
		if req.ChannelId != nil {
			channelID = *req.ChannelId
		}
		ch, err := s.channelRepo.GetByID(ctx, tenantID, channelID)
		if err != nil {
			return nil, err
		}
		isHTML := ch != nil && ch.Type == channel.TypeEMAIL
		if err := renderer.ValidateTemplate(subj, bod, isHTML); err != nil {
			return nil, notificationpb.ErrorInvalidTemplate("invalid template syntax")
		}
	}

	entity, err := s.templateRepo.Update(ctx, req.Id, tenantID, req.Name, req.Subject, req.Body, req.Variables, req.ChannelId, req.IsDefault, updatedBy)
	if err != nil {
		return nil, err
	}

	return &notificationpb.UpdateTemplateResponse{
		Template: s.templateRepo.ToProto(entity),
	}, nil
}

func (s *TemplateService) DeleteTemplate(ctx context.Context, req *notificationpb.DeleteTemplateRequest) (*emptypb.Empty, error) {
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
		ResourceType: authz.ResourceTypeTemplate,
		ResourceID:   req.Id,
		Permission:   authz.PermissionDelete,
	})
	if !deleteResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to delete this template")
	}

	// Look up the template before deleting so we know its channel for metrics
	entity, err := s.templateRepo.GetByID(ctx, tenantID, req.Id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, notificationpb.ErrorTemplateNotFound("template not found")
	}

	// Resolve channel type for metrics
	var channelTypeStr string
	ch, err := s.channelRepo.GetByID(ctx, tenantID, entity.ChannelID)
	if err == nil && ch != nil {
		channelTypeStr = string(ch.Type)
	}

	// M6: Clean up permissions first, then delete the template.
	if err := s.permissionRepo.DeleteByResource(ctx, tenantID, authz.ResourceTypeTemplate, req.Id); err != nil {
		s.log.Errorf("failed to clean up permissions for template %s: %v", req.Id, err)
		return nil, notificationpb.ErrorInternalServerError("failed to delete template")
	}

	if err := s.templateRepo.Delete(ctx, tenantID, req.Id); err != nil {
		return nil, err
	}

	if channelTypeStr != "" {
		s.collector.TemplateDeleted(channelTypeStr)
	}

	return &emptypb.Empty{}, nil
}

func (s *TemplateService) PreviewTemplate(ctx context.Context, req *notificationpb.PreviewTemplateRequest) (*notificationpb.PreviewTemplateResponse, error) {
	if getUserIDAsUint32(ctx) == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	// M5: Rate limit preview renders per tenant
	tenantID := getTenantIDFromContext(ctx)
	if !s.previewLimiter.allow(tenantID) {
		return nil, notificationpb.ErrorServiceUnavailable("preview rate limit exceeded, try again later")
	}

	// H3: Enforce size limits on preview inputs
	if len(req.Subject) > 1024 {
		return nil, notificationpb.ErrorInvalidTemplate("subject too long (max 1024 bytes)")
	}
	if len(req.Body) > 64*1024 {
		return nil, notificationpb.ErrorInvalidTemplate("body too long (max 64KB)")
	}

	// Determine if HTML rendering by looking up channel type
	isHTML := false
	if req.ChannelId != "" {
		ch, err := s.channelRepo.GetByID(ctx, tenantID, req.ChannelId)
		if err == nil && ch != nil {
			isHTML = ch.Type == channel.TypeEMAIL
		}
	}

	vars := req.Variables
	if vars == nil {
		vars = map[string]string{"key": "value", "name": "Test User"}
	}

	// Validate variables map size
	if err := validateVariables(vars); err != nil {
		return nil, err
	}

	renderedSubject, renderedBody, err := renderer.RenderSubjectAndBody(req.Subject, req.Body, vars, isHTML)
	if err != nil {
		// M6: Don't expose Go template internals to the client
		s.log.Warnf("template preview render failed: %v", err)
		return nil, notificationpb.ErrorInvalidTemplate("template rendering failed")
	}

	return &notificationpb.PreviewTemplateResponse{
		RenderedSubject: renderedSubject,
		RenderedBody:    renderedBody,
	}, nil
}
