import { describe, expect, it } from 'vitest'
import { analyseBody, bodySchema, serializeDoc } from '@/editor/bodyEditor'
import { actionsOf } from '@/editor/goTemplate'

/** Loads a body into the editor's document model and serializes it back. */
function roundTrip(body: string): { out: string; faithful: boolean } {
  const a = analyseBody(body)
  if (!a.doc) throw new Error('not loadable: ' + a.reason)
  return { out: serializeDoc(a.doc), faithful: a.faithful }
}

const exact = [
  ['text actions, spacing kept', '<p>Hello {{.name}}, welcome to {{ .tenant }} ({{  .spaced  }}).</p>'],
  ['pipes and trim markers', '<p>Valid for {{.valid_for | upper}} and {{- .x -}} and {{.a | printf "%q"}}</p>'],
  ['if/end inside a paragraph', '<p>{{if .message}}Note: <strong>{{.message}}</strong>{{end}}</p>'],
  ['if/else with a nested action', '<p>{{if .tenant}}<strong>{{.tenant}}</strong> on {{else}}the {{end}}platform</p>'],
  ['action in an href', '<p><a href="{{.link}}">Accept the invitation</a></p>'],
  ['actions mixed with text in an href', '<p><a href="https://x.example/{{.id}}?t={{.token | urlquery}}">open</a></p>'],
  ['quotes inside an attribute action', '<p><a href="{{printf "%s/x" .base}}">x</a></p>'],
  ['action text and link together', '<p>If the link does not open, copy this address:<br>{{.link}}</p>'],
  ['markup characters inside an action', '<p>{{if eq .a "<b>&amp;"}}yes{{end}}</p>'],
  ['comments', '<p>{{/* shown to nobody */}}Hi</p>'],
  ['headings, lists and marks', '<h2>Title {{.x}}</h2><ul><li>One {{.a}}</li><li><em>Two</em> <u>three</u></li></ul><ol><li>{{.b}}</li></ol>'],
] as const

describe('WYSIWYG round-trip keeps Go template actions exactly', () => {
  it.each(exact)('%s', (_name, body) => {
    const { out, faithful } = roundTrip(body)
    expect(faithful).toBe(true)
    expect(out).toBe(body)
  })

  it('keeps the built-in invitation wording (only whitespace between blocks changes)', () => {
    const body = `<p>Hello,</p>
<p>You have been invited to {{if .tenant}}<strong>{{.tenant}}</strong> on {{end}}the platform.</p>
<p><a href="{{.link}}">Accept the invitation</a></p>
<p>If the link does not open, copy this address into your browser:<br>{{.link}}</p>
<p>The link works once and expires in {{.valid_for}}.</p>`
    const { out, faithful } = roundTrip(body)
    expect(faithful).toBe(true)
    expect(out).toBe(body.replace(/>\n</g, '><'))
    expect(actionsOf(out)).toEqual(actionsOf(body))
  })

  it('an action typed as text in the editor is stored raw, not escaped', () => {
    const schema = bodySchema()
    const doc = schema.node('doc', null, [schema.node('paragraph', null, [schema.text('A {{if lt .n 3}}few & <b>{{end}}')])])
    expect(serializeDoc(doc)).toBe('<p>A {{if lt .n 3}}few &amp; &lt;b&gt;{{end}}</p>')
  })

  it('an empty document stores an empty body', () => {
    expect(roundTrip('').out).toBe('')
  })
})

describe('content the editor cannot keep falls back to source', () => {
  it.each([
    ['conditional between blocks', '{{if .message}}<p>Message:</p>\n<blockquote>{{.message}}</blockquote>\n{{end}}<p>x</p>'],
    ['inline style (not representable)', '<div style="white-space: pre-wrap">{{.text}}</div>'],
    ['action in an unsupported attribute', '<p><img src="{{.logo}}" alt=""></p>'],
    ['bare text body', '{{.link}} {{.valid_for}}'],
    ['action inside a tag', '<p {{if .x}}class="y"{{end}}>z</p>'],
  ])('%s', (_name, body) => {
    const a = analyseBody(body)
    expect(a.faithful).toBe(false)
  })

  it('an unbalanced action is not loadable at all', () => {
    expect(analyseBody('<p>{{.link</p>')).toMatchObject({ doc: null, faithful: false, reason: 'unbalanced' })
  })
})
