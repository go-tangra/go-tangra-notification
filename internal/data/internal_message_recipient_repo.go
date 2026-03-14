package data

import (
	"context"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/timeutil"
	"github.com/tx7do/go-utils/trans"

	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/internalmessagerecipient"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/predicate"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type InternalMessageRecipientRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *log.Helper

	mapper          *mapper.CopierMapper[notificationpb.InternalMessageRecipient, ent.InternalMessageRecipient]
	statusConverter *mapper.EnumTypeConverter[notificationpb.InternalMessageRecipient_Status, internalmessagerecipient.Status]

	repository *entCrud.Repository[
		ent.InternalMessageRecipientQuery, ent.InternalMessageRecipientSelect,
		ent.InternalMessageRecipientCreate, ent.InternalMessageRecipientCreateBulk,
		ent.InternalMessageRecipientUpdate, ent.InternalMessageRecipientUpdateOne,
		ent.InternalMessageRecipientDelete,
		predicate.InternalMessageRecipient,
		notificationpb.InternalMessageRecipient, ent.InternalMessageRecipient,
	]
}

func NewInternalMessageRecipientRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *InternalMessageRecipientRepo {
	repo := &InternalMessageRecipientRepo{
		log:             ctx.NewLoggerHelper("internal-message-recipient/repo/notification-service"),
		entClient:       entClient,
		mapper:          mapper.NewCopierMapper[notificationpb.InternalMessageRecipient, ent.InternalMessageRecipient](),
		statusConverter: mapper.NewEnumTypeConverter[notificationpb.InternalMessageRecipient_Status, internalmessagerecipient.Status](notificationpb.InternalMessageRecipient_Status_name, notificationpb.InternalMessageRecipient_Status_value),
	}

	repo.init()

	return repo
}

func (r *InternalMessageRecipientRepo) init() {
	r.repository = entCrud.NewRepository[
		ent.InternalMessageRecipientQuery, ent.InternalMessageRecipientSelect,
		ent.InternalMessageRecipientCreate, ent.InternalMessageRecipientCreateBulk,
		ent.InternalMessageRecipientUpdate, ent.InternalMessageRecipientUpdateOne,
		ent.InternalMessageRecipientDelete,
		predicate.InternalMessageRecipient,
		notificationpb.InternalMessageRecipient, ent.InternalMessageRecipient,
	](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
}

func (r *InternalMessageRecipientRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*notificationpb.ListUserInboxResponse, error) {
	if req == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessageRecipient.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &notificationpb.ListUserInboxResponse{Total: 0, Items: nil}, nil
	}

	return &notificationpb.ListUserInboxResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *InternalMessageRecipientRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.entClient.Client().InternalMessageRecipient.Query().
		Where(internalmessagerecipient.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf("query exist failed: %s", err.Error())
		return false, notificationpb.ErrorInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *InternalMessageRecipientRepo) Get(ctx context.Context, req *notificationpb.GetInternalMessageRecipientRequest) (*notificationpb.InternalMessageRecipient, error) {
	if req == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessageRecipient.Query()

	var whereCond []func(s *sql.Selector)
	switch req.QueryBy.(type) {
	default:
	case *notificationpb.GetInternalMessageRecipientRequest_Id:
		whereCond = append(whereCond, internalmessagerecipient.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *InternalMessageRecipientRepo) Create(ctx context.Context, req *notificationpb.InternalMessageRecipient) (*notificationpb.InternalMessageRecipient, error) {
	if req == nil {
		return nil, notificationpb.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().InternalMessageRecipient.Create().
		SetNillableTenantID(req.TenantId).
		SetNillableMessageID(req.MessageId).
		SetNillableRecipientUserID(req.RecipientUserId).
		SetNillableStatus(r.statusConverter.ToEntity(req.Status)).
		SetNillableReceivedAt(timeutil.TimestamppbToTime(req.ReceivedAt)).
		SetNillableReadAt(timeutil.TimestamppbToTime(req.ReadAt)).
		SetCreatedAt(time.Now())

	entity, err := builder.Save(ctx)
	if err != nil {
		r.log.Errorf("insert internal message recipient failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("insert internal message recipient failed")
	}

	return r.mapper.ToDTO(entity), nil
}

func (r *InternalMessageRecipientRepo) Update(ctx context.Context, req *notificationpb.UpdateInternalMessageRecipientRequest) error {
	if req == nil || req.Data == nil {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			req.Data.CreatedBy = req.Data.UpdatedBy
			req.Data.UpdatedBy = nil
			_, err = r.Create(ctx, req.Data)
			return err
		}
	}

	builder := r.entClient.Client().InternalMessageRecipient.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *notificationpb.InternalMessageRecipient) {
			builder.
				SetNillableMessageID(req.Data.MessageId).
				SetNillableRecipientUserID(req.Data.RecipientUserId).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableReceivedAt(timeutil.TimestamppbToTime(req.Data.ReceivedAt)).
				SetNillableReadAt(timeutil.TimestamppbToTime(req.Data.ReadAt)).
				SetUpdatedAt(time.Now())
		},
		func(s *sql.Selector) {
			s.Where(sql.EQ(internalmessagerecipient.FieldID, req.GetId()))
		},
	)

	return err
}

func (r *InternalMessageRecipientRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	if err := r.entClient.Client().InternalMessageRecipient.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return notificationpb.ErrorNotFound("internal message recipient not found")
		}

		r.log.Errorf("delete one data failed: %s", err.Error())

		return notificationpb.ErrorInternalServerError("delete failed")
	}

	return nil
}

