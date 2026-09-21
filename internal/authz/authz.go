// Package authz decides access to channels and templates with Zanzibar-style
// relation tuples: grants of owner/editor/viewer/sharer on a resource to a
// user, a role or the whole tenant, optionally expiring. The `use` action
// (sending) belongs to owner, editor and sharer. Tenant administrators (the
// built-in owner/admin roles) hold owner on everything of their tenant.
package authz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/go-freya/freya/services/auth/pkg/authclient"
	"github.com/go-freya/freya/services/notification/internal/audit"
	"github.com/go-freya/freya/services/notification/internal/store"
)

// Relations (data-model.md) and actions.
const (
	Owner  = "owner"
	Editor = "editor"
	Viewer = "viewer"
	Sharer = "sharer"

	Read   = "read"
	Write  = "write"
	Delete = "delete"
	Share  = "share"
	Use    = "use"
)

// Resource and subject types.
const (
	Channel  = "channel"
	Template = "template"

	SubjectUser   = "user"
	SubjectRole   = "role"
	SubjectTenant = "tenant"
)

// AdminRoles hold owner implicitly (FR-014).
var AdminRoles = []string{"owner", "admin"}

// Errors.
var (
	ErrForbidden = errors.New("authz: forbidden")
	ErrNotFound  = errors.New("authz: resource not found")
	ErrInput     = errors.New("authz: invalid input")
	// ErrAboveGranter refuses a grant above the granter's own relation.
	ErrAboveGranter = fmt.Errorf("%w: relation_above_granter", ErrForbidden)
)

// Store is the persistence authz reads and writes (repo.Store satisfies it).
type Store interface {
	GetChannel(ctx context.Context, tenantID, id string) (store.Channel, error)
	GetTemplate(ctx context.Context, tenantID, id string) (store.Template, error)
	UpsertGrant(ctx context.Context, g store.Grant) (store.Grant, error)
	GetGrant(ctx context.Context, tenantID, id string) (store.Grant, error)
	DeleteGrant(ctx context.Context, tenantID, id string) error
	GrantsOnResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]store.Grant, error)
	GrantsForSubjects(ctx context.Context, tenantID, userID string, roles []string, now time.Time) ([]store.Grant, error)
	DeleteGrantsOfResource(ctx context.Context, tenantID, resourceType, resourceID string) error
	DeleteGrantsOfSubject(ctx context.Context, tenantID, resourceType, resourceID, subjectType, subjectID string) error
}

// Subjects is the caller as seen by the grant tables. Service callers act
// for a tenant with no user id and no roles: only tenant-wide grants apply.
type Subjects struct {
	TenantID string
	UserID   string
	Roles    []string
	Service  string // SPIFFE id of a service caller ("" for people)
}

// SubjectsOf derives the subjects from a verified platform identity (the
// roles are effective: direct and through groups, feature 004).
func SubjectsOf(id authclient.Identity) Subjects {
	return Subjects{TenantID: id.TenantID, UserID: id.UserID, Roles: append([]string(nil), id.Roles...)}
}

// ServiceSubjects is a module acting for a tenant.
func ServiceSubjects(tenantID, spiffeID string) Subjects {
	return Subjects{TenantID: tenantID, Service: spiffeID}
}

// ActorKind and ActorID for audit events.
func (s Subjects) ActorKind() string {
	if s.Service != "" {
		return "service"
	}
	return "user"
}

// ActorID is the user id or the service id.
func (s Subjects) ActorID() string {
	if s.Service != "" {
		return s.Service
	}
	return s.UserID
}

// IsAdmin reports whether the subjects hold a tenant-administrator role.
func (s Subjects) IsAdmin() bool {
	for _, r := range s.Roles {
		for _, a := range AdminRoles {
			if r == a {
				return true
			}
		}
	}
	return false
}

// Permissions are the derived booleans of a relation set.
type Permissions struct {
	Read   bool `json:"read"`
	Write  bool `json:"write"`
	Delete bool `json:"delete"`
	Share  bool `json:"share"`
	Use    bool `json:"use"`
}

// Has reports one permission.
func (p Permissions) Has(perm string) bool {
	switch perm {
	case Read:
		return p.Read
	case Write:
		return p.Write
	case Delete:
		return p.Delete
	case Share:
		return p.Share
	case Use:
		return p.Use
	}
	return false
}

