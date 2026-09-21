package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-freya/freya/services/auth/pkg/authclient"
	"github.com/go-freya/freya/services/notification/internal/audit"
	"github.com/go-freya/freya/services/notification/internal/memstore"
	"github.com/go-freya/freya/services/notification/internal/store"
)

const (
	tA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	tB = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c66"
	uA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c77"
	uB = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c88"
)

type fixture struct {
	ms      *memstore.Store
	az      *Authz
	aw      *audit.Writer
	ch, tpl string
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ms := memstore.New()
	aw := audit.NewWriter(ms, nil)
	t.Cleanup(aw.Close)
	az := New(ms, aw)
	f := &fixture{ms: ms, az: az, aw: aw, now: time.Unix(1_700_000_000, 0)}
	az.SetClock(func() time.Time { return f.now })
	ms.Now = func() time.Time { return f.now }
	ctx := context.Background()
	f.ch, f.tpl = store.NewID(), store.NewID()
	must(t, ms.InsertChannel(ctx, store.Channel{ID: f.ch, TenantID: tA, Name: "relay", Type: "email", SettingsSealed: []byte("x")}))
	must(t, ms.InsertTemplate(ctx, store.Template{ID: f.tpl, TenantID: tA, Name: "welcome", ChannelID: &f.ch, ChannelType: "email"}))
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) grant(t *testing.T, rtype, rid, stype, sid, rel string, exp *time.Time) store.Grant {
	t.Helper()
	g, err := f.ms.UpsertGrant(context.Background(), store.Grant{ID: store.NewID(), TenantID: tA, ResourceType: rtype, ResourceID: rid, SubjectType: stype, SubjectID: sid, Relation: rel, ExpiresAt: exp})
	must(t, err)
	return g
}

func user(id string, roles ...string) Subjects {
	return Subjects{TenantID: tA, UserID: id, Roles: roles}
}

func TestRelationsAndSubjects(t *testing.T) {
	for rel, want := range map[string]Permissions{
		Owner:  {Read: true, Write: true, Delete: true, Share: true, Use: true},
		Editor: {Read: true, Write: true, Use: true},
		Viewer: {Read: true},
		Sharer: {Read: true, Share: true, Use: true},
		"x":    {},
	} {
		if Of(rel) != want {
			t.Errorf("%s: %+v", rel, Of(rel))
		}
	}
	p := Of(Owner)
	for _, perm := range []string{Read, Write, Delete, Share, Use} {
		if !p.Has(perm) {
			t.Errorf("owner lacks %s", perm)
		}
	}
	if p.Has("fly") || !ValidPermission(Use) || ValidPermission("fly") || !ValidRelation(Sharer) || ValidRelation("god") || !ValidResourceType(Channel) || ValidResourceType("folder") || !ValidSubjectType(SubjectTenant) || ValidSubjectType("group") {
		t.Fatal("validators")
	}
	s := SubjectsOf(authclient.Identity{TenantID: tA, UserID: uA, Roles: []string{"member", "admin"}})
	if s.TenantID != tA || s.UserID != uA || len(s.Roles) != 2 || !s.IsAdmin() || s.ActorKind() != "user" || s.ActorID() != uA {
		t.Fatalf("subjects %+v", s)
	}
	svc := ServiceSubjects(tA, "spiffe://example.org/svc/warden")
	if svc.ActorKind() != "service" || svc.ActorID() != "spiffe://example.org/svc/warden" || svc.IsAdmin() {
		t.Fatalf("service subjects %+v", svc)
	}
}

