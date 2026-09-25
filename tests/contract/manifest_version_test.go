package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/pkg/notificationmanifest"
)

// manifestFingerprint pins the manifest content to its version. The gateway
// keeps the first manifest registered under a version and refuses a different
// one with the same (or a lower) version as manifest_drift, so a content
// change without a version bump fails registration during a rolling update.
// When this test fails: raise notificationmanifest.Version and record the new
// fingerprint here.
var manifestFingerprint = struct{ Version, SHA256 string }{
	Version: "1.1.0",
	SHA256:  "40b25c044a2586ebc908c1e9cb4441c72d4646270740ce1257351feca035dc02",
}

func TestManifestContentMatchesVersion(t *testing.T) {
	m, err := notificationmanifest.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	version := m.Version
	m.Version = ""
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if version != manifestFingerprint.Version || got != manifestFingerprint.SHA256 {
		t.Fatalf("manifest %s has fingerprint %s; recorded %s for %s: the manifest changed, so raise notificationmanifest.Version and record the new fingerprint",
			version, got, manifestFingerprint.SHA256, manifestFingerprint.Version)
	}
}