// MarkNotificationAsRead marks notifications as read
func (r *InternalMessageRecipientRepo) MarkNotificationAsRead(ctx context.Context, req *notificationpb.MarkNotificationAsReadRequest) error {
	if len(req.GetRecipientIds()) == 0 {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}
	if req.GetUserId() == 0 {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	now := time.Now()
	_, err := r.entClient.Client().InternalMessageRecipient.Update().
		Where(
			internalmessagerecipient.IDIn(req.GetRecipientIds()...),
			internalmessagerecipient.RecipientUserIDEQ(req.GetUserId()),
			internalmessagerecipient.StatusNEQ(internalmessagerecipient.StatusRead),
		).
		SetStatus(internalmessagerecipient.StatusRead).
		SetNillableReadAt(trans.Ptr(now)).
		SetNillableUpdatedAt(trans.Ptr(now)).
		Save(ctx)
	return err
}

// MarkNotificationsStatus marks notifications to a specific status
func (r *InternalMessageRecipientRepo) MarkNotificationsStatus(ctx context.Context, req *notificationpb.MarkNotificationsStatusRequest) error {
	if len(req.GetRecipientIds()) == 0 {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}
	if req.GetUserId() == 0 {
		return notificationpb.ErrorBadRequest("invalid parameter")
	}

	now := time.Now()
	var readAt *time.Time
	var receiveAt *time.Time
	switch req.GetNewStatus() {
	case notificationpb.InternalMessageRecipient_READ:
		readAt = trans.Ptr(now)
	case notificationpb.InternalMessageRecipient_RECEIVED:
		receiveAt = trans.Ptr(now)
	}

	_, err := r.entClient.Client().InternalMessageRecipient.Update().
		Where(
			internalmessagerecipient.IDIn(req.GetRecipientIds()...),
			internalmessagerecipient.RecipientUserIDEQ(req.GetUserId()),
			internalmessagerecipient.StatusNEQ(*r.statusConverter.ToEntity(trans.Ptr(req.GetNewStatus()))),
		).
		SetNillableStatus(r.statusConverter.ToEntity(trans.Ptr(req.GetNewStatus()))).
		SetNillableReadAt(readAt).
		SetNillableReceivedAt(receiveAt).
		SetNillableUpdatedAt(trans.Ptr(now)).
		Save(ctx)
	return err
}

// RevokeMessage revokes a message for a specific user
func (r *InternalMessageRecipientRepo) RevokeMessage(ctx context.Context, req *notificationpb.RevokeMessageRequest) error {
	_, err := r.entClient.Client().InternalMessageRecipient.Delete().
		Where(
			internalmessagerecipient.MessageIDEQ(req.GetMessageId()),
			internalmessagerecipient.RecipientUserIDEQ(req.GetUserId()),
		).
		Exec(ctx)
	return err
}

func (r *InternalMessageRecipientRepo) DeleteNotificationFromInbox(ctx context.Context, req *notificationpb.DeleteNotificationFromInboxRequest) error {
	_, err := r.entClient.Client().InternalMessageRecipient.Delete().
		Where(
			internalmessagerecipient.IDIn(req.GetRecipientIds()...),
			internalmessagerecipient.RecipientUserIDEQ(req.GetUserId()),
		).
		Exec(ctx)
	return err
}
