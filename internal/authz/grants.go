package authz

import (
	"context"
	"time"

	"github.com/go-freya/freya/services/notification/internal/audit"
	"github.com/go-freya/freya/services/notification/internal/store"
)

// GrantInput is a grant request.
type GrantInput struct {
	ResourceType string
	ResourceID   string
	SubjectType  string
	SubjectID    string
	Relation     string
	ExpiresAt    *time.Time
}

// GrantView is a grant as returned to clients.
type GrantView struct {
	ID           string     `json:"id"`
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	SubjectType  string     `json:"subject_type"`
	SubjectID    string     `json:"subject_id,omitempty"`
	Relation     string     `json:"relation"`
	GrantedBy    string     `json:"granted_by,omitempty"`
	GrantedAt    time.Time  `json:"granted_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Expired      bool       `json:"expired"`
}

func (a *Authz) view(g store.Grant) GrantView {
	v := GrantView{ID: g.ID, ResourceType: g.ResourceType, ResourceID: g.ResourceID, SubjectType: g.SubjectType, SubjectID: g.SubjectID, Relation: g.Relation,
		GrantedAt: g.GrantedAt, ExpiresAt: g.ExpiresAt}
	if g.GrantedBy != nil {
		v.GrantedBy = *g.GrantedBy
	}
	if g.ExpiresAt != nil && !g.ExpiresAt.After(a.now()) {
		v.Expired = true
	}
	return v
}

// validate checks the shape of a grant request.
func (in GrantInput) validate(now time.Time) error {
	if !ValidResourceType(in.ResourceType) || !ValidSubjectType(in.SubjectType) || !ValidRelation(in.Relation) || in.ResourceID == "" {
		return ErrInput
	}
	switch in.SubjectType {
	case SubjectTenant:
		if in.SubjectID != "" {
			return ErrInput
		}
	default:
		if in.SubjectID == "" || len(in.SubjectID) > 128 {
			return ErrInput
		}
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return ErrInput
	}
	return nil
}

// Grant creates or replaces a grant. The granter needs share on the resource
// and may not hand out a relation above the strongest one they hold there.
func (a *Authz) Grant(ctx context.Context, s Subjects, in GrantInput) (GrantView, error) {
	if err := in.validate(a.now()); err != nil {
		return GrantView{}, err
	}
	d, err := a.Check(ctx, s, in.ResourceType, in.ResourceID, Share)
	if err != nil {
		return GrantView{}, err
	}
	if !d.Allowed {
		return GrantView{}, ErrForbidden
	}
	if rank(in.Relation) > rank(d.Relation) {
		a.emit(audit.Event{Type: audit.AccessRefused, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: in.ResourceType, SubjectID: in.ResourceID,
			Outcome: "refused", Reason: "relation_above_granter", Details: map[string]any{"relation": in.Relation, "held": d.Relation}})
		return GrantView{}, ErrAboveGranter
	}
	var by *string
	if s.UserID != "" {
		u := s.UserID
		by = &u
	}
	g, err := a.st.UpsertGrant(ctx, store.Grant{ID: store.NewID(), TenantID: s.TenantID, ResourceType: in.ResourceType, ResourceID: in.ResourceID,
		SubjectType: in.SubjectType, SubjectID: in.SubjectID, Relation: in.Relation, GrantedBy: by, ExpiresAt: in.ExpiresAt})
	if err != nil {
		return GrantView{}, err
	}
	a.emit(audit.Event{Type: audit.GrantCreated, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "grant", SubjectID: g.ID, Outcome: "ok",
		Details: map[string]any{"resource_type": g.ResourceType, "resource_id": g.ResourceID, "subject_type": g.SubjectType, "subject_id": g.SubjectID, "relation": g.Relation, "expires": g.ExpiresAt != nil}})
	return a.view(g), nil
}

// Revoke deletes a grant; the caller needs share on its resource.
func (a *Authz) Revoke(ctx context.Context, s Subjects, grantID string) error {
	g, err := a.st.GetGrant(ctx, s.TenantID, grantID)
	if err != nil {
		return notFound(err)
	}
	d, err := a.Check(ctx, s, g.ResourceType, g.ResourceID, Share)
	if err != nil {
		return err
	}
	if !d.Allowed {
		return ErrForbidden
	}
	if err := a.st.DeleteGrant(ctx, s.TenantID, grantID); err != nil {
		return notFound(err)
	}
	a.emit(audit.Event{Type: audit.GrantRevoked, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: "grant", SubjectID: g.ID, Outcome: "ok",
		Details: map[string]any{"resource_type": g.ResourceType, "resource_id": g.ResourceID, "subject_type": g.SubjectType, "subject_id": g.SubjectID, "relation": g.Relation}})
	return nil
}

// ListGrants returns the grants on a resource; the caller needs read.
func (a *Authz) ListGrants(ctx context.Context, s Subjects, resourceType, resourceID string) ([]GrantView, error) {
	if !ValidResourceType(resourceType) {
		return nil, ErrInput
	}
	if err := a.locate(ctx, s.TenantID, resourceType, resourceID); err != nil {
		return nil, err
	}
	grants, err := a.st.GrantsOnResource(ctx, s.TenantID, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	if d := a.decide(s, resourceType, resourceID, grants); !d.Permissions.Read {
		a.emit(audit.Event{Type: audit.AccessRefused, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: resourceType, SubjectID: resourceID, Outcome: "refused", Reason: "no_read"})
		return nil, ErrForbidden
	}
	out := make([]GrantView, 0, len(grants))
	for _, g := range grants {
		out = append(out, a.view(g))
	}
	return out, nil
}

// Effective explains the caller's own permissions on a resource with every
// contributing grant; readable by anyone (the answer is about themselves).
func (a *Authz) Effective(ctx context.Context, s Subjects, resourceType, resourceID string) (Decision, error) {
	if !ValidResourceType(resourceType) {
		return Decision{}, ErrInput
	}
	if err := a.locate(ctx, s.TenantID, resourceType, resourceID); err != nil {
		return Decision{}, err
	}
	d, err := a.evaluate(ctx, s, resourceType, resourceID)
	if err != nil {
		return Decision{}, err
	}
	d.Allowed = d.Permissions.Read
	if d.Sources == nil {
		d.Sources = []Source{}
	}
	return d, nil
}

// EffectiveFor explains another subject's permissions; the caller needs
// share on the resource. The subject is a user id (no roles known here) or a
// role slug.
func (a *Authz) EffectiveFor(ctx context.Context, s Subjects, resourceType, resourceID, subjectType, subjectID string) (Decision, error) {
	if !ValidResourceType(resourceType) || (subjectType != SubjectUser && subjectType != SubjectRole) || subjectID == "" {
		return Decision{}, ErrInput
	}
	if err := a.locate(ctx, s.TenantID, resourceType, resourceID); err != nil {
		return Decision{}, err
	}
	grants, err := a.st.GrantsOnResource(ctx, s.TenantID, resourceType, resourceID)
	if err != nil {
		return Decision{}, err
	}
	if !a.decide(s, resourceType, resourceID, grants).Permissions.Share {
		a.emit(audit.Event{Type: audit.AccessRefused, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: resourceType, SubjectID: resourceID, Outcome: "refused", Reason: "no_share"})
		return Decision{}, ErrForbidden
	}
	other := Subjects{TenantID: s.TenantID}
	if subjectType == SubjectUser {
		other.UserID = subjectID
	} else {
		other.Roles = []string{subjectID}
	}
	d := a.decide(other, resourceType, resourceID, grants)
	d.Allowed = d.Permissions.Read
	if d.Sources == nil {
		d.Sources = []Source{}
	}
	return d, nil
}
