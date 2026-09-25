package render

import (
	"context"
	"errors"
	"html"
	"net/url"
	"strings"
	"testing"
)

// hostile is a link carrying every character that escapes differently in
// HTML text, HTML attributes, URLs and JavaScript.
const hostile = `https://x.example/accept?token=NOTIF-TOKEN-a1b2c3&next=/a b"c'<d>=e`

// forms are the shapes a value can take after escaping; none may remain.
func forms(v string) []string {
	return []string{v, html.EscapeString(v), url.QueryEscape(v), "NOTIF-TOKEN-a1b2c3", strings.ToUpper(v)}
}

func leaks(t *testing.T, where, text string) {
	t.Helper()
	for _, f := range forms(hostile) {
		if strings.Contains(text, f) {
			t.Fatalf("%s leaks %q: %q", where, f, text)
		}
	}
}

// TestRenderRedacted: the delivered render carries the secret, the stored
// render carries the marker in its place in every context (text, HTML
// text, attribute, URL, subject, through a function), and non-secret
// values are identical in both.
func TestRenderRedacted(t *testing.T) {
	for _, kind := range []Kind{KindHTML, KindText} {
		c, err := Parse("Invite for {{.tenant}}: {{.link}}",
			`<p>Hi {{.tenant}}</p><a href="{{.link}}">open</a> {{.link}} <span title="{{.link}}">{{upper .link}}</span>{{if .link}} set{{end}}`, kind)
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]string{"link": hostile, "tenant": "Acme & Co"}
		sent, stored, err := c.RenderRedacted(context.Background(), values, []string{"link", "unused"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sent.Subject, hostile) || !strings.Contains(sent.Body, "NOTIF-TOKEN-a1b2c3") {
			t.Fatalf("%s: delivered render lost the link: %+v", kind, sent)
		}
		leaks(t, string(kind)+" stored subject", stored.Subject)
		leaks(t, string(kind)+" stored body", stored.Body)
		if !strings.Contains(stored.Subject, Redacted) || strings.Count(stored.Body, "redacted") < 3 || !strings.Contains(stored.Body, " set") {
			t.Fatalf("%s: stored %+v", kind, stored)
		}
		if !strings.Contains(stored.Subject, "Invite for Acme & Co") {
			t.Fatalf("%s: non-secret value changed: %q", kind, stored.Subject)
		}
		// The caller's map is not modified.
		if values["link"] != hostile {
			t.Fatal("values mutated")
		}
	}
}

func TestRenderRedactedEdges(t *testing.T) {
	ctx := context.Background()
	c, err := Parse("{{.a}}", "<p>{{.a}} {{.b}}</p>", KindHTML)
	if err != nil {
		t.Fatal(err)
	}
	// No secrets (or only empty ones): both renders are the same.
	sent, stored, err := c.RenderRedacted(ctx, map[string]string{"a": "x", "b": ""}, nil)
	if err != nil || sent != stored {
		t.Fatalf("%+v %+v %v", sent, stored, err)
	}
	sent, stored, err = c.RenderRedacted(ctx, map[string]string{"a": "x", "b": ""}, []string{"b"})
	if err != nil || sent != stored || strings.Contains(stored.Body, Redacted) {
		t.Fatalf("empty secret: %+v %+v %v", sent, stored, err)
	}
	// A secret copied into a non-secret variable is still scrubbed from the
	// stored copy (long values; raw and HTML-escaped forms).
	secret := "NOTIF-TOKEN-<copy>&x"
	sent, stored, err = c.RenderRedacted(ctx, map[string]string{"a": "see " + secret, "b": secret}, []string{"b"})
	if err != nil || !strings.Contains(sent.Body, html.EscapeString(secret)) {
		t.Fatalf("%+v %v", sent, err)
	}
	if strings.Contains(stored.Body, "NOTIF-TOKEN") || strings.Contains(stored.Subject, "NOTIF-TOKEN") {
		t.Fatalf("copied secret kept: %+v", stored)
	}
	// Short secrets are not string-scrubbed (they would garble unrelated text) but their placeholders are still redacted.
	sent, stored, err = c.RenderRedacted(ctx, map[string]string{"a": "x", "b": "x"}, []string{"b"})
	if err != nil || stored.Subject != "x" || !strings.Contains(stored.Body, Redacted) {
		t.Fatalf("short secret %+v %+v %v", sent, stored, err)
	}
	// Render failures surface from either pass.
	if _, _, err := c.RenderRedacted(ctx, map[string]string{"a": "x"}, []string{"b"}); err == nil {
		t.Fatal("missing variable rendered")
	}
	big, _ := Parse("s", "{{.a}}{{.a}}", KindText)
	if _, _, err := big.RenderRedacted(ctx, map[string]string{"a": strings.Repeat("y", MaxOutput/2+1)}, []string{"a"}); !errors.Is(err, ErrOutputSize) {
		t.Fatalf("output size: %v", err)
	}
	// The masked pass can fail on its own: a secret that fits masked output
	// differently (here: the second pass overflows only with the marker).
	edge, _ := Parse("s", "{{.a}}{{.b}}", KindText)
	vals := map[string]string{"a": strings.Repeat("y", MaxOutput-4), "b": "zzzz"}
	if _, _, err := edge.RenderRedacted(ctx, vals, []string{"b"}); !errors.Is(err, ErrOutputSize) {
		t.Fatalf("masked pass: %v", err)
	}
}
