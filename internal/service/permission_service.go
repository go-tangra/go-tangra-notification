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
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/templatepermission"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type PermissionService struct {
	notificationpb.UnimplementedNotificationPermissionServiceServer

	log            *log.Helper
	permissionRepo *data.PermissionRepo
	checker        *authz.Checker
	engine         *authz.Engine
}

func NewPermissionService(
	ctx *bootstrap.Context,
	permissionRepo *data.PermissionRepo,
	checker *authz.Checker,
	engine *authz.Engine,
) *PermissionService {
	return &PermissionService{
		log:            ctx.NewLoggerHelper("notification/service/permission"),
		permissionRepo: permissionRepo,
		checker:        checker,
		engine:         engine,
	}
}

func (s *PermissionService) GrantAccess(ctx context.Context, req *notificationpb.GrantAccessRequest) (*notificationpb.GrantAccessResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	// M12: Reject zero tenant_id at service layer (platform admins exempt)
	if tenantID == 0 && !isPlatformAdmin(ctx) {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}
	grantedBy := getUserIDAsUint32(ctx)

	// M8: Validate resource and subject ID formats
	if !uuidPattern.MatchString(req.ResourceId) {
		return nil, notificationpb.ErrorBadRequest("invalid resource ID format")
	}
	if len(req.SubjectId) == 0 || len(req.SubjectId) > 255 {
		return nil, notificationpb.ErrorBadRequest("invalid subject ID")
	}

	resourceType := protoResourceTypeToAuthz(req.ResourceType)

	// Check that the granting user has SHARE permission on the resource
	if grantedBy == nil {
		return nil, notificationpb.ErrorAccessDenied("user identity required to grant access")
	}
	checkResult := s.engine.Check(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    uintToString(grantedBy),
		SubjectType:  authz.SubjectTypeUser,
		ResourceType: resourceType,
		ResourceID:   req.ResourceId,
		Permission:   authz.PermissionShare,
	})
	if !checkResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to share this resource")
	}

	requestedRelation := protoRelationToAuthz(req.Relation)

	// H1: Prevent privilege escalation — granter's highest relation must be a
	// superset of the requested relation's permissions. This prevents an Editor
	// from granting Sharer (which has SHARE permission that Editor lacks).
	_, granterHighest := s.engine.GetEffectivePermissions(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    uintToString(grantedBy),
		SubjectType:  authz.SubjectTypeUser,
		ResourceType: resourceType,
		ResourceID:   req.ResourceId,
	})
	if !authz.RelationPermissionsAreSuperset(granterHighest, requestedRelation) {
		return nil, notificationpb.ErrorAccessDenied("cannot grant a relation whose permissions exceed your own")
	}

	// M13: Wildcard client grants (subject_id="*") require OWNER
	subjectType := protoSubjectTypeToAuthz(req.SubjectType)
	if subjectType == authz.SubjectTypeClient && req.SubjectId == authz.WildcardSubjectID {
		if granterHighest != authz.RelationOwner {
			return nil, notificationpb.ErrorAccessDenied("only owners can create wildcard client grants")
		}
	}

	tuple := authz.PermissionTuple{
		TenantID:     tenantID,
		ResourceType: resourceType,
		ResourceID:   req.ResourceId,
		Relation:     requestedRelation,
		SubjectType:  subjectType,
		SubjectID:    req.SubjectId,
		GrantedBy:    grantedBy,
	}

	if req.ExpiresAt != nil {
		t := req.ExpiresAt.AsTime()
		// M2: Reject expiration timestamps in the past
		if t.Before(time.Now()) {
			return nil, notificationpb.ErrorBadRequest("expires_at must be in the future")
		}
		tuple.ExpiresAt = &t
	}

	result, err := s.engine.Grant(ctx, tuple)
	if err != nil {
		return nil, err
	}

	return &notificationpb.GrantAccessResponse{
		Permission: tupleToProto(result),
	}, nil
}

