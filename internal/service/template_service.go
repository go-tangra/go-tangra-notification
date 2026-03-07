package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/data"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/template"
	"github.com/go-tangra/go-tangra-notification/pkg/renderer"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type TemplateService struct {
	notificationpb.UnimplementedNotificationTemplateServiceServer

	log          *log.Helper
	templateRepo *data.TemplateRepo
}

func NewTemplateService(
	ctx *bootstrap.Context,
	templateRepo *data.TemplateRepo,
) *TemplateService {
	return &TemplateService{
		log:          ctx.NewLoggerHelper("notification/service/template"),
		templateRepo: templateRepo,
	}
}

func (s *TemplateService) CreateTemplate(ctx context.Context, req *notificationpb.CreateTemplateRequest) (*notificationpb.CreateTemplateResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	createdBy := getUserIDAsUint32(ctx)

	if req.ChannelType == notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
		return nil, notificationpb.ErrorInvalidChannelType("channel type is required")
	}

	// Validate template by trying to render with sample variables
	isHTML := req.ChannelType == notificationpb.ChannelType_CHANNEL_TYPE_EMAIL
	sampleVars := map[string]string{"key": "value", "name": "Test"}
	_, _, err := renderer.RenderSubjectAndBody(req.Subject, req.Body, sampleVars, isHTML)
	if err != nil {
		return nil, notificationpb.ErrorInvalidTemplate("invalid template: %v", err)
	}

	channelType := protoToTemplateChannelType(req.ChannelType)
	entity, err := s.templateRepo.Create(ctx, tenantID, req.Name, channelType, req.Subject, req.Body, req.Variables, req.IsDefault, createdBy)
	if err != nil {
		return nil, err
	}

	return &notificationpb.CreateTemplateResponse{
		Template: s.templateRepo.ToProto(entity),
	}, nil
}

func (s *TemplateService) GetTemplate(ctx context.Context, req *notificationpb.GetTemplateRequest) (*notificationpb.GetTemplateResponse, error) {
	entity, err := s.templateRepo.GetByID(ctx, req.Id)
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

	var page, pageSize uint32
	if req.Page != nil {
		page = *req.Page
	}
	if req.PageSize != nil {
		pageSize = *req.PageSize
	}

	var channelType *template.ChannelType
	if req.ChannelType != nil && *req.ChannelType != notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
		ct := protoToTemplateChannelType(*req.ChannelType)
		channelType = &ct
	}

	entities, total, err := s.templateRepo.ListByTenant(ctx, tenantID, channelType, page, pageSize)
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

	// Validate template if content is being updated
	if req.Subject != nil || req.Body != nil {
		existing, err := s.templateRepo.GetByID(ctx, req.Id)
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

		isHTML := existing.ChannelType == template.ChannelTypeEMAIL
		sampleVars := map[string]string{"key": "value", "name": "Test"}
		_, _, err = renderer.RenderSubjectAndBody(subj, bod, sampleVars, isHTML)
		if err != nil {
			return nil, notificationpb.ErrorInvalidTemplate("invalid template: %v", err)
		}
	}

	entity, err := s.templateRepo.Update(ctx, req.Id, tenantID, req.Name, req.Subject, req.Body, req.Variables, req.IsDefault, updatedBy)
	if err != nil {
		return nil, err
	}

	return &notificationpb.UpdateTemplateResponse{
		Template: s.templateRepo.ToProto(entity),
	}, nil
}

func (s *TemplateService) DeleteTemplate(ctx context.Context, req *notificationpb.DeleteTemplateRequest) (*emptypb.Empty, error) {
	if err := s.templateRepo.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *TemplateService) PreviewTemplate(ctx context.Context, req *notificationpb.PreviewTemplateRequest) (*notificationpb.PreviewTemplateResponse, error) {
	isHTML := req.ChannelType == notificationpb.ChannelType_CHANNEL_TYPE_EMAIL
	vars := req.Variables
	if vars == nil {
		vars = map[string]string{"key": "value", "name": "Test User"}
	}

	renderedSubject, renderedBody, err := renderer.RenderSubjectAndBody(req.Subject, req.Body, vars, isHTML)
	if err != nil {
		return nil, notificationpb.ErrorInvalidTemplate("invalid template: %v", err)
	}

	return &notificationpb.PreviewTemplateResponse{
		RenderedSubject: renderedSubject,
		RenderedBody:    renderedBody,
	}, nil
}

func protoToTemplateChannelType(t notificationpb.ChannelType) template.ChannelType {
	switch t {
	case notificationpb.ChannelType_CHANNEL_TYPE_EMAIL:
		return template.ChannelTypeEMAIL
	case notificationpb.ChannelType_CHANNEL_TYPE_SMS:
		return template.ChannelTypeSMS
	case notificationpb.ChannelType_CHANNEL_TYPE_SLACK:
		return template.ChannelTypeSLACK
	case notificationpb.ChannelType_CHANNEL_TYPE_SSE:
		return template.ChannelTypeSSE
	default:
		return template.ChannelTypeEMAIL
	}
}
