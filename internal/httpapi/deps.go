package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-freya/freya/services/notification/internal/authz"
	"github.com/go-freya/freya/services/notification/internal/channel"
	"github.com/go-freya/freya/services/notification/internal/notify"
	"github.com/go-freya/freya/services/notification/internal/store"
)

// Prefix of the browser API.
const Prefix = "/api/notification/v1"

// PermissionChecker answers whether a person holds an API permission
// (the auth service's Authorization/Check; a role map in tests). The gateway
// gates the route; this only widens listings for stats:read holders.
type PermissionChecker interface {
	Has(ctx context.Context, tenantID, userID, permission string) bool
}

// PermissionFunc adapts a function to PermissionChecker (tests).
type PermissionFunc func(ctx context.Context, tenantID, userID, permission string) bool

// Has implements PermissionChecker.
func (f PermissionFunc) Has(ctx context.Context, tenantID, userID, permission string) bool {
	return f(ctx, tenantID, userID, permission)
}

// NotifyDeps are the services behind the channel, template and send routes.
type NotifyDeps struct {
	Channels  *notify.Channels
	Templates *notify.Templates
	Sender    *notify.Sender
	Authz     *authz.Authz
	Perms     PermissionChecker
}

// GrantDeps are the services behind the permission routes.
type GrantDeps struct {
	Authz *authz.Authz
}

// subjects derives the authz subjects from the verified caller.
func subjects(r *http.Request) (authz.Subjects, error) {
	id, err := Caller(r)
	if err != nil {
		return authz.Subjects{}, err
	}
	return authz.SubjectsOf(id), nil
}

// hasPermission reports whether the caller holds an API permission
// (stats:read widens log and message listings; messages:manage widens the
// message listing).
func hasPermission(r *http.Request, pc PermissionChecker, perm string) bool {
	id, err := Caller(r)
	if err != nil || pc == nil {
		return false
	}
	return pc.Has(r.Context(), id.TenantID, id.UserID, perm)
}

// domainError maps service errors to refusals; validation and in-use errors
// carry their detail.
func domainError(err error) error {
	var ve *notify.ValidationError
	var iu *notify.InUseError
	switch {
	case errors.As(err, &ve):
		return &DetailError{Err: ErrValidation, Detail: ve.Detail}
	case errors.As(err, &iu):
		return &DetailError{Err: ErrConflict, Detail: map[string]any{iu.What: iu.Count}}
	case errors.Is(err, authz.ErrAboveGranter):
		return &DetailError{Err: ErrForbidden, Detail: map[string]any{"reason": "relation_above_granter"}}
	case errors.Is(err, authz.ErrForbidden):
		return ErrForbidden
	case errors.Is(err, authz.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, authz.ErrInput):
		return ErrValidation
	case errors.Is(err, notify.ErrTypeMismatch):
		return &DetailError{Err: ErrValidation, Detail: map[string]any{"field": "channel_id", "message": "channel type differs from the template"}}
	case errors.Is(err, notify.ErrChannelDisabled):
		return ErrChannelDisabled
	case errors.Is(err, channel.ErrNoProvider):
		return ErrNoProvider
	case errors.Is(err, channel.ErrRecipient):
		return &DetailError{Err: ErrValidation, Detail: map[string]any{"field": "recipient"}}
	case errors.Is(err, notify.ErrRateLimited):
		return ErrRateLimited
	case errors.Is(err, store.ErrConflict):
		return ErrConflict
	}
	return err
}

// readError is domainError for single-resource reads: a resource the
// caller may not read answers not_found so that ids never leak existence.
func readError(err error) error {
	if errors.Is(err, authz.ErrForbidden) {
		return ErrNotFound
	}
	return domainError(err)
}

// DetailError is a refusal with a detail object.
type DetailError struct {
	Err    *Error
	Detail map[string]any
}

func (e *DetailError) Error() string { return e.Err.Reason }
func (e *DetailError) Unwrap() error { return e.Err }

// fail writes a domain error (with detail when present).
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var de *DetailError
	if errors.As(err, &de) {
		WriteDetail(w, de.Err, de.Detail)
		return
	}
	Fail(w, r, s.rt.Logger(), err)
}

func limitParam(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return n
}
