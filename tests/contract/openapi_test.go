package contract

import (
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-notification/v4/pkg/notificationmanifest"
	"github.com/go-tangra/go-tangra/v4/freyatest/testrt"
	"github.com/go-tangra/go-tangra/v4/freyatest/testutil"
)

// TestOpenAPIDocument proves the contract parses, every operation has an id,
// responses and a declared permission (nothing is public), and the mounted
// route table equals the declared one.
func TestOpenAPIDocument(t *testing.T) {
	doc, err := httpapi.LoadDocument()
	if err != nil {
		t.Fatal(err)
	}
	perms := map[string]bool{}
	for _, p := range notificationmanifest.PermissionRefs() {
		perms[p] = true
	}
	n := 0
	for p, item := range doc.Paths.Map() {
		if !strings.HasPrefix(p, httpapi.Prefix+"/") {
			t.Errorf("%s outside the API prefix", p)
		}
		for m, op := range item.Operations() {
			n++
			if op.OperationID == "" {
				t.Errorf("%s %s: missing operationId", m, p)
			}
			if op.Responses == nil || op.Responses.Len() == 0 {
				t.Errorf("%s %s: no responses", m, p)
			}
			perm, _ := op.Extensions[httpapi.PermissionExtension].(string)
			if public, _ := op.Extensions[httpapi.PublicExtension].(bool); public {
				t.Errorf("%s %s: public routes are not allowed", m, p)
			}
			if !perms[perm] {
				t.Errorf("%s %s: permission %q not in the manifest", m, p, perm)
			}
			// Mutations are POST/PUT with a body or an explicit action path; no DELETE verbs (CSRF-safe shapes).
			if m == "DELETE" || m == "PATCH" {
				t.Errorf("%s %s: verb not allowed", m, p)
			}
		}
	}
	if n != 46 {
		t.Fatalf("operations %d", n)
	}
	s, err := httpapi.NewHandler(testrt.New(t, testutil.MustCA("example.org"), "notification"))
	if err != nil {
		t.Fatal(err)
	}
	declared := httpapi.DeclaredRoutes(doc)
	if len(declared) != len(s.Declared()) {
		t.Fatalf("declared %d mounted %d", len(declared), len(s.Declared()))
	}
	if pub := httpapi.PublicRoutes(doc); len(pub) != 0 {
		t.Fatalf("public routes: %v", pub)
	}
	// Body-heavy and slow routes carry explicit limits and timeouts (contracts).
	for p, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			switch {
			case strings.HasSuffix(p, "/backup/import"):
				if op.Extensions[httpapi.BodyLimitExtension] == nil || op.Extensions[httpapi.TimeoutExtension] == nil {
					t.Errorf("%s: limits", p)
				}
			case strings.HasSuffix(p, "/stream"):
				if v, _ := op.Extensions[httpapi.TimeoutExtension].(float64); v != 300 {
					t.Errorf("%s: timeout %v", p, op.Extensions[httpapi.TimeoutExtension])
				}
			case strings.HasSuffix(p, "/send") || strings.HasSuffix(p, "/test"):
				if op.Extensions[httpapi.TimeoutExtension] == nil {
					t.Errorf("%s: timeout", p)
				}
			}
		}
	}
}