func TestCheckLattice(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// No grant: everything refused and audited; listing hides it.
	d, err := f.az.Check(ctx, user(uB), Template, f.tpl, Read)
	if err != nil || d.Allowed {
		t.Fatalf("no grant: %v %+v", err, d)
	}
	f.aw.Flush()
	if n := len(f.ms.AuditEvents(tA, string(audit.AccessRefused))); n != 1 {
		t.Fatalf("refusal audited %d", n)
	}
	ids, all, err := f.az.ReadableIDs(ctx, user(uB), Template)
	if err != nil || all || len(ids) != 0 {
		t.Fatalf("readable %v %v %v", ids, all, err)
	}
	// Viewer: read only; sharer: read/share/use; editor: read/write/use; owner: all.
	f.grant(t, Template, f.tpl, SubjectUser, uB, Viewer, nil)
	d, _ = f.az.Check(ctx, user(uB), Template, f.tpl, Use)
	if d.Allowed || !d.Permissions.Read || d.Relation != Viewer {
		t.Fatalf("viewer %+v", d)
	}
	f.grant(t, Template, f.tpl, SubjectRole, "ops", Sharer, nil)
	d, _ = f.az.Check(ctx, user(uB, "ops"), Template, f.tpl, Use)
	if !d.Allowed || !d.Permissions.Share || d.Permissions.Write || d.Relation != Sharer || len(d.Sources) != 2 {
		t.Fatalf("sharer through role %+v", d)
	}
	f.grant(t, Template, f.tpl, SubjectTenant, "", Editor, nil)
	d, _ = f.az.Check(ctx, user(uB), Template, f.tpl, Write)
	if !d.Allowed || !d.Permissions.Use || d.Permissions.Delete || d.Relation != Editor {
		t.Fatalf("editor through tenant %+v", d)
	}
	ids, all, _ = f.az.ReadableIDs(ctx, user(uB), Template)
	if all || !ids[f.tpl] {
		t.Fatalf("readable after grants %v %v", ids, all)
	}
	// Administrators hold owner implicitly.
	d, _ = f.az.Check(ctx, user(uB, "admin"), Channel, f.ch, Delete)
	if !d.Allowed || d.Relation != Owner || !d.Sources[0].Inherited {
		t.Fatalf("admin %+v", d)
	}
	if _, all, _ := f.az.ReadableIDs(ctx, user(uB, "owner"), Channel); !all {
		t.Fatal("admin readable all")
	}
	// Expiry: a grant in the past has no effect; at the boundary it is expired too.
	past := f.now.Add(-time.Second)
	f.grant(t, Channel, f.ch, SubjectUser, uB, Owner, &past)
	if d, _ := f.az.Check(ctx, user(uB), Channel, f.ch, Read); d.Allowed {
		t.Fatal("expired grant applied")
	}
	edge := f.now
	f.grant(t, Channel, f.ch, SubjectUser, uB, Owner, &edge)
	if d, _ := f.az.Check(ctx, user(uB), Channel, f.ch, Read); d.Allowed {
		t.Fatal("boundary grant applied")
	}
	future := f.now.Add(time.Hour)
	f.grant(t, Channel, f.ch, SubjectUser, uB, Owner, &future)
	if d, _ := f.az.Check(ctx, user(uB), Channel, f.ch, Delete); !d.Allowed {
		t.Fatal("future grant ignored")
	}
	// Service subjects see tenant grants only.
	svc := ServiceSubjects(tA, "spiffe://example.org/svc/warden")
	if d, _ := f.az.Check(ctx, svc, Template, f.tpl, Use); !d.Allowed || d.Relation != Editor {
		t.Fatalf("service tenant grant %+v", d)
	}
	if d, _ := f.az.Check(ctx, svc, Channel, f.ch, Use); d.Allowed {
		t.Fatal("service saw a user grant")
	}
	// Cross-tenant and unknown resources are not found; bad input refused.
	if _, err := f.az.Check(ctx, Subjects{TenantID: tB, UserID: uB}, Template, f.tpl, Read); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross tenant: %v", err)
	}
	if _, err := f.az.Check(ctx, user(uB), Channel, store.NewID(), Read); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := f.az.Check(ctx, user(uB), "folder", f.ch, Read); !errors.Is(err, ErrInput) {
		t.Fatalf("bad type: %v", err)
	}
	if _, err := f.az.Check(ctx, user(uB), Channel, f.ch, "fly"); !errors.Is(err, ErrInput) {
		t.Fatalf("bad perm: %v", err)
	}
	if _, err := f.az.Require(ctx, user(uA), Channel, f.ch, Read); !errors.Is(err, ErrForbidden) {
		t.Fatalf("require: %v", err)
	}
	if _, err := f.az.Require(ctx, user(uB), Channel, f.ch, Read); err != nil {
		t.Fatalf("require ok: %v", err)
	}
	if _, err := f.az.Require(ctx, user(uB), Channel, store.NewID(), Read); !errors.Is(err, ErrNotFound) {
		t.Fatalf("require missing: %v", err)
	}
	p, err := f.az.PermissionsOn(ctx, user(uB), Channel, f.ch)
	if err != nil || !p.Delete {
		t.Fatalf("permissions on %+v %v", p, err)
	}
	// Store failures propagate.
	f.ms.FailOn("GrantsOnResource", errors.New("db"))
	if _, err := f.az.Check(ctx, user(uB), Channel, f.ch, Read); err == nil {
		t.Fatal("db error swallowed")
	}
	if _, err := f.az.PermissionsOn(ctx, user(uB), Channel, f.ch); err == nil {
		t.Fatal("db error swallowed (permissions)")
	}
	f.ms.FailOn("GrantsOnResource", nil)
	f.ms.FailOn("GrantsForSubjects", errors.New("db"))
	if _, _, err := f.az.ReadableIDs(ctx, user(uB), Channel); err == nil {
		t.Fatal("db error swallowed (readable)")
	}
	f.ms.FailOn("GrantsForSubjects", nil)
	f.ms.FailOn("GetChannel", errors.New("db"))
	if _, err := f.az.Check(ctx, user(uB), Channel, f.ch, Read); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("locate db error: %v", err)
	}
	f.ms.FailOn("GetChannel", nil)
}

