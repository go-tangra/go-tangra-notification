package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/pkg/notificationmanifest"
)

// The release step lists the permissions from the module's own manifest.
func TestCataloguePermissions(t *testing.T) {
	var out bytes.Buffer
	if code := cataloguePermissions(&out); code != 0 {
		t.Fatal(code)
	}
	var perms []string
	if err := json.Unmarshal(out.Bytes(), &perms); err != nil {
		t.Fatal(err)
	}
	if strings.Join(perms, ",") != strings.Join(notificationmanifest.PermissionRefs(), ",") || len(perms) == 0 {
		t.Fatalf("%v", perms)
	}
}
