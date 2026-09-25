package render

import (
	"context"
	"html"
	"strings"
)

// Redacted replaces secret variable values in stored renders.
const Redacted = "[redacted]"

// minScrub is the shortest secret value also scrubbed by string match
// (shorter values would garble unrelated text; their placeholders are
// redacted by the second render anyway).
const minScrub = 8

// Output is one rendered subject and body.
type Output struct {
	Subject string
	Body    string
}

// RenderRedacted renders twice (research D6): sent carries the values and
// is delivered; stored has every non-empty secret variable replaced by
// Redacted and is what the log keeps. Rendering is deterministic, so the
// two differ only where a secret was placed, whatever escaping (HTML text,
// attribute, URL) the template applied there. As a second line, long
// secret values that reach the stored copy another way (copied into a
// non-secret variable) are scrubbed by string match, raw and HTML-escaped.
func (c *Compiled) RenderRedacted(ctx context.Context, values map[string]string, secret []string) (sent, stored Output, err error) {
	if sent.Subject, sent.Body, err = c.Render(ctx, values); err != nil {
		return Output{}, Output{}, err
	}
	masked := make(map[string]string, len(values))
	for k, v := range values {
		masked[k] = v
	}
	var scrub []string
	changed := false
	for _, name := range secret {
		if v := values[name]; v != "" {
			masked[name], changed = Redacted, true
			if len(v) >= minScrub {
				scrub = append(scrub, v, html.EscapeString(v))
			}
		}
	}
	if !changed {
		return sent, sent, nil
	}
	if stored.Subject, stored.Body, err = c.Render(ctx, masked); err != nil {
		return Output{}, Output{}, err
	}
	for _, s := range scrub {
		stored.Subject = strings.ReplaceAll(stored.Subject, s, Redacted)
		stored.Body = strings.ReplaceAll(stored.Body, s, Redacted)
	}
	return sent, stored, nil
}
