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
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/internalmessage"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/predicate"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type InternalMessageRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *log.Helper

	mapper          *mapper.CopierMapper[notificationpb.InternalMessage, ent.InternalMessage]
	statusConverter *mapper.EnumTypeConverter[notificationpb.InternalMessage_Status, internalmessage.Status]
	typeConverter   *mapper.EnumTypeConverter[notificationpb.InternalMessage_Type, internalmessage.Type]

	repository *entCrud.Repository[
		ent.InternalMessageQuery, ent.InternalMessageSelect,
		ent.InternalMessageCreate, ent.InternalMessageCreateBulk,
		ent.InternalMessageUpdate, ent.InternalMessageUpdateOne,
		ent.InternalMessageDelete,
		predicate.InternalMessage,
		notificationpb.InternalMessage, ent.InternalMessage,
	]
}

func NewInternalMessageRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *InternalMessageRepo {
	repo := &InternalMessageRepo{
		log:             ctx.NewLoggerHelper("internal-message/repo/notification-service"),
		entClient:       entClient,
		mapper:          mapper.NewCopierMapper[notificationpb.InternalMessage, ent.InternalMessage](),
		statusConverter: mapper.NewEnumTypeConverter[notificationpb.InternalMessage_Status, internalmessage.Status](notificationpb.InternalMessage_Status_name, notificationpb.InternalMessage_Status_value),
		typeConverter:   mapper.NewEnumTypeConverter[notificationpb.InternalMessage_Type, internalmessage.Type](notificationpb.InternalMessage_Type_name, notificationpb.InternalMessage_Type_value),
	}

	repo.init()

	return repo
}

func (r *InternalMessageRepo) init() {
	r.repository = entCrud.NewRepository[
		ent.InternalMessageQuery, ent.InternalMessageSelect,
		ent.InternalMessageCreate, ent.InternalMessageCreateBulk,
		ent.InternalMessageUpdate, ent.InternalMessageUpdateOne,
		ent.InternalMessageDelete,
		predicate.InternalMessage,
		notificationpb.InternalMessage, ent.InternalMessage,
	](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.typeConverter.NewConverterPair())
}

func (r *InternalMessageRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*notificationpb.ListInternalMessageResponse, error) {
	if req == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessage.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &notificationpb.ListInternalMessageResponse{Total: 0, Items: nil}, nil
	}

	return &notificationpb.ListInternalMessageResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *InternalMessageRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.entClient.Client().InternalMessage.Query().
		Where(internalmessage.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf("query exist failed: %s", err.Error())
		return false, notificationpb.ErrorInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *InternalMessageRepo) Get(ctx context.Context, req *notificationpb.GetInternalMessageRequest) (*notificationpb.InternalMessage, error) {
	if req == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessage.Query()

	var whereCond []func(s *sql.Selector)
	switch req.QueryBy.(type) {
	default:
	case *notificationpb.GetInternalMessageRequest_Id:
		whereCond = append(whereCond, internalmessage.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *InternalMessageRepo) Create(ctx context.Context, req *notificationpb.CreateInternalMessageRequest) (*notificationpb.InternalMessage, error) {
	if req == nil || req.Data == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessage.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableTitle(req.Data.Title).
		SetNillableContent(req.Data.Content).
		SetSenderID(req.Data.GetSenderId()).
		SetNillableCategoryID(req.Data.CategoryId).
		SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
		SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	entity, err := builder.Save(ctx)
	if err != nil {
		r.log.Errorf("insert internal message failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("insert internal message failed")
	}

	return r.mapper.ToDTO(entity), nil
}

func (r *InternalMessageRepo) Update(ctx context.Context, req *notificationpb.UpdateInternalMessageRequest) error {
	if req == nil || req.Data == nil {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &notificationpb.CreateInternalMessageRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			_, err = r.Create(ctx, createReq)
			return err
		}
	}

	builder := r.entClient.Client().InternalMessage.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *notificationpb.InternalMessage) {
			builder.
				SetNillableTitle(req.Data.Title).
				SetNillableContent(req.Data.Content).
				SetNillableSenderID(req.Data.SenderId).
				SetNillableCategoryID(req.Data.CategoryId).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableType(r.typeConverter.ToEntity(req.Data.Type)).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		func(s *sql.Selector) {
			s.Where(sql.EQ(internalmessage.FieldID, req.GetId()))
		},
	)

	return err
}

func (r *InternalMessageRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	if err := r.entClient.Client().InternalMessage.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return notificationpb.ErrorNotFound("internal message not found")
		}

		r.log.Errorf("delete one data failed: %s", err.Error())

		return notificationpb.ErrorInternalServerError("delete failed")
	}

	return nil
}
