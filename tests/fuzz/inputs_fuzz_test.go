package fuzz

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-freya/freya/services/notification/internal/channel"
	"github.com/go-freya/freya/services/notification/internal/channel/email"
	"github.com/go-freya/freya/services/notification/internal/render"
	"github.com/go-freya/freya/services/notification/internal/stream"
)

// FuzzHeaderValue: a header value is safe iff it has no CR/LF/NUL/control
// characters, and a built message never carries an injected header.
func FuzzHeaderValue(f *testing.F) {
	for _, s := range []string{"Hello", "a\r\nBcc: x@y", "tab\there", "\x00", "Ünïcode ✓", strings.Repeat("x", 2000)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		safe := email.HeaderSafe(v)
		hasBad := false
		for _, r := range v {
			if r == '\r' || r == '\n' || r == 0 || (r < 0x20 && r != '\t') || r == 0x7f {
				hasBad = true
			}
		}
		if safe == hasBad {
			t.Fatalf("%q: safe=%v", v, safe)
		}
		if !safe {
			return
		}
		msg, err := email.Build("noreply@example.org", "to@example.org", "", v, v, "", time.Unix(0, 0))
		if err != nil {
			return
		}
		head, _, _ := bytes.Cut(msg, []byte("\r\n\r\n"))
		if bytes.Contains(head, []byte("Bcc:")) {
			t.Fatalf("injected header from %q", v)
		}
		for _, line := range bytes.Split(head, []byte("\r\n")) {
			if len(line) > 998 {
				t.Fatalf("header line over 998 bytes from %q", v)
			}
		}
	})
}

// FuzzRecipient: recipients never carry line breaks, are bounded, and email
// recipients parse as one address.
func FuzzRecipient(f *testing.F) {
	for _, s := range []string{"a@b.c", "Ana <a@b.c>", "a@b.c\r\nBcc: x@y", "", strings.Repeat("a", 513), "+15550001", "a@b.c, d@e.f"} {
		f.Add("email", s)
		f.Add("sms", s)
	}
	f.Fuzz(func(t *testing.T, typ, r string) {
		err := channel.ValidateRecipient(typ, r)
		if err == nil && (r == "" || len(r) > 512 || strings.ContainsAny(r, "\r\n\x00")) {
			t.Fatalf("%q accepted", r)
		}
		if typ == "email" && err == nil && !email.HeaderSafe(r) {
			t.Fatalf("unsafe email recipient accepted: %q", r)
		}
	})
}

// FuzzTemplate: parse and validate never panic; a template never reaches
// anything outside the fixed function set; rendering stays bounded.
func FuzzTemplate(f *testing.F) {
	for _, s := range []string{"Hello {{.Name}}", "{{.Name", "{{template \"x\"}}", "{{call .F}}", "{{range .}}{{end}}", "{{printf \"%s\" .Name}}", "{{.Name | upper}}", "{{ $x := .Name }}{{$x}}", "{{define \"a\"}}b{{end}}"} {
		f.Add(s, s, "Name")
	}
	f.Fuzz(func(t *testing.T, subject, body, declared string) {
		for _, kind := range []render.Kind{render.KindHTML, render.KindText} {
			c, err := render.Parse(subject, body, kind)
			if err != nil {
				continue
			}
			vars := strings.Fields(declared)
			if err := c.Validate(vars); err != nil {
				continue
			}
			values := map[string]string{}
			for _, v := range vars {
				values[v] = "{{.Injected}}<script>"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			s, b, err := c.Render(ctx, values)
			cancel()
			if err != nil {
				continue
			}
			if len(s)+len(b) > render.MaxOutput {
				t.Fatal("output over the limit")
			}
			if kind == render.KindHTML && strings.Contains(b, "<script>") && !strings.Contains(body, "<script>") {
				t.Fatalf("value rendered unescaped: %q", b)
			}
		}
	})
}

// FuzzVariables: template syntax inside values is data, never executed.
func FuzzVariables(f *testing.F) {
	f.Add("{{.Secret}}")
	f.Add("{{ call .X }}")
	f.Add("<b>bold</b>")
	f.Add(strings.Repeat("{{", 5000))
	f.Fuzz(func(t *testing.T, value string) {
		c, err := render.Parse("{{.V}}", "<p>{{.V}}</p>", render.KindHTML)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s, b, err := c.Render(ctx, map[string]string{"V": value})
		if err != nil {
			return
		}
		if strings.TrimSpace(s) != strings.TrimSpace(value) {
			t.Fatalf("subject altered the value: %q → %q", value, s)
		}
		if strings.Contains(value, "<") && strings.Contains(b, "<b>") {
			t.Fatalf("html not escaped: %q", b)
		}
	})
}

// FuzzSSEFrame: framing never breaks — data lines carry no bare line breaks
// and the frame ends with a blank line.
func FuzzSSEFrame(f *testing.F) {
	f.Add("1-0", "inbox", `{"a":1}`)
	f.Add("", "x", "line1\nline2")
	f.Add("9-9", "reset", "\r\n\r\n")
	f.Fuzz(func(t *testing.T, id, typ, data string) {
		frame := stream.Frame(stream.Event{ID: id, Type: typ, Data: data})
		if !strings.HasSuffix(frame, "\n\n") {
			t.Fatalf("frame not terminated: %q", frame)
		}
		body := strings.TrimSuffix(frame, "\n\n")
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "data: ") && strings.ContainsAny(line[6:], "\r\n") {
				t.Fatalf("data line carries a break: %q", line)
			}
		}
		if strings.Count(frame, "data: ") != 1 {
			t.Fatalf("data lines: %q", frame)
		}
	})
}