func (s *PermissionService) RevokeAccess(ctx context.Context, req *notificationpb.RevokeAccessRequest) (*emptypb.Empty, error) {
	tenantID := getTenantIDFromContext(ctx)
	if tenantID == 0 && !isPlatformAdmin(ctx) {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}
	userID := getUserIDAsUint32(ctx)

	// M8: Validate resource and subject ID formats
	if !uuidPattern.MatchString(req.ResourceId) {
		return nil, notificationpb.ErrorBadRequest("invalid resource ID format")
	}
	if len(req.SubjectId) == 0 || len(req.SubjectId) > 255 {
		return nil, notificationpb.ErrorBadRequest("invalid subject ID")
	}

	resourceType := protoResourceTypeToAuthz(req.ResourceType)

	// Check that the revoking user has SHARE permission
	if userID == nil {
		return nil, notificationpb.ErrorAccessDenied("user identity required to revoke access")
	}
	revokeResult := s.engine.Check(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    uintToString(userID),
		SubjectType:  authz.SubjectTypeUser,
		ResourceType: resourceType,
		ResourceID:   req.ResourceId,
		Permission:   authz.PermissionShare,
	})
	if !revokeResult.Allowed {
		return nil, notificationpb.ErrorAccessDenied("you do not have permission to manage access for this resource")
	}

	// H2: Prevent privilege escalation — revoker cannot revoke a relation
	// higher than their own highest relation on the resource
	_, revokerHighest := s.engine.GetEffectivePermissions(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    uintToString(userID),
		SubjectType:  authz.SubjectTypeUser,
		ResourceType: resourceType,
		ResourceID:   req.ResourceId,
	})

	var relation *authz.Relation
	if req.Relation != nil {
		r := protoRelationToAuthz(*req.Relation)
		relation = &r
		// When revoking a specific relation, check the revoker outranks it
		if !authz.IsRelationAtLeast(revokerHighest, r) {
			return nil, notificationpb.ErrorAccessDenied("cannot revoke a relation higher than your own")
		}
	} else {
		// When revoking all relations (relation=nil), require OWNER
		if revokerHighest != authz.RelationOwner {
			return nil, notificationpb.ErrorAccessDenied("only owners can revoke all relations")
		}
	}

	// M1: Prevent revoking the last OWNER — would orphan the resource
	targetSubjectType := protoSubjectTypeToAuthz(req.SubjectType)
	wouldRemoveOwner := (relation != nil && *relation == authz.RelationOwner) || relation == nil
	if wouldRemoveOwner {
		tuples, err := s.engine.ListPermissions(ctx, tenantID, resourceType, req.ResourceId)
		if err != nil {
			s.log.Errorf("failed to list permissions for orphan check: %v", err)
			return nil, notificationpb.ErrorInternalServerError("failed to revoke access")
		}
		ownerCount := 0
		isTargetOwner := false
		for _, t := range tuples {
			if t.Relation == authz.RelationOwner {
				ownerCount++
				if t.SubjectType == targetSubjectType && t.SubjectID == req.SubjectId {
					isTargetOwner = true
				}
			}
		}
		if isTargetOwner && ownerCount <= 1 {
			return nil, notificationpb.ErrorBadRequest("cannot revoke the last owner — resource would be orphaned")
		}
	}

	if err := s.engine.Revoke(ctx, tenantID, resourceType, req.ResourceId, relation, targetSubjectType, req.SubjectId); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *PermissionService) ListPermissions(ctx context.Context, req *notificationpb.ListPermissionsRequest) (*notificationpb.ListPermissionsResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	if tenantID == 0 && !isPlatformAdmin(ctx) {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}
	userID := getUserIDAsUint32(ctx)
	if userID == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	// M8: Validate resource ID format
	if !uuidPattern.MatchString(req.ResourceId) {
		return nil, notificationpb.ErrorBadRequest("invalid resource ID format")
	}

	resourceType := protoResourceTypeToAuthz(req.ResourceType)

	// Platform admins bypass per-resource permission checks
	if !isPlatformAdmin(ctx) {
		// Require at least READ permission on the resource to list its permissions
		readResult := s.engine.Check(ctx, authz.CheckContext{
			TenantID:     tenantID,
			SubjectID:    fmt.Sprintf("%d", *userID),
			SubjectType:  authz.SubjectTypeUser,
			ResourceType: resourceType,
			ResourceID:   req.ResourceId,
			Permission:   authz.PermissionRead,
		})
		if !readResult.Allowed {
			return nil, notificationpb.ErrorAccessDenied("you do not have permission to view this resource's permissions")
		}
	}

	var subjectType *templatepermission.SubjectType
	if req.SubjectType != nil && *req.SubjectType != notificationpb.SubjectType_SUBJECT_TYPE_UNSPECIFIED {
		st := templatepermission.SubjectType(protoSubjectTypeToAuthz(*req.SubjectType))
		subjectType = &st
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

	entities, total, err := s.permissionRepo.ListByResource(ctx, tenantID, templatepermission.ResourceType(resourceType), req.ResourceId, subjectType, req.SubjectId, page, pageSize)
	if err != nil {
		return nil, err
	}

	permissions := make([]*notificationpb.NotificationPermission, 0, len(entities))
	for _, e := range entities {
		permissions = append(permissions, s.permissionRepo.ToProto(e))
	}

	return &notificationpb.ListPermissionsResponse{
		Permissions: permissions,
		Total:       uint32(total),
	}, nil
}

func (s *PermissionService) CheckAccess(ctx context.Context, req *notificationpb.CheckAccessRequest) (*notificationpb.CheckAccessResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	if tenantID == 0 && !isPlatformAdmin(ctx) {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}

	callerID := getUserIDAsUint32(ctx)
	if callerID == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	// H5: Restrict permission probing for all subject types
	if req.SubjectType == notificationpb.SubjectType_SUBJECT_TYPE_USER {
		// Users can only check their own permissions
		if req.SubjectId != fmt.Sprintf("%d", *callerID) {
			return nil, notificationpb.ErrorAccessDenied("cannot check permissions for other users")
		}
	} else {
		// For ROLE/CLIENT subject types, require SHARE permission on the resource
		shareResult := s.engine.Check(ctx, authz.CheckContext{
			TenantID:     tenantID,
			SubjectID:    fmt.Sprintf("%d", *callerID),
			SubjectType:  authz.SubjectTypeUser,
			ResourceType: protoResourceTypeToAuthz(req.ResourceType),
			ResourceID:   req.ResourceId,
			Permission:   authz.PermissionShare,
		})
		if !shareResult.Allowed {
			return nil, notificationpb.ErrorAccessDenied("you do not have permission to check access for this resource")
		}
	}

	result := s.engine.Check(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    req.SubjectId,
		SubjectType:  protoSubjectTypeToAuthz(req.SubjectType),
		ResourceType: protoResourceTypeToAuthz(req.ResourceType),
		ResourceID:   req.ResourceId,
		Permission:   protoPermissionActionToAuthz(req.Permission),
	})

	resp := &notificationpb.CheckAccessResponse{
		Allowed: result.Allowed,
		Reason:  result.Reason,
	}

	if result.Relation != nil {
		resp.Relation = authzRelationToProto(*result.Relation)
	}

	return resp, nil
}

