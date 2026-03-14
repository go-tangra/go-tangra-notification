package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-tangra/go-tangra-notification/internal/data"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type InternalMessageCategoryService struct {
	notificationpb.UnimplementedInternalMessageCategoryServiceServer

	log *log.Helper

	repo *data.InternalMessageCategoryRepo
}

func NewInternalMessageCategoryService(ctx *bootstrap.Context, repo *data.InternalMessageCategoryRepo) *InternalMessageCategoryService {
	return &InternalMessageCategoryService{
		log:  ctx.NewLoggerHelper("internal-message-category/service/notification-service"),
		repo: repo,
	}
}

func (s *InternalMessageCategoryService) List(ctx context.Context, req *paginationV1.PagingRequest) (*notificationpb.ListInternalMessageCategoryResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *InternalMessageCategoryService) Get(ctx context.Context, req *notificationpb.GetInternalMessageCategoryRequest) (*notificationpb.InternalMessageCategory, error) {
	return s.repo.Get(ctx, req)
}

func (s *InternalMessageCategoryService) Create(ctx context.Context, req *notificationpb.CreateInternalMessageCategoryRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	uid := getUserIDAsUint32(ctx)
	if uid != nil {
		req.Data.CreatedBy = uid
	}

	if err := s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *InternalMessageCategoryService) Update(ctx context.Context, req *notificationpb.UpdateInternalMessageCategoryRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	uid := getUserIDAsUint32(ctx)
	if uid != nil {
		req.Data.UpdatedBy = uid
		if req.UpdateMask != nil {
			req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
		}
	}

	if err := s.repo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *InternalMessageCategoryService) Delete(ctx context.Context, req *notificationpb.DeleteInternalMessageCategoryRequest) (*emptypb.Empty, error) {
	if err := s.repo.Delete(ctx, req); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

