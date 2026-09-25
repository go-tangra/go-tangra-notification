package notify

import (
	"regexp"
	"strings"
)

// keyRE is the system template key: "<service>.<name>" (contracts
// notification-grpc.md; the migration checks the same pattern).
var keyRE = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]{0,62}$`)

// serviceRE is a service name that can own a key namespace.
var serviceRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ValidKey reports whether key is a well-formed system template key.
func ValidKey(key string) bool { return keyRE.MatchString(key) }

// KeyService returns the namespace (owning service) of a key, "" when the
// key is malformed.
func KeyService(key string) string {
	if !ValidKey(key) {
		return ""
	}
	svc, _, _ := strings.Cut(key, ".")
	return svc
}

// ServiceFromSPIFFE extracts the service name of a verified mesh identity
// spiffe://<trust domain>/svc/<name>; anything else yields no namespace.
func ServiceFromSPIFFE(id string) (string, bool) {
	rest, ok := strings.CutPrefix(id, "spiffe://")
	if !ok {
		return "", false
	}
	td, path, ok := strings.Cut(rest, "/")
	if !ok || td == "" {
		return "", false
	}
	name, ok := strings.CutPrefix(path, "svc/")
	if !ok || !serviceRE.MatchString(name) {
		return "", false
	}
	return name, true
}
