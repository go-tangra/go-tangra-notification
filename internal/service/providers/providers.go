package providers

import (
	"context"
	"strings"

	"github.com/go-kratos/kratos/v2/metadata"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-notification/internal/authz"
	"github.com/go-tangra/go-tangra-notification/internal/data"
)

// ProvideResourceLookup creates a ResourceLookup for the notification service
func ProvideResourceLookup() authz.ResourceLookup {
	return &resourceLookupImpl{}
}

// ProvidePermissionStore creates a PermissionStore from the permission repo
func ProvidePermissionStore(permRepo *data.PermissionRepo) authz.PermissionStore {
	return permRepo
}

// ProvideAuthzEngine creates the authorization engine
func ProvideAuthzEngine(store authz.PermissionStore, lookup authz.ResourceLookup, ctx *bootstrap.Context) *authz.Engine {
	return authz.NewEngine(store, lookup, ctx.GetLogger())
}

// ProvideAuthzChecker creates the authorization checker
func ProvideAuthzChecker(engine *authz.Engine) *authz.Checker {
	return authz.NewChecker(engine)
}

// resourceLookupImpl implements authz.ResourceLookup
type resourceLookupImpl struct{}

func (r *resourceLookupImpl) GetUserRoleIDs(ctx context.Context, tenantID uint32, userID string) ([]string, error) {
	md, ok := metadata.FromServerContext(ctx)
	if !ok {
		return nil, nil
	}

	rolesStr := md.Get("x-md-global-roles")
	if rolesStr == "" {
		return nil, nil
	}

	var roles []string
	for _, role := range strings.Split(rolesStr, ",") {
		role = strings.TrimSpace(role)
		if role != "" {
			roles = append(roles, role)
		}
	}

	return roles, nil
}
