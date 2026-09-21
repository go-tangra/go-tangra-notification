// Package render turns templates into subjects and bodies with a fixed safe
// function set: no file, environment or network access, values are strings
// (never executed as template code), HTML bodies escape contextually, and
// every render is bounded in time and size (SR-002; research R3).
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"
	"time"
)

// Kind selects the body escaping.
type Kind string

// Kinds.
const (
	KindHTML Kind = "html" // email bodies
	KindText Kind = "text" // every other channel type
)

// Limits.
const (
	MaxSubject = 998
	MaxBody    = 256 << 10
	MaxOutput  = 1 << 20
	Deadline   = time.Second
)

// Errors.
var (
	ErrTooLarge   = errors.New("render: template too large")
	ErrTimeout    = errors.New("render: rendering exceeded the deadline")
	ErrOutputSize = errors.New("render: output exceeds 1 MiB")
	ErrVariable   = errors.New("render: variable name")
)

// SyntaxError reports a parse failure with its position.
type SyntaxError struct {
	Where    string // subject | body
	Position string // "<line>:<column>" when known
	Msg      string
}

func (e *SyntaxError) Error() string { return "render: " + e.Where + " " + e.Position + ": " + e.Msg }

// UndeclaredError names a referenced variable that is not declared.
type UndeclaredError struct {
	Where    string
	Variable string
}

func (e *UndeclaredError) Error() string {
	return "render: " + e.Where + " references undeclared variable " + e.Variable
}

// ExecError reports a runtime failure (missing variable at render time).
type ExecError struct{ Msg string }

func (e *ExecError) Error() string { return "render: " + e.Msg }

var nameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// ValidName reports whether a variable name is acceptable.
func ValidName(n string) bool { return nameRE.MatchString(n) }

// funcs is the fixed function set.
var funcs = template.FuncMap{
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"title": func(s string) string {
		words := strings.Fields(s)
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		return strings.Join(words, " ")
	},
	"trim": strings.TrimSpace,
	"default": func(def, v string) string {
		if v == "" {
			return def
		}
		return v
	},
	"date": func(layout string) string { return time.Now().UTC().Format(layout) },
	"join": func(sep string, parts ...string) string { return strings.Join(parts, sep) },
}

// Compiled is a parsed subject + body.
type Compiled struct {
	subject *template.Template
	text    *template.Template
	html    *htmltemplate.Template
	Refs    []string // referenced variable names, sorted
}

// Parse compiles a subject (text) and a body (html or text).
func Parse(subject, body string, kind Kind) (*Compiled, error) {
	if len(subject) > MaxSubject || len(body) > MaxBody {
		return nil, ErrTooLarge
	}
	c := &Compiled{}
	refs := map[string]bool{}
	st, err := template.New("subject").Funcs(funcs).Option("missingkey=error").Parse(subject)
	if err != nil {
		return nil, syntaxErr("subject", err)
	}
	c.subject = st
	collect(st.Tree, refs)
	switch kind {
	case KindHTML:
		ht, err := htmltemplate.New("body").Funcs(htmltemplate.FuncMap(funcs)).Option("missingkey=error").Parse(body)
		if err != nil {
			return nil, syntaxErr("body", err)
		}
		c.html = ht
		collect(ht.Tree, refs)
	default:
		tt, err := template.New("body").Funcs(funcs).Option("missingkey=error").Parse(body)
		if err != nil {
			return nil, syntaxErr("body", err)
		}
		c.text = tt
		collect(tt.Tree, refs)
	}
	for r := range refs {
		c.Refs = append(c.Refs, r)
	}
	sort.Strings(c.Refs)
	return c, nil
}

var posRE = regexp.MustCompile(`^template: (?:subject|body):(\d+(?::\d+)?): (.*)$`)

func syntaxErr(where string, err error) error {
	msg := err.Error()
	e := &SyntaxError{Where: where, Msg: msg}
	if m := posRE.FindStringSubmatch(msg); m != nil {
		e.Position, e.Msg = m[1], m[2]
	}
	return e
}

