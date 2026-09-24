package fuzz

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-notification/v4/internal/transfer"
)

// FuzzBackup: the backup parser never panics, enforces size and depth
// bounds and only yields documents the schema accepts.
func FuzzBackup(f *testing.F) {
	f.Add(`{"version":1,"exported_at":"2024-01-01T00:00:00Z","tenant":"t","channels":[],"templates":[],"categories":[]}`)
	f.Add(`{"version":1,"channels":[{"name":"a","type":"email","settings":{"host":"h","port":25,"from":"a@b.c"}}]}`)
	f.Add(`[[[[[[[[[[]]]]]]]]]]`)
	f.Add(`{"version":"1"}`)
	f.Add(``)
	f.Add(strings.Repeat("{", 100))
	f.Fuzz(func(t *testing.T, raw string) {
		doc, err := transfer.DecodeBounded([]byte(raw))
		if err != nil {
			if len(raw) > transfer.MaxBytes && !errors.Is(err, transfer.ErrTooLarge) {
				t.Fatalf("oversize not reported as such: %v", err)
			}
			return
		}
		if doc.Version != 1 {
			t.Fatalf("version %d accepted", doc.Version)
		}
		if len(doc.Channels)+len(doc.Templates)+len(doc.Categories) > transfer.MaxItems {
			t.Fatal("item bound")
		}
		for _, c := range doc.Channels {
			if c.Name == "" || c.Type == "" {
				t.Fatalf("channel without name/type accepted: %+v", c)
			}
		}
		// Round trip stays valid.
		again, _ := json.Marshal(doc)
		if _, err := transfer.DecodeBounded(again); err != nil {
			t.Fatalf("round trip: %v", err)
		}
	})
}
