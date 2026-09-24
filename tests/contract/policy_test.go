package contract

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type policyRule struct {
	ID         string   `yaml:"id"`
	From       []string `yaml:"from"`
	To         []string `yaml:"to"`
	Operations []string `yaml:"operations"`
	Effect     string   `yaml:"effect"`
}

// TestPolicyShape proves the development policy admits the gateway on every
// operation and named services on the notification.v1 methods only.
func TestPolicyShape(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version string       `yaml:"version"`
		Rules   []policyRule `yaml:"rules"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version == "" || len(doc.Rules) < 2 {
		t.Fatalf("policy %+v", doc)
	}
	gateway, services := false, false
	for _, r := range doc.Rules {
		if r.Effect != "allow" {
			t.Errorf("%s: effect %q", r.ID, r.Effect)
		}
		for _, from := range r.From {
			if from == "spiffe://example.org/svc/gateway" {
				gateway = true
				continue
			}
			for _, op := range r.Operations {
				switch op {
				case "/notification.v1.Notifier/Send", "/notification.v1.Notifier/SendTest", "/notification.v1.Events/Publish", "/grpc.health.v1.Health/Check":
					services = true
				default:
					t.Errorf("%s: %s may call %s", r.ID, from, op)
				}
			}
		}
	}
	if !gateway || !services {
		t.Fatalf("gateway=%v services=%v", gateway, services)
	}
}

// TestAuthPolicyAdmitsNotification proves the auth policy admits the
// notification service on the directory RPCs it needs. The auth policy lives in
// the go-tangra-auth repository: GO_TANGRA_AUTH_DIR names a checkout, and by
// default a sibling clone next to this repository (../go-tangra-auth) is used.
// Without a checkout the check is skipped.
func TestAuthPolicyAdmitsNotification(t *testing.T) {
	dir := os.Getenv("GO_TANGRA_AUTH_DIR")
	if dir == "" {
		dir = "../../../go-tangra-auth"
	}
	raw, err := os.ReadFile(filepath.Join(dir, "deploy", "policy.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("auth checkout not found at %s (clone github.com/go-tangra/go-tangra-auth there or set GO_TANGRA_AUTH_DIR)", dir)
	}
	if err != nil {
		t.Fatal(err)
	}
	var auth struct {
		Rules []policyRule `yaml:"rules"`
	}
	if err := yaml.Unmarshal(raw, &auth); err != nil {
		t.Fatal(err)
	}
	need := map[string]bool{"/auth.v1.Profiles/Lookup": false, "/auth.v1.Profiles/ListMembers": false, "/auth.v1.Authorization/Check": false, "/auth.v1.Keys/List": false}
	for _, r := range auth.Rules {
		for _, from := range r.From {
			if from != "spiffe://example.org/svc/notification" && from != "spiffe://example.org/svc/*" {
				continue
			}
			for _, op := range r.Operations {
				if _, ok := need[op]; ok {
					need[op] = true
				}
			}
		}
	}
	for op, ok := range need {
		if !ok {
			t.Errorf("auth policy does not admit notification on %s", op)
		}
	}
}