func (s *PermissionService) GetEffectivePermissions(ctx context.Context, req *notificationpb.GetEffectivePermissionsRequest) (*notificationpb.GetEffectivePermissionsResponse, error) {
	tenantID := getTenantIDFromContext(ctx)
	if tenantID == 0 && !isPlatformAdmin(ctx) {
		return nil, notificationpb.ErrorAccessDenied("tenant context required")
	}

	callerID := getUserIDAsUint32(ctx)
	if callerID == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	// H6: Restrict permission probing for all subject types
	if req.SubjectType == notificationpb.SubjectType_SUBJECT_TYPE_USER {
		if req.SubjectId != fmt.Sprintf("%d", *callerID) {
			return nil, notificationpb.ErrorAccessDenied("cannot check permissions for other users")
		}
	} else {
		// For ROLE/CLIENT subject types, require SHARE permission on the resource
		shareResult := s.engine.Check(ctx, authz.CheckContext{
			TenantID:     tenantID,
			SubjectID:    fmt.Sprintf("%d", *callerID),
			SubjectType:  authz.SubjectTypeUser,
			ResourceType: protoResourceTypeToAuthz(req.ResourceType),
			ResourceID:   req.ResourceId,
			Permission:   authz.PermissionShare,
		})
		if !shareResult.Allowed {
			return nil, notificationpb.ErrorAccessDenied("you do not have permission to check access for this resource")
		}
	}

	permissions, highestRelation := s.engine.GetEffectivePermissions(ctx, authz.CheckContext{
		TenantID:     tenantID,
		SubjectID:    req.SubjectId,
		SubjectType:  protoSubjectTypeToAuthz(req.SubjectType),
		ResourceType: protoResourceTypeToAuthz(req.ResourceType),
		ResourceID:   req.ResourceId,
	})

	protoPerms := make([]notificationpb.PermissionAction, 0, len(permissions))
	for _, p := range permissions {
		protoPerms = append(protoPerms, authzPermissionToProto(p))
	}

	return &notificationpb.GetEffectivePermissionsResponse{
		Permissions:     protoPerms,
		HighestRelation: authzRelationToProto(highestRelation),
	}, nil
}

// Conversion helpers

func protoResourceTypeToAuthz(rt notificationpb.ResourceType) authz.ResourceType {
	switch rt {
	case notificationpb.ResourceType_RESOURCE_TYPE_TEMPLATE:
		return authz.ResourceTypeTemplate
	case notificationpb.ResourceType_RESOURCE_TYPE_CHANNEL:
		return authz.ResourceTypeChannel
	default:
		return authz.ResourceTypeTemplate
	}
}