func TestOwnerTenantUseAndDrop(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.az.GrantOwner(ctx, tA, Template, f.tpl, uA))
	must(t, f.az.GrantOwner(ctx, tA, Template, f.tpl, "")) // service creator: no grant
	if d, _ := f.az.Check(ctx, user(uA), Template, f.tpl, Delete); !d.Allowed {
		t.Fatal("creator not owner")
	}
	// Default channel: tenant-wide sharer grant on and off.
	must(t, f.az.SetTenantUse(ctx, tA, f.ch, true))
	if d, _ := f.az.Check(ctx, user(uB), Channel, f.ch, Use); !d.Allowed || d.Permissions.Write {
		t.Fatalf("tenant use %+v", d)
	}
	must(t, f.az.SetTenantUse(ctx, tA, f.ch, false))
	if d, _ := f.az.Check(ctx, user(uB), Channel, f.ch, Use); d.Allowed {
		t.Fatal("tenant use not removed")
	}
	f.grant(t, Template, f.tpl, SubjectUser, uB, Viewer, nil)
	must(t, f.az.DropResource(ctx, tA, Template, f.tpl))
	if len(f.ms.Grants) != 0 {
		t.Fatalf("grants left %d", len(f.ms.Grants))
	}
	f.ms.FailOn("UpsertGrant", errors.New("db"))
	if err := f.az.GrantOwner(ctx, tA, Template, f.tpl, uA); err == nil {
		t.Fatal("db error swallowed")
	}
	if err := f.az.SetTenantUse(ctx, tA, f.ch, true); err == nil {
		t.Fatal("db error swallowed (tenant use)")
	}
	f.ms.FailOn("UpsertGrant", nil)
}

