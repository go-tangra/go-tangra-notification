import { describe, expect, it } from 'vitest'
import { ActionTable, actionsOf, canonicalHtml, insertAt, lexActions, protect } from '@/editor/goTemplate'

describe('Go template action lexer', () => {
  it('splits text and actions, keeping every byte of each action', () => {
    const src = 'Hi {{.name}}, {{ .tenant }} {{- .x -}} {{.valid_for | upper}}'
    const { segments, complete } = lexActions(src)
    expect(complete).toBe(true)
    expect(segments.filter((s) => s.kind === 'action').map((s) => s.value)).toEqual(['{{.name}}', '{{ .tenant }}', '{{- .x -}}', '{{.valid_for | upper}}'])
    expect(segments.map((s) => s.value).join('')).toBe(src)
  })
  it('does not end an action at braces inside strings, raw strings, chars or comments', () => {
    const src = '{{printf "}}%s" .a}}|{{printf `}}` }}|{{if eq .c \'}\'}}y{{end}}|{{/* }} note */}}|{{- /* c */ -}}'
    expect(actionsOf(src)).toEqual(['{{printf "}}%s" .a}}', '{{printf `}}` }}', "{{if eq .c '}'}}", '{{end}}', '{{/* }} note */}}', '{{- /* c */ -}}'])
  })
  it('reports an unterminated action as incomplete', () => {
    expect(lexActions('<p>{{.link</p>').complete).toBe(false)
    expect(lexActions('{{/* open').complete).toBe(false)
    expect(lexActions('{{"no end}}').complete).toBe(false)
  })
})

describe('protect / restore', () => {
  it('replaces actions with reserved placeholders and restores them exactly', () => {
    const t = new ActionTable()
    const src = '<a href="{{printf "%s/x" .base}}">{{.link}}</a>{{.link}}'
    const p = protect(src, t)!
    expect(p).not.toContain('{{')
    expect(t.codes).toEqual(['{{printf "%s/x" .base}}', '{{.link}}'])
    expect(t.restore(p)).toBe(src)
  })
  it('refuses bodies that are unbalanced or already contain the reserved characters', () => {
    expect(protect('{{.a', new ActionTable())).toBeNull()
    expect(protect('xy', new ActionTable())).toBeNull()
  })
})

describe('canonical HTML', () => {
  it('ignores formatting whitespace and attribute order, not structure', () => {
    expect(canonicalHtml('<p>\n  Hello   <b>you</b>\n</p>\n<p>x</p>')).toBe(canonicalHtml('<p>Hello <strong>you</strong></p><p>x</p>'))
    expect(canonicalHtml('<a title="t" href="h">x</a>')).toBe(canonicalHtml('<a href="h" title="t">x</a>'))
    expect(canonicalHtml('<ul><li><p>a</p></li></ul>')).toBe(canonicalHtml('<ul><li>a</li></ul>'))
    expect(canonicalHtml('<div>x</div>')).not.toBe(canonicalHtml('<p>x</p>'))
    expect(canonicalHtml('x<p>y</p>')).not.toBe(canonicalHtml('<p>x</p><p>y</p>'))
  })
})

describe('insertAt', () => {
  it('replaces the selection and places the caret after the insert', () => {
    expect(insertAt('Hello !', 6, 6, '{{.name}}')).toEqual({ value: 'Hello {{.name}}!', caret: 15 })
    expect(insertAt('abc', 1, 2, 'X')).toEqual({ value: 'aXc', caret: 2 })
  })
})
