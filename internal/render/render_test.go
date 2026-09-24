package render

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseValidateRender(t *testing.T) {
	c, err := Parse("Hello {{.Name | upper}}", "<p>Hi {{.Name}}, go to {{.Link}}{{if .Extra}} ({{.Extra}}){{end}}</p>", KindHTML)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Refs, ",") != "Extra,Link,Name" {
		t.Fatalf("refs %v", c.Refs)
	}
	if err := c.Validate([]string{"Name", "Link"}); err == nil {
		t.Fatal("undeclared Extra accepted")
	} else {
		var ue *UndeclaredError
		if !errors.As(err, &ue) || ue.Variable != "Extra" {
			t.Fatalf("undeclared error %v", err)
		}
	}
	if err := c.Validate([]string{"Name", "Link", "Extra", "Unused"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate([]string{"Name", "Link", "Extra", "bad name"}); !errors.Is(err, ErrVariable) {
		t.Fatalf("bad name: %v", err)
	}
	subject, body, err := c.Render(context.Background(), map[string]string{"Name": "alice", "Link": "https://x.y/?a=1&b=2", "Extra": "<b>x</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Hello ALICE" || !strings.Contains(body, "https://x.y/?a=1&amp;b=2") || !strings.Contains(body, "&lt;b&gt;x&lt;/b&gt;") {
		t.Fatalf("rendered %q %q", subject, body)
	}
	// Values are data: template syntax inside a value is never executed.
	_, body, err = c.Render(context.Background(), map[string]string{"Name": "{{.Link}}", "Link": "L", "Extra": ""})
	if err != nil || !strings.Contains(body, "{{.Link}}") {
		t.Fatalf("value executed: %q %v", body, err)
	}
	// A referenced variable missing from the values fails.
	if _, _, err := c.Render(context.Background(), map[string]string{"Name": "a"}); err == nil {
		t.Fatal("missing variable rendered")
	}
	// Text kind renders without escaping.
	tc, err := Parse("{{.A}}", "{{.A}} & {{default \"none\" .B}} {{join \", \" .A .B}} {{title .A}} {{lower .A}} {{trim .A}}", KindText)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err = tc.Render(context.Background(), map[string]string{"A": " hi there ", "B": ""})
	if err != nil || !strings.Contains(body, " hi there  & none") || !strings.Contains(body, "Hi There") {
		t.Fatalf("text %q %v", body, err)
	}
	if _, body, err = tc.Render(context.Background(), map[string]string{"A": "a", "B": "b"}); err != nil || !strings.Contains(body, "a & b") {
		t.Fatalf("text %q %v", body, err)
	}
	dc, _ := Parse("{{date \"2006\"}}", "x", KindText)
	if s, _, err := dc.Render(context.Background(), nil); err != nil || len(s) != 4 {
		t.Fatalf("date %q %v", s, err)
	}
}

func TestSyntaxAndLimits(t *testing.T) {
	_, err := Parse("ok", "line1\n{{.Name", KindHTML)
	var se *SyntaxError
	if !errors.As(err, &se) || se.Where != "body" || !strings.HasPrefix(se.Position, "2") {
		t.Fatalf("syntax %v", err)
	}
	if _, err := Parse("{{.X", "b", KindText); !errors.As(err, &se) || se.Where != "subject" {
		t.Fatalf("subject syntax %v", err)
	}
	// Only the fixed function set exists.
	for _, tpl := range []string{"{{env \"HOME\"}}", "{{exec \"ls\"}}", "{{readFile \"/etc/passwd\"}}"} {
		if _, err := Parse("s", tpl, KindText); err == nil {
			t.Errorf("%s accepted", tpl)
		}
	}
	if _, err := Parse(strings.Repeat("s", MaxSubject+1), "b", KindText); !errors.Is(err, ErrTooLarge) {
		t.Fatal("long subject accepted")
	}
	if _, err := Parse("s", strings.Repeat("b", MaxBody+1), KindText); !errors.Is(err, ErrTooLarge) {
		t.Fatal("long body accepted")
	}
	// Output bound: many references to a long value blow past 1 MiB; the writer stops.
	big, _ := Parse("s", strings.Repeat("{{.A}}", 300), KindText)
	huge := strings.Repeat("x", 4096)
	if _, _, err := big.Render(context.Background(), map[string]string{"A": huge, "B": huge}); !errors.Is(err, ErrOutputSize) {
		t.Fatalf("output bound: %v", err)
	}
	// An undefined template name fails at render time, not at parse time.
	if u, err := Parse("s", "{{template \"other\"}}", KindText); err != nil {
		t.Fatal(err)
	} else if _, _, err := u.Render(context.Background(), nil); err == nil {
		t.Fatal("undefined template rendered")
	}
	// Deadline.
	old := Deadline
	_ = old
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, _, err := big.Render(ctx, map[string]string{"A": "a", "B": "b"}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("deadline: %v", err)
	}
	// Preview: parse error, undeclared, then ok.
	if _, _, err := Preview(context.Background(), "{{", "b", KindText, nil, nil); err == nil {
		t.Fatal("preview syntax")
	}
	if _, _, err := Preview(context.Background(), "{{.X}}", "b", KindText, nil, nil); err == nil {
		t.Fatal("preview undeclared")
	}
	if s, b, err := Preview(context.Background(), "S {{.X}}", "B {{.X}}", KindText, []string{"X"}, map[string]string{"X": "1"}); err != nil || s != "S 1" || b != "B 1" {
		t.Fatalf("preview %q %q %v", s, b, err)
	}
	if Position(3, 4) != "3:4" || !ValidName("Name_1") || ValidName("1Name") || ValidName("") {
		t.Fatal("helpers")
	}
	// Branch coverage of the walker: with/else/chain/template pipe nodes.
	c, err := Parse("s", "{{with .A}}{{.}}{{else}}{{.B}}{{end}}{{if .C}}x{{else}}{{.D}}{{end}}{{(index . \"E\").F}}{{range $k, $v := .}}{{$k}}{{end}}", KindText)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"A", "B", "C", "D"} {
		found := false
		for _, r := range c.Refs {
			if r == want {
				found = true
			}
		}
		if !found {
			t.Errorf("ref %s missing in %v", want, c.Refs)
		}
	}
	// Execution errors surface as ExecError (index on a nil map key is fine; call a missing key path).
	ec, _ := Parse("s", "{{.A.B}}", KindText)
	if _, _, err := ec.Render(context.Background(), map[string]string{"A": "x"}); err == nil {
		t.Fatal("exec error swallowed")
	}
	es, _ := Parse("{{.A.B}}", "b", KindText)
	if _, _, err := es.Render(context.Background(), map[string]string{"A": "x"}); err == nil {
		t.Fatal("subject exec error swallowed")
	}
	eh, _ := Parse("s", "{{.A.B}}", KindHTML)
	if _, _, err := eh.Render(context.Background(), map[string]string{"A": "x"}); err == nil {
		t.Fatal("html exec error swallowed")
	}
	if (&ExecError{Msg: "m"}).Error() == "" || (&UndeclaredError{Where: "w", Variable: "v"}).Error() == "" || (&SyntaxError{}).Error() == "" {
		t.Fatal("error strings")
	}
	// Position-less syntax error keeps the whole message.
	if e := syntaxErr("body", errors.New("weird")); e.(*SyntaxError).Position != "" || e.(*SyntaxError).Msg != "weird" {
		t.Fatalf("syntaxErr %+v", e)
	}
	collect(nil, map[string]bool{})
}
