package authz

import (
	"context"
	"fmt"
)

// Checker provides a simplified interface for permission checks
type Checker struct {
	engine *Engine
}

// NewChecker creates a new permission checker
func NewChecker(engine *Engine) *Checker {
	return &Checker{engine: engine}
}

// CanRead checks if a user can read a template
func (c *Checker) CanRead(ctx context.Context, tenantID uint32, userID string, templateID string) error {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    userID,
		SubjectType:  SubjectTypeUser,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
		Permission:   PermissionRead,
	})
	if !result.Allowed {
		return fmt.Errorf("access denied: %s", result.Reason)
	}
	return nil
}

// CanWrite checks if a user can write to a template
func (c *Checker) CanWrite(ctx context.Context, tenantID uint32, userID string, templateID string) error {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    userID,
		SubjectType:  SubjectTypeUser,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
		Permission:   PermissionWrite,
	})
	if !result.Allowed {
		return fmt.Errorf("access denied: %s", result.Reason)
	}
	return nil
}

// CanDelete checks if a user can delete a template
func (c *Checker) CanDelete(ctx context.Context, tenantID uint32, userID string, templateID string) error {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    userID,
		SubjectType:  SubjectTypeUser,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
		Permission:   PermissionDelete,
	})
	if !result.Allowed {
		return fmt.Errorf("access denied: %s", result.Reason)
	}
	return nil
}

// CanShare checks if a user can share a template
func (c *Checker) CanShare(ctx context.Context, tenantID uint32, userID string, templateID string) error {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    userID,
		SubjectType:  SubjectTypeUser,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
		Permission:   PermissionShare,
	})
	if !result.Allowed {
		return fmt.Errorf("access denied: %s", result.Reason)
	}
	return nil
}

// CanUse checks if a user can use (render/send with) a template
func (c *Checker) CanUse(ctx context.Context, tenantID uint32, userID string, templateID string) error {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    userID,
		SubjectType:  SubjectTypeUser,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
		Permission:   PermissionUse,
	})
	if !result.Allowed {
		return fmt.Errorf("access denied: %s", result.Reason)
	}
	return nil
}

// CanClientUse checks if an mTLS client can use (render/send with) a template
func (c *Checker) CanClientUse(ctx context.Context, tenantID uint32, clientID string, templateID string) error {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    clientID,
		SubjectType:  SubjectTypeClient,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
		Permission:   PermissionUse,
	})
	if !result.Allowed {
		return fmt.Errorf("access denied: client %s cannot use template %s: %s", clientID, templateID, result.Reason)
	}
	return nil
}

// CanClientUseChannel checks if an mTLS client can use a channel
func (c *Checker) CanClientUseChannel(ctx context.Context, tenantID uint32, clientID string, channelID string) error {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    clientID,
		SubjectType:  SubjectTypeClient,
		ResourceType: ResourceTypeChannel,
		ResourceID:   channelID,
		Permission:   PermissionUse,
	})
	if !result.Allowed {
		return fmt.Errorf("access denied: client %s cannot use channel %s: %s", clientID, channelID, result.Reason)
	}
	return nil
}

// ListClientAccessibleChannels lists all channels accessible by an mTLS client
func (c *Checker) ListClientAccessibleChannels(ctx context.Context, tenantID uint32, clientID string) ([]string, error) {
	return c.engine.ListClientAccessibleResources(ctx, tenantID, clientID, ResourceTypeChannel)
}

// CheckPermission checks if a user has a specific permission on a template
func (c *Checker) CheckPermission(ctx context.Context, tenantID uint32, userID string, templateID string, permission Permission) (bool, string) {
	result := c.engine.Check(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    userID,
		SubjectType:  SubjectTypeUser,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
		Permission:   permission,
	})
	return result.Allowed, result.Reason
}

// RequirePermission checks if a user has a specific permission and returns an error if not
func (c *Checker) RequirePermission(ctx context.Context, tenantID uint32, userID string, templateID string, permission Permission) error {
	allowed, reason := c.CheckPermission(ctx, tenantID, userID, templateID, permission)
	if !allowed {
		return fmt.Errorf("access denied: %s", reason)
	}
	return nil
}

// GetEffectivePermissions returns all effective permissions for a user on a template
func (c *Checker) GetEffectivePermissions(ctx context.Context, tenantID uint32, userID string, templateID string) ([]Permission, Relation) {
	return c.engine.GetEffectivePermissions(ctx, CheckContext{
		TenantID:     tenantID,
		SubjectID:    userID,
		SubjectType:  SubjectTypeUser,
		ResourceType: ResourceTypeTemplate,
		ResourceID:   templateID,
	})
}

// ListAccessibleTemplates lists all templates accessible by a user
func (c *Checker) ListAccessibleTemplates(ctx context.Context, tenantID uint32, userID string) ([]string, error) {
	return c.engine.ListAccessibleResources(ctx, tenantID, userID, ResourceTypeTemplate, PermissionRead)
}

// ListClientAccessibleTemplates lists all templates accessible by an mTLS client
func (c *Checker) ListClientAccessibleTemplates(ctx context.Context, tenantID uint32, clientID string) ([]string, error) {
	return c.engine.ListClientAccessibleResources(ctx, tenantID, clientID, ResourceTypeTemplate)
}
