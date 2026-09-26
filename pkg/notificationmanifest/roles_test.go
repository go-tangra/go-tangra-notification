package notificationmanifest_test

import (
	"slices"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/pkg/notificationmanifest"
)

// TestRoles checks the module role set (feature 019, research D9): slugs,
// display names and permissions, all of them the module's own; no role
// carries events:publish (a module capability, user decision).
func TestRoles(t *testing.T) {
	all := notificationmanifest.PermissionRefs()
	admin := slices.DeleteFunc(slices.Clone(all), func(p string) bool { return p == "events:publish" })
	want := map[string]struct {
		name  string
		perms []string
	}{
		"administrator": {"Notifications administrator", admin},
		"sender":        {"Notifications sender", []string{"channels:read", "templates:read", "notifications:send", "notifications:read", "messages:read", "messages:manage", "inbox:read"}},
		"viewer":        {"Notifications viewer", []string{"channels:read", "templates:read", "notifications:read", "messages:read", "inbox:read"}},
	}
	own := map[string]bool{}
	for _, p := range all {
		own[p] = true
	}
	if !own["events:publish"] || len(admin) != len(all)-1 {
		t.Fatalf("events:publish must be a declared permission: %v", all)
	}
	if len(notificationmanifest.Roles) != len(want) {
		t.Fatalf("%d roles, want %d", len(notificationmanifest.Roles), len(want))
	}
	for _, r := range notificationmanifest.Roles {
		w, ok := want[r.Slug]
		if !ok {
			t.Fatalf("unexpected role %q", r.Slug)
		}
		if r.DisplayName != w.name || r.Description == "" {
			t.Errorf("%s: name %q, description %q", r.Slug, r.DisplayName, r.Description)
		}
		if !slices.Equal(r.Permissions, w.perms) {
			t.Errorf("%s: %v, want %v", r.Slug, r.Permissions, w.perms)
		}
		for _, p := range r.Permissions {
			if !own[p] {
				t.Errorf("%s names %q, not a notification permission", r.Slug, p)
			}
			if p == "events:publish" {
				t.Errorf("%s carries events:publish", r.Slug)
			}
		}
	}
}

// TestRegistration checks the registration sent to auth: module identity,
// every permission, the role set and the built-in grants; auth's rules hold.
func TestRegistration(t *testing.T) {
	reg := notificationmanifest.Registration()
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	if reg.Module != "notification" || reg.DisplayName != "Notifications" || len(reg.Permissions) != len(notificationmanifest.Permissions) || len(reg.Roles) != 3 {
		t.Fatalf("%+v", reg)
	}
	for i, p := range notificationmanifest.Permissions {
		if got := reg.Permissions[i]; got.Resource != p.Resource || got.Action != p.Action || got.Description != p.Description {
			t.Errorf("permission %d: %+v", i, got)
		}
	}
	for _, slug := range []string{"owner", "admin", "member", "auditor", "operator"} {
		if !slices.Equal(reg.BuiltinGrants[slug], notificationmanifest.Grants[slug]) {
			t.Errorf("grant %s: %v", slug, reg.BuiltinGrants[slug])
		}
	}
}