// Of returns the permissions a relation carries.
func Of(relation string) Permissions {
	switch relation {
	case Owner:
		return Permissions{Read: true, Write: true, Delete: true, Share: true, Use: true}
	case Editor:
		return Permissions{Read: true, Write: true, Use: true}
	case Viewer:
		return Permissions{Read: true}
	case Sharer:
		return Permissions{Read: true, Share: true, Use: true}
	}
	return Permissions{}
}

// rank orders relations for "strongest" reporting and granter bounds.
func rank(relation string) int {
	switch relation {
	case Owner:
		return 4
	case Editor:
		return 3
	case Sharer:
		return 2
	case Viewer:
		return 1
	}
	return 0
}

// ValidRelation / ValidPermission / ValidResourceType / ValidSubjectType.
func ValidRelation(r string) bool { return rank(r) > 0 }
func ValidPermission(p string) bool {
	return p == Read || p == Write || p == Delete || p == Share || p == Use
}
func ValidResourceType(t string) bool { return t == Channel || t == Template }
func ValidSubjectType(t string) bool {
	return t == SubjectUser || t == SubjectRole || t == SubjectTenant
}

// Source explains where a permission comes from.
type Source struct {
	GrantID      string     `json:"grant_id"`
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	SubjectType  string     `json:"subject_type"`
	SubjectID    string     `json:"subject_id,omitempty"`
	Relation     string     `json:"relation"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Inherited    bool       `json:"inherited"` // true for the implicit administrator owner
}

// Decision is the outcome of a check.
type Decision struct {
	Allowed     bool        `json:"allowed"`
	Relation    string      `json:"relation,omitempty"` // strongest relation held
	Permissions Permissions `json:"permissions"`
	Sources     []Source    `json:"sources,omitempty"`
}

// Authz evaluates and manages grants.
type Authz struct {
	st    Store
	audit *audit.Writer
	now   func() time.Time
}

// New wires the evaluator.
func New(st Store, aw *audit.Writer) *Authz {
	return &Authz{st: st, audit: aw, now: time.Now}
}

// SetClock injects the clock (tests).
func (a *Authz) SetClock(now func() time.Time) { a.now = now }

// locate verifies the (already validated) resource type/id exists in the
// caller's tenant; anything outside is not found.
func (a *Authz) locate(ctx context.Context, tenantID, resourceType, resourceID string) error {
	var err error
	if resourceType == Channel {
		_, err = a.st.GetChannel(ctx, tenantID, resourceID)
	} else {
		_, err = a.st.GetTemplate(ctx, tenantID, resourceID)
	}
	return notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// matches reports whether a grant applies to the subjects at now.
func matches(g store.Grant, s Subjects, now time.Time) bool {
	if g.ExpiresAt != nil && !g.ExpiresAt.After(now) {
		return false
	}
	switch g.SubjectType {
	case SubjectUser:
		return s.UserID != "" && g.SubjectID == s.UserID
	case SubjectRole:
		for _, r := range s.Roles {
			if r == g.SubjectID {
				return true
			}
		}
		return false
	}
	return g.SubjectType == SubjectTenant
}

// evaluate computes the decision for subjects on a located resource.
func (a *Authz) evaluate(ctx context.Context, s Subjects, resourceType, resourceID string) (Decision, error) {
	grants, err := a.st.GrantsOnResource(ctx, s.TenantID, resourceType, resourceID)
	if err != nil {
		return Decision{}, err
	}
	return a.decide(s, resourceType, resourceID, grants), nil
}

// decide computes the decision from the grants on the resource.
func (a *Authz) decide(s Subjects, resourceType, resourceID string, grants []store.Grant) Decision {
	d := Decision{}
	if s.IsAdmin() {
		d.Relation = Owner
		d.Permissions = Of(Owner)
		d.Sources = append(d.Sources, Source{ResourceType: resourceType, ResourceID: resourceID, SubjectType: SubjectRole, SubjectID: "admin", Relation: Owner, Inherited: true})
	}
	now := a.now()
	for _, g := range grants {
		if !matches(g, s, now) {
			continue
		}
		d.Permissions = union(d.Permissions, Of(g.Relation))
		if rank(g.Relation) > rank(d.Relation) {
			d.Relation = g.Relation
		}
		d.Sources = append(d.Sources, Source{GrantID: g.ID, ResourceType: g.ResourceType, ResourceID: g.ResourceID, SubjectType: g.SubjectType, SubjectID: g.SubjectID,
			Relation: g.Relation, ExpiresAt: g.ExpiresAt})
	}
	sort.SliceStable(d.Sources, func(i, j int) bool { return rank(d.Sources[i].Relation) > rank(d.Sources[j].Relation) })
	return d
}

func union(a, b Permissions) Permissions {
	return Permissions{Read: a.Read || b.Read, Write: a.Write || b.Write, Delete: a.Delete || b.Delete, Share: a.Share || b.Share, Use: a.Use || b.Use}
}

// Check answers whether the subjects hold permission on the resource. A
// resource outside the tenant is ErrNotFound; a refusal is audited.
func (a *Authz) Check(ctx context.Context, s Subjects, resourceType, resourceID, permission string) (Decision, error) {
	if !ValidPermission(permission) || !ValidResourceType(resourceType) {
		return Decision{}, ErrInput
	}
	if err := a.locate(ctx, s.TenantID, resourceType, resourceID); err != nil {
		return Decision{}, err
	}
	d, err := a.evaluate(ctx, s, resourceType, resourceID)
	if err != nil {
		return Decision{}, err
	}
	d.Allowed = d.Permissions.Has(permission)
	if !d.Allowed {
		a.emit(audit.Event{Type: audit.AccessRefused, TenantID: s.TenantID, ActorKind: s.ActorKind(), ActorID: s.ActorID(), SubjectKind: resourceType, SubjectID: resourceID,
			Outcome: "refused", Reason: "no_" + permission, Details: map[string]any{"permission": permission}})
	}
	return d, nil
}

// Require is Check that returns ErrForbidden when not allowed.
func (a *Authz) Require(ctx context.Context, s Subjects, resourceType, resourceID, permission string) (Decision, error) {
	d, err := a.Check(ctx, s, resourceType, resourceID, permission)
	if err != nil {
		return d, err
	}
	if !d.Allowed {
		return d, ErrForbidden
	}
	return d, nil
}

// PermissionsOn returns the permissions the subjects hold on an existing
// resource without auditing (listing decoration).
func (a *Authz) PermissionsOn(ctx context.Context, s Subjects, resourceType, resourceID string) (Permissions, error) {
	d, err := a.evaluate(ctx, s, resourceType, resourceID)
	return d.Permissions, err
}

// ReadableIDs returns the ids of resourceType the subjects may read, or
// all=true for administrators (listing filter).
func (a *Authz) ReadableIDs(ctx context.Context, s Subjects, resourceType string) (ids map[string]bool, all bool, err error) {
	if s.IsAdmin() {
		return nil, true, nil
	}
	grants, err := a.st.GrantsForSubjects(ctx, s.TenantID, s.UserID, s.Roles, a.now())
	if err != nil {
		return nil, false, err
	}
	ids = map[string]bool{}
	for _, g := range grants {
		if g.ResourceType == resourceType && Of(g.Relation).Read {
			ids[g.ResourceID] = true
		}
	}
	return ids, false, nil
}

// GrantOwner records the creator-owner grant of a new resource.
func (a *Authz) GrantOwner(ctx context.Context, tenantID, resourceType, resourceID, userID string) error {
	if userID == "" {
		return nil // service creators: administrators hold owner implicitly
	}
	by := userID
	_, err := a.st.UpsertGrant(ctx, store.Grant{ID: store.NewID(), TenantID: tenantID, ResourceType: resourceType, ResourceID: resourceID,
		SubjectType: SubjectUser, SubjectID: userID, Relation: Owner, GrantedBy: &by})
	return err
}

// SetTenantUse materialises (on) or removes (off) the tenant-wide sharer
// grant a default channel carries (research R7): every member and every
// module may send through it.
func (a *Authz) SetTenantUse(ctx context.Context, tenantID, channelID string, on bool) error {
	if !on {
		return a.st.DeleteGrantsOfSubject(ctx, tenantID, Channel, channelID, SubjectTenant, "")
	}
	_, err := a.st.UpsertGrant(ctx, store.Grant{ID: store.NewID(), TenantID: tenantID, ResourceType: Channel, ResourceID: channelID, SubjectType: SubjectTenant, Relation: Sharer})
	return err
}

// DropResource removes every grant of a deleted resource.
func (a *Authz) DropResource(ctx context.Context, tenantID, resourceType, resourceID string) error {
	return a.st.DeleteGrantsOfResource(ctx, tenantID, resourceType, resourceID)
}

func (a *Authz) emit(e audit.Event) {
	if a.audit != nil {
		_ = a.audit.Emit(e)
	}
}
