package data

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/go-tangra/go-tangra-common/grpcx"
	"github.com/go-tangra/go-tangra-common/middleware/mtls"
	"github.com/go-tangra/go-tangra-notification/internal/authz"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/predicate"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/templatepermission"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type PermissionRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *log.Helper
}

func NewPermissionRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *PermissionRepo {
	return &PermissionRepo{
		log:       ctx.NewLoggerHelper("notification/repo/permission"),
		entClient: entClient,
	}
}

// notExpiredPredicate returns a predicate that filters out expired permissions.
func notExpiredPredicate() predicate.TemplatePermission {
	return templatepermission.Or(
		templatepermission.ExpiresAtIsNil(),
		templatepermission.ExpiresAtGT(time.Now()),
	)
}

// GetDirectPermissions returns permissions directly on a resource
func (r *PermissionRepo) GetDirectPermissions(ctx context.Context, tenantID uint32, resourceType authz.ResourceType, resourceID string) ([]authz.PermissionTuple, error) {
	entities, err := r.entClient.Client().TemplatePermission.Query().
		Where(
			templatepermission.TenantIDEQ(tenantID),
			templatepermission.ResourceTypeEQ(templatepermission.ResourceType(resourceType)),
			templatepermission.ResourceIDEQ(resourceID),
			notExpiredPredicate(),
		).
		Limit(maxPermissionQueryResults).
		All(ctx)
	if err != nil {
		r.log.Errorf("get direct permissions failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("get permissions failed")
	}

	tuples := make([]authz.PermissionTuple, 0, len(entities))
	for _, e := range entities {
		tuples = append(tuples, r.toAuthzTuple(e))
	}

	return tuples, nil
}

// GetSubjectPermissions returns all permissions for a subject
func (r *PermissionRepo) GetSubjectPermissions(ctx context.Context, tenantID uint32, subjectType authz.SubjectType, subjectID string) ([]authz.PermissionTuple, error) {
	entities, err := r.entClient.Client().TemplatePermission.Query().
		Where(
			templatepermission.TenantIDEQ(tenantID),
			templatepermission.SubjectTypeEQ(templatepermission.SubjectType(subjectType)),
			templatepermission.SubjectIDEQ(subjectID),
			notExpiredPredicate(),
		).
		Limit(maxPermissionQueryResults).
		All(ctx)
	if err != nil {
		r.log.Errorf("get subject permissions failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("get permissions failed")
	}

	tuples := make([]authz.PermissionTuple, 0, len(entities))
	for _, e := range entities {
		tuples = append(tuples, r.toAuthzTuple(e))
	}

	return tuples, nil
}

// HasPermission checks if a specific permission exists
func (r *PermissionRepo) HasPermission(ctx context.Context, tenantID uint32, resourceType authz.ResourceType, resourceID string, subjectType authz.SubjectType, subjectID string) (*authz.PermissionTuple, error) {
	entity, err := r.entClient.Client().TemplatePermission.Query().
		Where(
			templatepermission.TenantIDEQ(tenantID),
			templatepermission.ResourceTypeEQ(templatepermission.ResourceType(resourceType)),
			templatepermission.ResourceIDEQ(resourceID),
			templatepermission.SubjectTypeEQ(templatepermission.SubjectType(subjectType)),
			templatepermission.SubjectIDEQ(subjectID),
			notExpiredPredicate(),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		r.log.Errorf("check permission failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("check permission failed")
	}

	tuple := r.toAuthzTuple(entity)
	return &tuple, nil
}

// CreatePermission creates a new permission
func (r *PermissionRepo) CreatePermission(ctx context.Context, tuple authz.PermissionTuple) (*authz.PermissionTuple, error) {
	// H2: Reject zero tenant_id to prevent cross-tenant permission injection (allow platform admins and mTLS clients)
	if tuple.TenantID == 0 && !grpcx.IsPlatformAdmin(ctx) && mtls.GetClientID(ctx) == "" {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}

	builder := r.entClient.Client().TemplatePermission.Create().
		SetTenantID(tuple.TenantID).
		SetResourceType(templatepermission.ResourceType(tuple.ResourceType)).
		SetResourceID(tuple.ResourceID).
		SetRelation(templatepermission.Relation(tuple.Relation)).
		SetSubjectType(templatepermission.SubjectType(tuple.SubjectType)).
		SetSubjectID(tuple.SubjectID).
		SetCreateTime(time.Now())

	if tuple.GrantedBy != nil {
		builder.SetGrantedBy(*tuple.GrantedBy)
	}
	if tuple.ExpiresAt != nil {
		builder.SetExpiresAt(*tuple.ExpiresAt)
	}

	entity, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, notificationpb.ErrorPermissionAlreadyExists("permission already exists")
		}
		r.log.Errorf("create permission failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("create permission failed")
	}

	result := r.toAuthzTuple(entity)
	return &result, nil
}

// DeletePermission deletes a permission
func (r *PermissionRepo) DeletePermission(ctx context.Context, tenantID uint32, resourceType authz.ResourceType, resourceID string, relation *authz.Relation, subjectType authz.SubjectType, subjectID string) error {
	query := r.entClient.Client().TemplatePermission.Delete().
		Where(
			templatepermission.TenantIDEQ(tenantID),
			templatepermission.ResourceTypeEQ(templatepermission.ResourceType(resourceType)),
			templatepermission.ResourceIDEQ(resourceID),
			templatepermission.SubjectTypeEQ(templatepermission.SubjectType(subjectType)),
			templatepermission.SubjectIDEQ(subjectID),
		)

	if relation != nil {
		query = query.Where(templatepermission.RelationEQ(templatepermission.Relation(*relation)))
	}

	_, err := query.Exec(ctx)
	if err != nil {
		r.log.Errorf("delete permission failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("delete permission failed")
	}

	return nil
}

// maxPermissionQueryResults caps permission queries to prevent unbounded result sets.
const maxPermissionQueryResults = 10000

// ListResourcesBySubject lists resources accessible by a subject
func (r *PermissionRepo) ListResourcesBySubject(ctx context.Context, tenantID uint32, subjectType authz.SubjectType, subjectID string, resourceType authz.ResourceType) ([]string, error) {
	entities, err := r.entClient.Client().TemplatePermission.Query().
		Where(
			templatepermission.TenantIDEQ(tenantID),
			templatepermission.SubjectTypeEQ(templatepermission.SubjectType(subjectType)),
			templatepermission.SubjectIDEQ(subjectID),
			templatepermission.ResourceTypeEQ(templatepermission.ResourceType(resourceType)),
			notExpiredPredicate(),
		).
		Select(templatepermission.FieldResourceID).
		Limit(maxPermissionQueryResults).
		All(ctx)
	if err != nil {
		r.log.Errorf("list resources by subject failed: %s", err.Error())
		return nil, notificationpb.ErrorInternalServerError("list resources failed")
	}

	ids := make([]string, 0, len(entities))
	for _, e := range entities {
		ids = append(ids, e.ResourceID)
	}

	return ids, nil
}

// ListByResource lists permissions with optional filters
func (r *PermissionRepo) ListByResource(ctx context.Context, tenantID uint32, resourceType templatepermission.ResourceType, resourceID string, subjectType *templatepermission.SubjectType, subjectID *string, page, pageSize uint32) ([]*ent.TemplatePermission, int, error) {
	query := r.entClient.Client().TemplatePermission.Query().
		Where(
			templatepermission.TenantIDEQ(tenantID),
			templatepermission.ResourceTypeEQ(resourceType),
			templatepermission.ResourceIDEQ(resourceID),
			notExpiredPredicate(),
		)

	if subjectType != nil {
		query = query.Where(templatepermission.SubjectTypeEQ(*subjectType))
	}
	if subjectID != nil && *subjectID != "" {
		query = query.Where(templatepermission.SubjectIDEQ(*subjectID))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		r.log.Errorf("count permissions failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("count permissions failed")
	}

	// M2: Always apply pagination limit to prevent unbounded queries
	if page == 0 {
		page = 1
	}
	// H5: Compute offset as int to avoid uint32 overflow with large page values
	offset := int(page-1) * int(pageSize)
	query = query.Offset(offset).Limit(int(pageSize))

	entities, err := query.
		Order(ent.Desc(templatepermission.FieldCreateTime)).
		All(ctx)
	if err != nil {
		r.log.Errorf("list permissions failed: %s", err.Error())
		return nil, 0, notificationpb.ErrorInternalServerError("list permissions failed")
	}

	return entities, total, nil
}

// DeleteByResource deletes all permissions for a resource
func (r *PermissionRepo) DeleteByResource(ctx context.Context, tenantID uint32, resourceType authz.ResourceType, resourceID string) error {
	_, err := r.entClient.Client().TemplatePermission.Delete().
		Where(
			templatepermission.TenantIDEQ(tenantID),
			templatepermission.ResourceTypeEQ(templatepermission.ResourceType(resourceType)),
			templatepermission.ResourceIDEQ(resourceID),
		).
		Exec(ctx)
	if err != nil {
		r.log.Errorf("delete permissions by resource failed: %s", err.Error())
		return notificationpb.ErrorInternalServerError("delete permissions failed")
	}
	return nil
}

// toAuthzTuple converts an ent.TemplatePermission to authz.PermissionTuple
func (r *PermissionRepo) toAuthzTuple(entity *ent.TemplatePermission) authz.PermissionTuple {
	tuple := authz.PermissionTuple{
		ID:           uint32(entity.ID),
		TenantID:     derefUint32(entity.TenantID),
		ResourceType: authz.ResourceType(entity.ResourceType),
		ResourceID:   entity.ResourceID,
		Relation:     authz.Relation(entity.Relation),
		SubjectType:  authz.SubjectType(entity.SubjectType),
		SubjectID:    entity.SubjectID,
		GrantedBy:    entity.GrantedBy,
		ExpiresAt:    entity.ExpiresAt,
	}
	if entity.CreateTime != nil {
		tuple.CreateTime = *entity.CreateTime
	}
	return tuple
}

// ToProto converts an ent.TemplatePermission to proto NotificationPermission
func (r *PermissionRepo) ToProto(entity *ent.TemplatePermission) *notificationpb.NotificationPermission {
	if entity == nil {
		return nil
	}

	proto := &notificationpb.NotificationPermission{
		Id:         uint32(entity.ID),
		TenantId:   derefUint32(entity.TenantID),
		ResourceId: entity.ResourceID,
		SubjectId:  entity.SubjectID,
	}

	// Map resource type
	switch entity.ResourceType {
	case templatepermission.ResourceTypeRESOURCE_TYPE_TEMPLATE:
		proto.ResourceType = notificationpb.ResourceType_RESOURCE_TYPE_TEMPLATE
	case templatepermission.ResourceTypeRESOURCE_TYPE_CHANNEL:
		proto.ResourceType = notificationpb.ResourceType_RESOURCE_TYPE_CHANNEL
	default:
		proto.ResourceType = notificationpb.ResourceType_RESOURCE_TYPE_UNSPECIFIED
	}

	// Map relation
	switch entity.Relation {
	case templatepermission.RelationRELATION_OWNER:
		proto.Relation = notificationpb.Relation_RELATION_OWNER
	case templatepermission.RelationRELATION_EDITOR:
		proto.Relation = notificationpb.Relation_RELATION_EDITOR
	case templatepermission.RelationRELATION_VIEWER:
		proto.Relation = notificationpb.Relation_RELATION_VIEWER
	case templatepermission.RelationRELATION_SHARER:
		proto.Relation = notificationpb.Relation_RELATION_SHARER
	default:
		proto.Relation = notificationpb.Relation_RELATION_UNSPECIFIED
	}

	// Map subject type
	switch entity.SubjectType {
	case templatepermission.SubjectTypeSUBJECT_TYPE_USER:
		proto.SubjectType = notificationpb.SubjectType_SUBJECT_TYPE_USER
	case templatepermission.SubjectTypeSUBJECT_TYPE_ROLE:
		proto.SubjectType = notificationpb.SubjectType_SUBJECT_TYPE_ROLE
	case templatepermission.SubjectTypeSUBJECT_TYPE_CLIENT:
		proto.SubjectType = notificationpb.SubjectType_SUBJECT_TYPE_CLIENT
	default:
		proto.SubjectType = notificationpb.SubjectType_SUBJECT_TYPE_UNSPECIFIED
	}

	if entity.GrantedBy != nil {
		proto.GrantedBy = entity.GrantedBy
	}
	if entity.ExpiresAt != nil {
		proto.ExpiresAt = timestamppb.New(*entity.ExpiresAt)
	}
	if entity.CreateTime != nil && !entity.CreateTime.IsZero() {
		proto.CreateTime = timestamppb.New(*entity.CreateTime)
	}

	return proto
}
