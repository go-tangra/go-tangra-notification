package data

import (
	"context"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/internalmessagecategory"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/predicate"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type InternalMessageCategoryRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *log.Helper

	mapper *mapper.CopierMapper[notificationpb.InternalMessageCategory, ent.InternalMessageCategory]

	repository *entCrud.Repository[
		ent.InternalMessageCategoryQuery, ent.InternalMessageCategorySelect,
		ent.InternalMessageCategoryCreate, ent.InternalMessageCategoryCreateBulk,
		ent.InternalMessageCategoryUpdate, ent.InternalMessageCategoryUpdateOne,
		ent.InternalMessageCategoryDelete,
		predicate.InternalMessageCategory,
		notificationpb.InternalMessageCategory, ent.InternalMessageCategory,
	]
}

func NewInternalMessageCategoryRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *InternalMessageCategoryRepo {
	repo := &InternalMessageCategoryRepo{
		log:       ctx.NewLoggerHelper("internal-message-category/repo/notification-service"),
		entClient: entClient,
		mapper:    mapper.NewCopierMapper[notificationpb.InternalMessageCategory, ent.InternalMessageCategory](),
	}

	repo.init()

	return repo
}

func (r *InternalMessageCategoryRepo) init() {
	r.repository = entCrud.NewRepository[
		ent.InternalMessageCategoryQuery, ent.InternalMessageCategorySelect,
		ent.InternalMessageCategoryCreate, ent.InternalMessageCategoryCreateBulk,
		ent.InternalMessageCategoryUpdate, ent.InternalMessageCategoryUpdateOne,
		ent.InternalMessageCategoryDelete,
		predicate.InternalMessageCategory,
		notificationpb.InternalMessageCategory, ent.InternalMessageCategory,
	](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())
}

func (r *InternalMessageCategoryRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*notificationpb.ListInternalMessageCategoryResponse, error) {
	if req == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessageCategory.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &notificationpb.ListInternalMessageCategoryResponse{Total: 0, Items: nil}, nil
	}

	return &notificationpb.ListInternalMessageCategoryResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *InternalMessageCategoryRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.entClient.Client().InternalMessageCategory.Query().
		Where(internalmessagecategory.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf("query exist failed: %s", err.Error())
		return false, notificationpb.ErrorInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *InternalMessageCategoryRepo) Get(ctx context.Context, req *notificationpb.GetInternalMessageCategoryRequest) (*notificationpb.InternalMessageCategory, error) {
	if req == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessageCategory.Query()

	var whereCond []func(s *sql.Selector)
	switch req.QueryBy.(type) {
	default:
	case *notificationpb.GetInternalMessageCategoryRequest_Id:
		whereCond = append(whereCond, internalmessagecategory.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// ListCategoriesByIds fetches categories by ID list
func (r *InternalMessageCategoryRepo) ListCategoriesByIds(ctx context.Context, ids []uint32) ([]*notificationpb.InternalMessageCategory, error) {
	if len(ids) == 0 {
		return []*notificationpb.InternalMessageCategory{}, nil
	}

	entities, err := r.entClient.Client().InternalMessageCategory.Query().
		Where(internalmessagecategory.IDIn(ids...)).
		All(ctx)
	if err != nil {
		r.log.Errorf("query internal message category by ids failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("query internal message category by ids failed")
	}

	dtos := make([]*notificationpb.InternalMessageCategory, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

func (r *InternalMessageCategoryRepo) Create(ctx context.Context, req *notificationpb.CreateInternalMessageCategoryRequest) error {
	if req == nil || req.Data == nil {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessageCategory.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableName(req.Data.Name).
		SetNillableCode(req.Data.Code).
		SetNillableIconURL(req.Data.IconUrl).
		SetNillableSortOrder(req.Data.SortOrder).
		SetNillableIsEnabled(req.Data.IsEnabled).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf("insert internal message category failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("insert internal message category failed")
	}

	return nil
}

func (r *InternalMessageCategoryRepo) Update(ctx context.Context, req *notificationpb.UpdateInternalMessageCategoryRequest) error {
	if req == nil || req.Data == nil {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &notificationpb.CreateInternalMessageCategoryRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.entClient.Client().InternalMessageCategory.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *notificationpb.InternalMessageCategory) {
			builder.
				SetNillableName(req.Data.Name).
				SetNillableCode(req.Data.Code).
				SetNillableIconURL(req.Data.IconUrl).
				SetNillableSortOrder(req.Data.SortOrder).
				SetNillableIsEnabled(req.Data.IsEnabled).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		func(s *sql.Selector) {
			s.Where(sql.EQ(internalmessagecategory.FieldID, req.GetId()))
		},
	)

	return err
}

func (r *InternalMessageCategoryRepo) Delete(ctx context.Context, req *notificationpb.DeleteInternalMessageCategoryRequest) error {
	if req == nil || req.GetId() == 0 {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	if err := r.entClient.Client().InternalMessageCategory.DeleteOneID(req.GetId()).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return notificationpb.ErrorNotFound("internal message category not found")
		}

		r.log.Errorf("delete internal message category failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("delete internal message category failed")
	}

	return nil
}
