package contract

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/go-tangra/go-tangra-notification/v4/pkg/notificationmanifest"
	"github.com/go-tangra/go-tangra-portal/sdk/v4/api/schema"
)

// TestManifestMatchesContract builds the manifest from the OpenAPI document
// and checks it against contracts/manifest.md and the gateway schema.
func TestManifestMatchesContract(t *testing.T) {
	m, err := notificationmanifest.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.Module != "notification" || m.Version != "1.1.0" || len(m.Prefixes) != 1 || len(m.Permissions) != 13 || len(m.Abilities) != 9 || len(m.Nav) != 7 || len(m.Methods) != 0 || len(m.Exposes) != 3 {
		t.Fatalf("%+v", m)
	}
	byKey := map[string]int{}
	for i, r := range m.Routes {
		byKey[r.Method+" "+r.Path] = i
	}
	check := func(key string, perm string, public bool, body uint64, timeout time.Duration) {
		t.Helper()
		i, ok := byKey[key]
		if !ok {
			t.Fatalf("%s missing", key)
		}
		r := m.Routes[i]
		if r.Permission != perm || r.Public != public || r.MaxBodyBytes != body || r.Timeout != timeout || r.ClientAddress {
			t.Errorf("%s: %+v", key, r)
		}
	}
	check("POST /api/notification/v1/backup/import", "backup:manage", false, 16777216, 120*time.Second)
	check("POST /api/notification/v1/backup/export", "backup:manage", false, 0, 120*time.Second)
	check("GET /api/notification/v1/stream", "inbox:read", false, 0, 300*time.Second)
	check("POST /api/notification/v1/notifications/send", "notifications:send", false, 0, 60*time.Second)
	check("POST /api/notification/v1/channels/{id}/test", "channels:manage", false, 0, 60*time.Second)
	check("POST /api/notification/v1/messages/{id}/send", "messages:manage", false, 0, 120*time.Second)
	check("GET /api/notification/v1/channels/{id}", "channels:read", false, 0, 0)
	check("POST /api/notification/v1/channels/{id}/remove", "channels:manage", false, 0, 0)
	check("POST /api/notification/v1/grants", "permissions:manage", false, 0, 0)
	check("GET /api/notification/v1/inbox", "inbox:read", false, 0, 0)
	// The module owns no public routes: the federated remote is reached through
	// the gateway's per-module relay (/m/notification/…), not an owned /ui prefix.
	for _, r := range m.Routes {
		if r.Public {
			t.Errorf("public route %s", r.Path)
		}
	}
	// Every ability and nav entry requires a declared permission; grants only name declared ones.
	perms := map[string]bool{}
	for _, p := range notificationmanifest.PermissionRefs() {
		perms[p] = true
	}
	for _, a := range m.Abilities {
		if !perms[a.Requires] {
			t.Errorf("ability requires %q", a.Requires)
		}
	}
	for _, n := range m.Nav {
		if !perms[n.Requires] {
			t.Errorf("nav requires %q", n.Requires)
		}
	}
	for role, refs := range notificationmanifest.Grants {
		for _, ref := range refs {
			if !perms[ref] {
				t.Errorf("grant %s → %q", role, ref)
			}
		}
	}
	if len(notificationmanifest.Grants["owner"]) != 13 || len(notificationmanifest.Grants["member"]) != 6 {
		t.Fatalf("grants %v", notificationmanifest.Grants)
	}
	// The wire form satisfies the gateway's published manifest schema (incl. ./header).
	pm, err := m.Proto()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("manifest.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("manifest.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	// protojson omits empty lists and writes uint64 as strings; the gateway's
	// FromProto normalises both before validating.
	top := inst.(map[string]any)
	for _, k := range []string{"routes", "methods", "permissions", "abilities", "nav"} {
		if _, ok := top[k]; !ok {
			top[k] = []any{}
		}
	}
	for _, r := range top["routes"].([]any) {
		rm := r.(map[string]any)
		if v, ok := rm["max_body_bytes"].(string); ok {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			rm["max_body_bytes"] = json.Number(strconv.FormatInt(n, 10))
		}
	}
	if err := s.Validate(inst); err != nil {
		t.Fatalf("manifest refused by the gateway schema: %v", err)
	}
}