func TestGrantRevokeListEffective(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.az.GrantOwner(ctx, tA, Template, f.tpl, uA))
	owner := user(uA)
	// Owner grants sharer to a role and viewer to a user with expiry.
	exp := f.now.Add(time.Hour)
	g1, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectRole, SubjectID: "ops", Relation: Sharer})
	must(t, err)
	g2, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer, ExpiresAt: &exp})
	must(t, err)
	if g1.Relation != Sharer || g2.ExpiresAt == nil || g2.GrantedBy != uA {
		t.Fatalf("views %+v %+v", g1, g2)
	}
	// A repeated grant replaces (same id count).
	if _, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: uB, Relation: Editor}); err != nil {
		t.Fatal(err)
	}
	list, err := f.az.ListGrants(ctx, owner, Template, f.tpl)
	if err != nil || len(list) != 3 {
		t.Fatalf("list %d %v", len(list), err)
	}
	// Sharer (through role) may grant viewer but not editor/owner; viewer may not grant at all.
	sharer := user(store.NewID(), "ops")
	if _, err := f.az.Grant(ctx, sharer, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: store.NewID(), Relation: Viewer}); err != nil {
		t.Fatalf("sharer grants viewer: %v", err)
	}
	if _, err := f.az.Grant(ctx, sharer, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: store.NewID(), Relation: Owner}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("sharer grants owner: %v", err)
	}
	f.aw.Flush()
	found := false
	for _, e := range f.ms.AuditEvents(tA, string(audit.AccessRefused)) {
		if e.Reason == "relation_above_granter" {
			found = true
		}
	}
	if !found {
		t.Fatal("escalation not audited")
	}
	// Viewer (uB now editor: write+use, no share) cannot grant.
	if _, err := f.az.Grant(ctx, user(uB), GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: store.NewID(), Relation: Viewer}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor grants: %v", err)
	}
	// Bad input.
	for _, in := range []GrantInput{
		{ResourceType: "folder", ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer},
		{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectTenant, SubjectID: "x", Relation: Viewer},
		{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: "", Relation: Viewer},
		{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: uB, Relation: "god"},
		{ResourceType: Template, ResourceID: "", SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer},
	} {
		if _, err := f.az.Grant(ctx, owner, in); !errors.Is(err, ErrInput) {
			t.Errorf("%+v: %v", in, err)
		}
	}
	past := f.now.Add(-time.Minute)
	if _, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer, ExpiresAt: &past}); !errors.Is(err, ErrInput) {
		t.Fatalf("past expiry: %v", err)
	}
	if _, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: store.NewID(), SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing resource: %v", err)
	}
	// Effective for self and for another subject (needs share).
	d, err := f.az.Effective(ctx, user(uB), Template, f.tpl)
	if err != nil || d.Relation != Editor || !d.Allowed || len(d.Sources) != 1 {
		t.Fatalf("effective %+v %v", d, err)
	}
	if d, err := f.az.Effective(ctx, user(store.NewID()), Template, f.tpl); err != nil || d.Allowed || d.Sources == nil {
		t.Fatalf("effective none %+v %v", d, err)
	}
	d, err = f.az.EffectiveFor(ctx, owner, Template, f.tpl, SubjectRole, "ops")
	if err != nil || d.Relation != Sharer {
		t.Fatalf("effective for role %+v %v", d, err)
	}
	if d, err := f.az.EffectiveFor(ctx, owner, Template, f.tpl, SubjectUser, store.NewID()); err != nil || d.Allowed || d.Sources == nil {
		t.Fatalf("effective for unknown user %+v %v", d, err)
	}
	if _, err := f.az.EffectiveFor(ctx, user(uB), Template, f.tpl, SubjectRole, "ops"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("effective for without share: %v", err)
	}
	if _, err := f.az.EffectiveFor(ctx, owner, Template, f.tpl, SubjectTenant, ""); !errors.Is(err, ErrInput) {
		t.Fatalf("effective for tenant: %v", err)
	}
	if _, err := f.az.EffectiveFor(ctx, owner, Template, store.NewID(), SubjectUser, uB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("effective for missing: %v", err)
	}
	if _, err := f.az.Effective(ctx, owner, "folder", f.tpl); !errors.Is(err, ErrInput) {
		t.Fatal("effective bad type")
	}
	if _, err := f.az.Effective(ctx, owner, Template, store.NewID()); !errors.Is(err, ErrNotFound) {
		t.Fatal("effective missing")
	}
	// Expiry flag in listings once the clock passes.
	f.now = f.now.Add(2 * time.Hour)
	list, _ = f.az.ListGrants(ctx, owner, Template, f.tpl)
	expired := 0
	for _, g := range list {
		if g.Expired {
			expired++
		}
	}
	if expired != 0 { // uB's grant was replaced without expiry
		t.Fatalf("expired %d", expired)
	}
	soon := f.now.Add(time.Minute)
	must(t, f.az.SetTenantUse(ctx, tA, f.ch, false))
	if _, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: store.NewID(), Relation: Viewer, ExpiresAt: &soon}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(2 * time.Minute)
	list, _ = f.az.ListGrants(ctx, owner, Template, f.tpl)
	expired = 0
	for _, g := range list {
		if g.Expired {
			expired++
		}
	}
	if expired != 1 {
		t.Fatalf("expired after clock %d", expired)
	}
	// ListGrants needs read; viewer of nothing is refused; bad type / missing.
	if _, err := f.az.ListGrants(ctx, user(store.NewID()), Template, f.tpl); !errors.Is(err, ErrForbidden) {
		t.Fatalf("list without read: %v", err)
	}
	if _, err := f.az.ListGrants(ctx, owner, "folder", f.tpl); !errors.Is(err, ErrInput) {
		t.Fatal("list bad type")
	}
	if _, err := f.az.ListGrants(ctx, owner, Template, store.NewID()); !errors.Is(err, ErrNotFound) {
		t.Fatal("list missing")
	}
	// Revoke: needs share; unknown id not found; effective on the next check.
	if err := f.az.Revoke(ctx, user(uB), g1.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoke without share: %v", err)
	}
	if err := f.az.Revoke(ctx, owner, store.NewID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
	must(t, f.az.Revoke(ctx, owner, g1.ID))
	if d, _ := f.az.Check(ctx, sharer, Template, f.tpl, Use); d.Allowed {
		t.Fatal("revoked grant still applies")
	}
	// Service granter: tenant grants carry no granted_by.
	must(t, f.az.SetTenantUse(ctx, tA, f.ch, true))
	svc := ServiceSubjects(tA, "spiffe://example.org/svc/warden")
	if g, err := f.az.Grant(ctx, svc, GrantInput{ResourceType: Channel, ResourceID: f.ch, SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer}); err != nil || g.GrantedBy != "" {
		t.Fatalf("service grant %+v %v", g, err)
	}
	// Store failures.
	f.ms.FailOn("UpsertGrant", errors.New("db"))
	if _, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer}); err == nil {
		t.Fatal("upsert error swallowed")
	}
	f.ms.FailOn("UpsertGrant", nil)
	f.ms.FailOn("DeleteGrant", errors.New("db"))
	if err := f.az.Revoke(ctx, owner, g2.ID); err == nil {
		t.Fatal("delete error swallowed")
	}
	f.ms.FailOn("DeleteGrant", nil)
	f.ms.FailOn("GrantsOnResource", errors.New("db"))
	if _, err := f.az.ListGrants(ctx, owner, Template, f.tpl); err == nil {
		t.Fatal("list db error swallowed")
	}
	if _, err := f.az.Effective(ctx, owner, Template, f.tpl); err == nil {
		t.Fatal("effective db error swallowed")
	}
	if err := f.az.Revoke(ctx, owner, g2.ID); err == nil {
		t.Fatal("revoke check db error swallowed")
	}
	if _, err := f.az.EffectiveFor(ctx, owner, Template, f.tpl, SubjectUser, uB); err == nil {
		t.Fatal("effective-for db error swallowed")
	}
	f.ms.FailOn("GrantsOnResource", nil)
	// Grant with a Check error (db) propagates.
	f.ms.FailOn("GetTemplate", errors.New("db"))
	if _, err := f.az.Grant(ctx, owner, GrantInput{ResourceType: Template, ResourceID: f.tpl, SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer}); err == nil {
		t.Fatal("grant locate error swallowed")
	}
	f.ms.FailOn("GetTemplate", nil)
	// Listing with the read from the tenant grant evaluates for a service too.
	if list, err := f.az.ListGrants(ctx, svc, Channel, f.ch); err != nil || len(list) == 0 {
		t.Fatalf("service list %v %v", list, err)
	}
	_ = f.now
}

func TestEffectiveForAfterGrantCheckErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Revoke with a grant whose resource evaluation fails on the GrantsOnResource read.
	must(t, f.az.GrantOwner(ctx, tA, Channel, f.ch, uA))
	g, err := f.az.Grant(ctx, user(uA), GrantInput{ResourceType: Channel, ResourceID: f.ch, SubjectType: SubjectUser, SubjectID: uB, Relation: Viewer})
	must(t, err)
	f.ms.FailOn("GetGrant", errors.New("db"))
	if err := f.az.Revoke(ctx, user(uA), g.ID); err == nil {
		t.Fatal("get grant error swallowed")
	}
	f.ms.FailOn("GetGrant", nil)
	// Evaluate error inside ListGrants after the read check passes.
	f.ms.FailOn("GetChannel", errors.New("db"))
	if _, err := f.az.ListGrants(ctx, user(uA), Channel, f.ch); err == nil {
		t.Fatal("locate error swallowed")
	}
	if _, err := f.az.Effective(ctx, user(uA), Channel, f.ch); err == nil {
		t.Fatal("locate error swallowed (effective)")
	}
	f.ms.FailOn("GetChannel", nil)
}

func TestNotFoundHelper(t *testing.T) {
	if !errors.Is(notFound(store.ErrNotFound), ErrNotFound) || notFound(nil) != nil {
		t.Fatal("notFound mapping")
	}
	other := errors.New("x")
	if !errors.Is(notFound(other), other) {
		t.Fatal("notFound passthrough")
	}
}
