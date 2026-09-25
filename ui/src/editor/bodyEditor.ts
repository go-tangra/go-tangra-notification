// The rich-text model of an email template body (TipTap / ProseMirror).
//
// Template actions in text become atomic "goAction" chips that carry the
// action's exact text; actions in attribute values (href="{{.link}}") stay in
// the attribute verbatim. Serialization turns both back into the original
// action text after the HTML is written, so the HTML serializer never escapes
// or reflows them. analyseBody reports whether the editor can represent a body
// without changing what it renders; when it cannot, the source view stays
// authoritative.
import { Node, getSchema, mergeAttributes, type Extensions } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { DOMParser as PMDOMParser, DOMSerializer, type Node as PMNode, type Schema } from '@tiptap/pm/model'
import { ActionTable, PLACEHOLDER, canonicalHtml, protect, unwrapListParagraphs } from './goTemplate'

/** An inline, atomic Go template action ({{.link}}, {{if .x}}, {{end}} …). */
export const GoAction = Node.create({
  name: 'goAction',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  addAttributes() {
    return {
      code: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-code') ?? '',
        renderHTML: (a) => ({ 'data-code': a.code as string }),
      },
    }
  },
  parseHTML() {
    return [{ tag: 'span[data-go-action]', priority: 100 }]
  },
  renderHTML({ node, HTMLAttributes }) {
    return ['span', mergeAttributes(HTMLAttributes, { 'data-go-action': '', class: 'tpl-action', contenteditable: 'false' }), String(node.attrs.code)]
  },
  renderText({ node }) {
    return String(node.attrs.code)
  },
})

export function bodyExtensions(): Extensions {
  return [
    StarterKit.configure({
      heading: { levels: [1, 2, 3] },
      // No node the operator did not write: a trailing paragraph would change the body.
      trailingNode: false,
      link: { openOnClick: false, autolink: false, HTMLAttributes: { target: null, rel: null, class: null } },
    }),
    GoAction,
  ]
}

let schema: Schema | undefined
export function bodySchema(): Schema {
  return (schema ??= getSchema(bodyExtensions()))
}

export type Analysis = { doc: PMNode; faithful: boolean } | { doc: null; faithful: false; reason: 'unbalanced' | 'reserved' }

function walkText(root: globalThis.Node, fn: (t: Text) => void): void {
  const it = root.ownerDocument!.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  const all: Text[] = []
  while (it.nextNode()) all.push(it.currentNode as Text)
  all.forEach(fn)
}

function eachAttr(root: Element, fn: (el: Element, name: string, value: string) => void): void {
  root.querySelectorAll('*').forEach((el) => [...el.attributes].forEach((a) => fn(el, a.name, a.value)))
}

/** Parses a body into the editor's document and checks that serializing it gives back the same rendered HTML and the same actions. */
export function analyseBody(body: string, s: Schema = bodySchema()): Analysis {
  const table = new ActionTable()
  const src = protect(body, table)
  if (src === null) return { doc: null, faithful: false, reason: /[]/.test(body) ? 'reserved' : 'unbalanced' }
  const dom = new DOMParser().parseFromString('<!DOCTYPE html><body>' + src, 'text/html').body
  walkText(dom, (t) => {
    const parts = t.data.split(PLACEHOLDER)
    if (parts.length === 1) return
    // split() with a capture group alternates text, action index, text …
    t.replaceWith(
      ...parts.map((p, i) => {
        if (i % 2 === 0) return p
        const chip = dom.ownerDocument.createElement('span')
        chip.setAttribute('data-go-action', '')
        chip.setAttribute('data-code', table.codes[Number(p)] ?? '')
        return chip
      }),
    )
  })
  eachAttr(dom, (el, name, value) => {
    if (value.includes('')) el.setAttribute(name, table.restore(value))
  })
  const doc = PMDOMParser.fromSchema(s).parse(dom)
  return { doc, faithful: canonicalHtml(src) === canonicalHtml(serializeProtected(doc, table)) }
}

function isEmpty(doc: PMNode): boolean {
  const first = doc.firstChild
  return doc.childCount === 1 && !!first && first.type.name === 'paragraph' && first.childCount === 0
}

/** Serializes doc with every action (chips, attribute values and actions typed as text) replaced by its placeholder in table. */
function serializeProtected(doc: PMNode, table: ActionTable): string {
  if (isEmpty(doc)) return ''
  const base = DOMSerializer.fromSchema(doc.type.schema)
  // The serializer only accepts elements: a chip is written as a marker element
  // holding its placeholder, then unwrapped.
  const ser = new DOMSerializer({ ...base.nodes, goAction: (n) => ['tpl-action', table.placeholder(String(n.attrs.code))] }, base.marks)
  const wrap = document.createElement('div')
  wrap.appendChild(ser.serializeFragment(doc.content, { document }))
  wrap.querySelectorAll('tpl-action').forEach((m) => m.replaceWith(m.textContent ?? ''))
  wrap.normalize()
  unwrapListParagraphs(wrap)
  walkText(wrap, (t) => {
    if (t.data.includes('{{')) t.data = table.protectLenient(t.data)
  })
  eachAttr(wrap, (el, name, value) => {
    if (value.includes('{{')) el.setAttribute(name, table.protectLenient(value))
  })
  return wrap.innerHTML
}

/** The stored body for doc: HTML with every Go template action written exactly as entered. */
export function serializeDoc(doc: PMNode): string {
  const table = new ActionTable()
  return table.restore(serializeProtected(doc, table))
}
