package renderer

import (
	"bytes"
	"fmt"
	"html/template"
	textTemplate "text/template"
)

// RenderText renders a Go text/template with the given variables map.
func RenderText(tmplStr string, variables map[string]string) (string, error) {
	tmpl, err := textTemplate.New("text").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, variables); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// RenderHTML renders a Go html/template with the given variables map.
func RenderHTML(tmplStr string, variables map[string]string) (string, error) {
	tmpl, err := template.New("html").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, variables); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
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
