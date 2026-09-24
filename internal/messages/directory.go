package messages

import (
	"context"

	authv1 "github.com/go-tangra/go-tangra-auth/sdk/v4/api/proto/auth/v1"
)

// AuthDirectory resolves members through the auth service's Profiles RPCs
// (contracts/auth-changes.md).
type AuthDirectory struct {
	Client authv1.ProfilesClient
}

// Lookup returns the active members among ids (1,000 per call).
func (d AuthDirectory) Lookup(ctx context.Context, tenantID string, ids []string) ([]string, error) {
	var out []string
	for i := 0; i < len(ids); i += 1000 {
		end := min(i+1000, len(ids))
		res, err := d.Client.ListMembers(ctx, &authv1.ListMembersRequest{TenantId: tenantID, UserIds: ids[i:end]})
		if err != nil {
			return nil, err
		}
		out = append(out, res.GetUserIds()...)
	}
	return out, nil
}

// Members pages every active member id through fn.
func (d AuthDirectory) Members(ctx context.Context, tenantID string, fn func(ids []string) error) error {
	cursor := ""
	for {
		res, err := d.Client.ListMembers(ctx, &authv1.ListMembersRequest{TenantId: tenantID, Cursor: cursor, Limit: 1000})
		if err != nil {
			return err
		}
		if len(res.GetUserIds()) > 0 {
			if err := fn(res.GetUserIds()); err != nil {
				return err
			}
		}
		cursor = res.GetNextCursor()
		if cursor == "" {
			return nil
		}
	}
}

// StaticDirectory is an in-memory directory (tests).
type StaticDirectory struct {
	Active map[string]bool
	Err    error
}

// Lookup implements Directory.
func (d StaticDirectory) Lookup(_ context.Context, _ string, ids []string) ([]string, error) {
	if d.Err != nil {
		return nil, d.Err
	}
	var out []string
	for _, id := range ids {
		if d.Active[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// Members implements Directory (sorted ids, pages of 1000).
func (d StaticDirectory) Members(_ context.Context, _ string, fn func(ids []string) error) error {
	if d.Err != nil {
		return d.Err
	}
	var all []string
	for id, ok := range d.Active {
		if ok {
			all = append(all, id)
		}
	}
	sortStrings(all)
	for i := 0; i < len(all); i += 1000 {
		end := min(i+1000, len(all))
		if err := fn(all[i:end]); err != nil {
			return err
		}
	}
	return nil
}