func protoRelationToAuthz(r notificationpb.Relation) authz.Relation {
	switch r {
	case notificationpb.Relation_RELATION_OWNER:
		return authz.RelationOwner
	case notificationpb.Relation_RELATION_EDITOR:
		return authz.RelationEditor
	case notificationpb.Relation_RELATION_VIEWER:
		return authz.RelationViewer
	case notificationpb.Relation_RELATION_SHARER:
		return authz.RelationSharer
	default:
		return authz.RelationViewer
	}
}

func protoSubjectTypeToAuthz(st notificationpb.SubjectType) authz.SubjectType {
	switch st {
	case notificationpb.SubjectType_SUBJECT_TYPE_USER:
		return authz.SubjectTypeUser
	case notificationpb.SubjectType_SUBJECT_TYPE_ROLE:
		return authz.SubjectTypeRole
	case notificationpb.SubjectType_SUBJECT_TYPE_CLIENT:
		return authz.SubjectTypeClient
	default:
		return authz.SubjectTypeUser
	}
}

func protoPermissionActionToAuthz(p notificationpb.PermissionAction) authz.Permission {
	switch p {
	case notificationpb.PermissionAction_PERMISSION_ACTION_READ:
		return authz.PermissionRead
	case notificationpb.PermissionAction_PERMISSION_ACTION_WRITE:
		return authz.PermissionWrite
	case notificationpb.PermissionAction_PERMISSION_ACTION_DELETE:
		return authz.PermissionDelete
	case notificationpb.PermissionAction_PERMISSION_ACTION_SHARE:
		return authz.PermissionShare
	case notificationpb.PermissionAction_PERMISSION_ACTION_USE:
		return authz.PermissionUse
	default:
		return authz.PermissionRead
	}
}

func authzRelationToProto(r authz.Relation) notificationpb.Relation {
	switch r {
	case authz.RelationOwner:
		return notificationpb.Relation_RELATION_OWNER
	case authz.RelationEditor:
		return notificationpb.Relation_RELATION_EDITOR
	case authz.RelationViewer:
		return notificationpb.Relation_RELATION_VIEWER
	case authz.RelationSharer:
		return notificationpb.Relation_RELATION_SHARER
	default:
		return notificationpb.Relation_RELATION_UNSPECIFIED
	}
}

func authzPermissionToProto(p authz.Permission) notificationpb.PermissionAction {
	switch p {
	case authz.PermissionRead:
		return notificationpb.PermissionAction_PERMISSION_ACTION_READ
	case authz.PermissionWrite:
		return notificationpb.PermissionAction_PERMISSION_ACTION_WRITE
	case authz.PermissionDelete:
		return notificationpb.PermissionAction_PERMISSION_ACTION_DELETE
	case authz.PermissionShare:
		return notificationpb.PermissionAction_PERMISSION_ACTION_SHARE
	case authz.PermissionUse:
		return notificationpb.PermissionAction_PERMISSION_ACTION_USE
	default:
		return notificationpb.PermissionAction_PERMISSION_ACTION_UNSPECIFIED
	}
}

func authzResourceTypeToProto(rt authz.ResourceType) notificationpb.ResourceType {
	switch rt {
	case authz.ResourceTypeTemplate:
		return notificationpb.ResourceType_RESOURCE_TYPE_TEMPLATE
	case authz.ResourceTypeChannel:
		return notificationpb.ResourceType_RESOURCE_TYPE_CHANNEL
	default:
		return notificationpb.ResourceType_RESOURCE_TYPE_UNSPECIFIED
	}
}

func tupleToProto(tuple *authz.PermissionTuple) *notificationpb.NotificationPermission {
	if tuple == nil {
		return nil
	}
	return &notificationpb.NotificationPermission{
		Id:           tuple.ID,
		TenantId:     tuple.TenantID,
		ResourceType: authzResourceTypeToProto(tuple.ResourceType),
		ResourceId:   tuple.ResourceID,
		Relation:     authzRelationToProto(tuple.Relation),
		SubjectType:  authzSubjectTypeToProto(tuple.SubjectType),
		SubjectId:    tuple.SubjectID,
		GrantedBy:    tuple.GrantedBy,
	}
}

func authzSubjectTypeToProto(st authz.SubjectType) notificationpb.SubjectType {
	switch st {
	case authz.SubjectTypeUser:
		return notificationpb.SubjectType_SUBJECT_TYPE_USER
	case authz.SubjectTypeRole:
		return notificationpb.SubjectType_SUBJECT_TYPE_ROLE
	case authz.SubjectTypeClient:
		return notificationpb.SubjectType_SUBJECT_TYPE_CLIENT
	default:
		return notificationpb.SubjectType_SUBJECT_TYPE_UNSPECIFIED
	}
}

func uintToString(v *uint32) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}