// collect walks the parse tree for field references (.Name).
func collect(tree *parse.Tree, refs map[string]bool) {
	if tree == nil || tree.Root == nil {
		return
	}
	var walk func(n parse.Node)
	walk = func(n parse.Node) {
		switch x := n.(type) {
		case *parse.ListNode:
			if x == nil {
				return
			}
			for _, c := range x.Nodes {
				walk(c)
			}
		case *parse.ActionNode:
			walk(x.Pipe)
		case *parse.PipeNode:
			if x == nil {
				return
			}
			for _, c := range x.Cmds {
				walk(c)
			}
		case *parse.CommandNode:
			for _, a := range x.Args {
				walk(a)
			}
		case *parse.FieldNode:
			if len(x.Ident) > 0 {
				refs[x.Ident[0]] = true
			}
		case *parse.ChainNode:
			walk(x.Node)
		case *parse.IfNode:
			walk(x.Pipe)
			walk(x.List)
			walk(x.ElseList)
		case *parse.RangeNode:
			walk(x.Pipe)
			walk(x.List)
			walk(x.ElseList)
		case *parse.WithNode:
			walk(x.Pipe)
			walk(x.List)
			walk(x.ElseList)
		case *parse.TemplateNode:
			walk(x.Pipe)
		}
	}
	walk(tree.Root)
}

// Validate checks every referenced variable is declared and every declared
// name is well formed (declared-but-unused is fine).
func (c *Compiled) Validate(declared []string) error {
	set := map[string]bool{}
	for _, d := range declared {
		if !ValidName(d) {
			return fmt.Errorf("%w %q", ErrVariable, d)
		}
		set[d] = true
	}
	for _, r := range c.Refs {
		if !set[r] {
			return &UndeclaredError{Where: "template", Variable: r}
		}
	}
	return nil
}

// limitedWriter stops after MaxOutput bytes.
type limitedWriter struct {
	buf bytes.Buffer
	n   int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.n+len(p) > MaxOutput {
		return 0, ErrOutputSize
	}
	w.n += len(p)
	return w.buf.Write(p)
}

// Render executes subject and body with the values (strings only) under the
// deadline. Missing declared variables render as empty strings; referenced
// variables absent from values fail.
func (c *Compiled) Render(ctx context.Context, values map[string]string) (subject, body string, err error) {
	data := make(map[string]string, len(values))
	for k, v := range values {
		data[k] = v
	}
	for _, r := range c.Refs {
		if _, ok := data[r]; !ok {
			return "", "", &ExecError{Msg: "missing variable " + r}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, Deadline)
	defer cancel()
	type result struct {
		subject, body string
		err           error
	}
	done := make(chan result, 1)
	go func() {
		var sw, bw limitedWriter
		if err := c.subject.Execute(&sw, data); err != nil {
			done <- result{err: execErr(err)}
			return
		}
		var err error
		if c.html != nil {
			err = c.html.Execute(&bw, data)
		} else {
			err = c.text.Execute(&bw, data)
		}
		if err != nil {
			done <- result{err: execErr(err)}
			return
		}
		done <- result{subject: strings.TrimSpace(sw.buf.String()), body: bw.buf.String()}
	}()
	select {
	case <-ctx.Done():
		return "", "", ErrTimeout
	case r := <-done:
		return r.subject, r.body, r.err
	}
}

func execErr(err error) error {
	if errors.Is(err, ErrOutputSize) {
		return ErrOutputSize
	}
	return &ExecError{Msg: strings.TrimPrefix(err.Error(), "template: ")}
}

// Preview parses, validates against the declared names and renders once.
func Preview(ctx context.Context, subject, body string, kind Kind, declared []string, values map[string]string) (string, string, error) {
	c, err := Parse(subject, body, kind)
	if err != nil {
		return "", "", err
	}
	if err := c.Validate(declared); err != nil {
		return "", "", err
	}
	return c.Render(ctx, values)
}

// Position formats a line/column pair (helper for tests and errors).
func Position(line, col int) string { return strconv.Itoa(line) + ":" + strconv.Itoa(col) }
