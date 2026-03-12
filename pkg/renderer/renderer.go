package renderer

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"sync/atomic"
	textTemplate "text/template"
	"time"
)

const (
	// maxTemplateLen is the maximum allowed template string length (64KB).
	maxTemplateLen = 64 * 1024
	// maxOutputSize is the maximum rendered output size (1MB).
	maxOutputSize = 1 << 20
	// renderTimeout is the maximum time allowed for template execution.
	renderTimeout = 5 * time.Second
)

// errOutputTooLarge is returned when template output exceeds maxOutputSize.
var errOutputTooLarge = fmt.Errorf("template output exceeds maximum size (%d bytes)", maxOutputSize)

// limitedWriter wraps a writer and enforces a maximum byte limit.
type limitedWriter struct {
	w       io.Writer
	written int
	limit   int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.written+len(p) > lw.limit {
		return 0, errOutputTooLarge
	}
	n, err := lw.w.Write(p)
	lw.written += n
	return n, err
}

// safeTextFuncMap provides only simple, safe functions for text templates.
// This prevents SSTI by removing access to dangerous built-in functions.
// The "call" override blocks arbitrary method invocation via {{call .Method}}.
var safeTextFuncMap = textTemplate.FuncMap{
	"call": func(args ...any) (string, error) {
		return "", fmt.Errorf("call is not allowed in templates")
	},
	"print": func(args ...any) string {
		return ""
	},
	"printf": func(format string, args ...any) string {
		return ""
	},
	"println": func(args ...any) string {
		return ""
	},
}

// RenderText renders a Go text/template with the given variables map.
// Uses a restricted function map to prevent template injection attacks.
func RenderText(tmplStr string, variables map[string]string) (string, error) {
	if len(tmplStr) > maxTemplateLen {
		return "", fmt.Errorf("template too large (max %d bytes)", maxTemplateLen)
	}

	tmpl, err := textTemplate.New("text").Funcs(safeTextFuncMap).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	lw := &limitedWriter{w: &buf, limit: maxOutputSize}

	if err := executeWithTimeout(func() error {
		return tmpl.Execute(lw, variables)
	}); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// safeHTMLFuncMap provides only simple, safe functions for HTML templates.
// The "call" override blocks arbitrary method invocation via {{call .Method}}.
var safeHTMLFuncMap = template.FuncMap{
	"call": func(args ...any) (string, error) {
		return "", fmt.Errorf("call is not allowed in templates")
	},
	"print": func(args ...any) string {
		return ""
	},
	"printf": func(format string, args ...any) string {
		return ""
	},
	"println": func(args ...any) string {
		return ""
	},
}

// RenderHTML renders a Go html/template with the given variables map.
// Uses a restricted function map to prevent template injection attacks.
func RenderHTML(tmplStr string, variables map[string]string) (string, error) {
	if len(tmplStr) > maxTemplateLen {
		return "", fmt.Errorf("template too large (max %d bytes)", maxTemplateLen)
	}

	tmpl, err := template.New("html").Funcs(safeHTMLFuncMap).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	lw := &limitedWriter{w: &buf, limit: maxOutputSize}

	if err := executeWithTimeout(func() error {
		return tmpl.Execute(lw, variables)
	}); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// renderSem bounds the number of concurrent template renders to prevent
// goroutine/memory exhaustion from timed-out renders that continue executing.
var renderSem = make(chan struct{}, 50)

// orphanedGoroutines tracks goroutines that timed out and are still running.
// When this count gets too high, new renders are rejected to prevent OOM.
var orphanedGoroutines atomic.Int64

const maxOrphanedGoroutines = 100

// executeWithTimeout runs fn in a goroutine with a timeout.
// If the function completes before the deadline, its error is returned.
// If the deadline expires first, a timeout error is returned.
// Orphaned goroutines from timeouts are tracked to prevent unbounded accumulation.
func executeWithTimeout(fn func() error) error {
	if orphanedGoroutines.Load() >= maxOrphanedGoroutines {
		return fmt.Errorf("too many orphaned template render goroutines, rejecting new renders")
	}

	select {
	case renderSem <- struct{}{}:
		defer func() { <-renderSem }()
	default:
		return fmt.Errorf("too many concurrent template renders")
	}

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(renderTimeout):
		// The goroutine is still running; track it as orphaned
		orphanedGoroutines.Add(1)
		go func() {
			<-done // wait for the orphaned goroutine to finish
			orphanedGoroutines.Add(-1)
		}()
		return fmt.Errorf("template execution timed out after %s", renderTimeout)
	}
}

// ValidateTemplate parses templates without executing, to check syntax.
// Uses missingkey=zero so templates with custom variables don't fail validation.
func ValidateTemplate(subject, body string, isHTML bool) error {
	if len(subject) > maxTemplateLen {
		return fmt.Errorf("subject template too large (max %d bytes)", maxTemplateLen)
	}
	if len(body) > maxTemplateLen {
		return fmt.Errorf("body template too large (max %d bytes)", maxTemplateLen)
	}

	if _, err := textTemplate.New("subject").Funcs(safeTextFuncMap).Option("missingkey=zero").Parse(subject); err != nil {
		return fmt.Errorf("invalid subject template: %w", err)
	}

	if isHTML {
		if _, err := template.New("body").Funcs(safeHTMLFuncMap).Option("missingkey=zero").Parse(body); err != nil {
			return fmt.Errorf("invalid body template: %w", err)
		}
	} else {
		if _, err := textTemplate.New("body").Funcs(safeTextFuncMap).Option("missingkey=zero").Parse(body); err != nil {
			return fmt.Errorf("invalid body template: %w", err)
		}
	}

	return nil
}

// RenderSubjectAndBody renders subject (text/template) and body.
// For EMAIL channel type, body is rendered as html/template.
// For other channels, body is rendered as text/template.
func RenderSubjectAndBody(subject, body string, variables map[string]string, isHTML bool) (string, string, error) {
	renderedSubject, err := RenderText(subject, variables)
	if err != nil {
		return "", "", fmt.Errorf("render subject: %w", err)
	}

	var renderedBody string
	if isHTML {
		renderedBody, err = RenderHTML(body, variables)
	} else {
		renderedBody, err = RenderText(body, variables)
	}
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}

	return renderedSubject, renderedBody, nil
}
