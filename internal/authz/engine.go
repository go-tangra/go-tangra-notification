package authz

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// PermissionTuple represents a permission relationship in the system
type PermissionTuple struct {
	ID           uint32
	TenantID     uint32
	ResourceType ResourceType
	ResourceID   string
	Relation     Relation
	SubjectType  SubjectType
	SubjectID    string
	GrantedBy    *uint32
	ExpiresAt    *time.Time
	CreateTime   time.Time
}

// ResourceLookup provides methods to look up user role memberships
type ResourceLookup interface {
	// GetUserRoleIDs returns the role IDs for a user
	GetUserRoleIDs(ctx context.Context, tenantID uint32, userID string) ([]string, error)
}

// PermissionStore provides methods to store and retrieve permissions
type PermissionStore interface {
	// GetDirectPermissions returns permissions directly on a resource
	GetDirectPermissions(ctx context.Context, tenantID uint32, resourceType ResourceType, resourceID string) ([]PermissionTuple, error)
	// GetSubjectPermissions returns all permissions for a subject
	GetSubjectPermissions(ctx context.Context, tenantID uint32, subjectType SubjectType, subjectID string) ([]PermissionTuple, error)
	// HasPermission checks if a specific permission exists
	HasPermission(ctx context.Context, tenantID uint32, resourceType ResourceType, resourceID string, subjectType SubjectType, subjectID string) (*PermissionTuple, error)
	// CreatePermission creates a new permission
	CreatePermission(ctx context.Context, tuple PermissionTuple) (*PermissionTuple, error)
	// DeletePermission deletes a permission
	DeletePermission(ctx context.Context, tenantID uint32, resourceType ResourceType, resourceID string, relation *Relation, subjectType SubjectType, subjectID string) error
	// ListResourcesBySubject lists resources accessible by a subject
	ListResourcesBySubject(ctx context.Context, tenantID uint32, subjectType SubjectType, subjectID string, resourceType ResourceType) ([]string, error)
}

// Engine implements Zanzibar-like permission checking for notification templates
type Engine struct {
	store  PermissionStore
	lookup ResourceLookup
	log    *log.Helper
}

// NewEngine creates a new authorization engine
func NewEngine(store PermissionStore, lookup ResourceLookup, logger log.Logger) *Engine {
	return &Engine{
		store:  store,
		lookup: lookup,
		log:    log.NewHelper(log.With(logger, "module", "authz/engine")),
	}
}

// CheckContext contains context for permission checks
type CheckContext struct {
	TenantID     uint32
	SubjectID    string
	SubjectType  SubjectType
	ResourceType ResourceType
	ResourceID   string
	Permission   Permission
}

// CheckResult represents the result of a permission check
type CheckResult struct {
	Allowed  bool
	Relation *Relation
	Reason   string
}

// Check performs a permission check following simplified Zanzibar algorithm:
// For CLIENT subjects:
//  1. Check direct client permission on resource
//
// For USER subjects:
//  1. Check direct user permission on resource
//  2. Check user's role permissions on resource
//
// No hierarchy traversal (templates are flat resources).
func (e *Engine) Check(ctx context.Context, check CheckContext) CheckResult {
	// For CLIENT subjects: check direct client permission, then wildcard (*)
	if check.SubjectType == SubjectTypeClient {
		if result := e.checkDirectPermission(ctx, check, SubjectTypeClient, check.SubjectID); result.Allowed {
			return result
		}
		// Check wildcard grant (subject_id = "*" means all clients)
		if check.SubjectID != WildcardSubjectID {
			if result := e.checkDirectPermission(ctx, check, SubjectTypeClient, WildcardSubjectID); result.Allowed {
				return result
			}
		}
		return CheckResult{
			Allowed: false,
			Reason:  "no client permission found",
		}
	}

	// For USER subjects:

	// Step 1: Check direct user permission on resource
	if result := e.checkDirectPermission(ctx, check, SubjectTypeUser, check.SubjectID); result.Allowed {
		return result
	}

	// Step 2: Check user's role permissions on resource
	roleIDs, err := e.lookup.GetUserRoleIDs(ctx, check.TenantID, check.SubjectID)
	if err != nil {
		e.log.Warnf("Failed to get user roles: %v", err)
	} else {
		for _, roleID := range roleIDs {
			if result := e.checkDirectPermission(ctx, check, SubjectTypeRole, roleID); result.Allowed {
				return result
			}
		}
	}

	return CheckResult{
		Allowed: false,
		Reason:  "no permission found",
	}
}

// checkDirectPermission checks for a direct permission on a resource
func (e *Engine) checkDirectPermission(ctx context.Context, check CheckContext, subjectType SubjectType, subjectID string) CheckResult {
	tuple, err := e.store.HasPermission(ctx, check.TenantID, check.ResourceType, check.ResourceID, subjectType, subjectID)
	if err != nil {
		e.log.Warnf("Error checking permission: %v", err)
		return CheckResult{Allowed: false, Reason: "error checking permission"}
	}

	if tuple == nil {
		return CheckResult{Allowed: false, Reason: "no direct permission"}
	}

	// Check if permission has expired
	if tuple.ExpiresAt != nil && tuple.ExpiresAt.Before(time.Now()) {
		return CheckResult{Allowed: false, Reason: "permission expired"}
	}

	// Check if the relation grants the required permission
	if RelationGrantsPermission(tuple.Relation, check.Permission) {
		relation := tuple.Relation
		return CheckResult{
			Allowed:  true,
			Relation: &relation,
			Reason:   "direct permission",
		}
	}

	return CheckResult{Allowed: false, Reason: "relation does not grant permission"}
}

