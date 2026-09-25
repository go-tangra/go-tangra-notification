package fuzz

import (
	"regexp"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
)

var keyRE = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]{0,62}$`)

// FuzzSystemKey: a system template key is accepted iff it matches the
// contract pattern, and its service prefix is exactly the part before the
// dot; nothing else ever yields a namespace.
func FuzzSystemKey(f *testing.F) {
	for _, s := range []string{"auth.invite", "warden.share", "auth.", ".invite", "Auth.invite", "auth.invite.x", "auth.in-vite", "a.b", "auth.\x00", "auth." + strings.Repeat("x", 63), "auth." + strings.Repeat("x", 64), "auth2.a_b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, key string) {
		ok := notify.ValidKey(key)
		if ok != keyRE.MatchString(key) {
			t.Fatalf("%q: valid=%v", key, ok)
		}
		svc := notify.KeyService(key)
		if !ok {
			if svc != "" {
				t.Fatalf("%q: namespace %q from an invalid key", key, svc)
			}
			return
		}
		if svc == "" || !strings.HasPrefix(key, svc+".") || strings.Contains(svc, ".") {
			t.Fatalf("%q: namespace %q", key, svc)
		}
	})
}

// FuzzServiceFromSPIFFE: only spiffe://<td>/svc/<name> yields a service
// name, and the name is always a valid key namespace.
func FuzzServiceFromSPIFFE(f *testing.F) {
	for _, s := range []string{"spiffe://example.org/svc/auth", "spiffe://example.org/svc/auth/x", "spiffe://example.org/user/auth", "spiffe://example.org/svc/", "https://example.org/svc/auth", "spiffe:///svc/auth", "spiffe://example.org/svc/Auth", "spiffe://example.org/svc/warden"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, id string) {
		name, ok := notify.ServiceFromSPIFFE(id)
		if !ok {
			if name != "" {
				t.Fatalf("%q: name %q when refused", id, name)
			}
			return
		}
		if !strings.HasPrefix(id, "spiffe://") || !strings.HasSuffix(id, "/svc/"+name) || !notify.ValidKey(name+".x") {
			t.Fatalf("%q: name %q", id, name)
		}
	})
}