// Grant grants a permission to a subject
func (e *Engine) Grant(ctx context.Context, tuple PermissionTuple) (*PermissionTuple, error) {
	return e.store.CreatePermission(ctx, tuple)
}

// Revoke revokes a permission from a subject
func (e *Engine) Revoke(ctx context.Context, tenantID uint32, resourceType ResourceType, resourceID string, relation *Relation, subjectType SubjectType, subjectID string) error {
	return e.store.DeletePermission(ctx, tenantID, resourceType, resourceID, relation, subjectType, subjectID)
}

// ListPermissions lists all permissions on a resource
func (e *Engine) ListPermissions(ctx context.Context, tenantID uint32, resourceType ResourceType, resourceID string) ([]PermissionTuple, error) {
	return e.store.GetDirectPermissions(ctx, tenantID, resourceType, resourceID)
}

// ListAccessibleResources lists all resources of a type accessible by a user
// for the given permission. It collects candidate resources from direct and role
// grants, then post-filters to only include those whose relation actually grants
// the requested permission.
func (e *Engine) ListAccessibleResources(ctx context.Context, tenantID uint32, userID string, resourceType ResourceType, permission Permission) ([]string, error) {
	// Collect candidate resource IDs with their granting relations
	type candidate struct {
		subjectType SubjectType
		subjectID   string
	}
	candidates := make(map[string][]candidate)

	// Get user's direct permissions
	userPerms, err := e.store.GetSubjectPermissions(ctx, tenantID, SubjectTypeUser, userID)
	if err != nil {
		return nil, err
	}
	for _, tuple := range userPerms {
		if tuple.ResourceType == resourceType && RelationGrantsPermission(tuple.Relation, permission) {
			if tuple.ExpiresAt == nil || tuple.ExpiresAt.After(time.Now()) {
				candidates[tuple.ResourceID] = append(candidates[tuple.ResourceID], candidate{SubjectTypeUser, userID})
			}
		}
	}

	// Get user's role permissions
	roleIDs, err := e.lookup.GetUserRoleIDs(ctx, tenantID, userID)
	if err != nil {
		e.log.Warnf("Failed to get user roles: %v", err)
	} else {
		for _, roleID := range roleIDs {
			rolePerms, err := e.store.GetSubjectPermissions(ctx, tenantID, SubjectTypeRole, roleID)
			if err != nil {
				continue
			}
			for _, tuple := range rolePerms {
				if tuple.ResourceType == resourceType && RelationGrantsPermission(tuple.Relation, permission) {
					if tuple.ExpiresAt == nil || tuple.ExpiresAt.After(time.Now()) {
						candidates[tuple.ResourceID] = append(candidates[tuple.ResourceID], candidate{SubjectTypeRole, roleID})
					}
				}
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(candidates))
	for id := range candidates {
		result = append(result, id)
	}

	return result, nil
}

// ListClientAccessibleResources lists all resources of a type accessible by a client.
// M1: Also includes resources granted via wildcard (subject_id="*") grants.
func (e *Engine) ListClientAccessibleResources(ctx context.Context, tenantID uint32, clientID string, resourceType ResourceType) ([]string, error) {
	directIDs, err := e.store.ListResourcesBySubject(ctx, tenantID, SubjectTypeClient, clientID, resourceType)
	if err != nil {
		return nil, err
	}

	// Also fetch wildcard grants that apply to all clients
	if clientID != WildcardSubjectID {
		wildcardIDs, err := e.store.ListResourcesBySubject(ctx, tenantID, SubjectTypeClient, WildcardSubjectID, resourceType)
		if err != nil {
			e.log.Warnf("Failed to fetch wildcard client grants: %v", err)
		} else {
			// Merge and deduplicate
			seen := make(map[string]bool, len(directIDs))
			for _, id := range directIDs {
				seen[id] = true
			}
			for _, id := range wildcardIDs {
				if !seen[id] {
					directIDs = append(directIDs, id)
				}
			}
		}
	}

	return directIDs, nil
}

// GetEffectivePermissions returns all permissions a subject has on a resource
func (e *Engine) GetEffectivePermissions(ctx context.Context, check CheckContext) ([]Permission, Relation) {
	var highestRelation Relation
	permissions := make(map[Permission]bool)

	// Check each permission type
	for _, perm := range AllPermissions {
		checkWithPerm := check
		checkWithPerm.Permission = perm
		result := e.Check(ctx, checkWithPerm)
		if result.Allowed {
			permissions[perm] = true
			if result.Relation != nil && IsRelationAtLeast(*result.Relation, highestRelation) {
				highestRelation = *result.Relation
			}
		}
	}

	// Convert map to slice
	result := make([]Permission, 0, len(permissions))
	for perm := range permissions {
		result = append(result, perm)
	}

	return result, highestRelation
}
