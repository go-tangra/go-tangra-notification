// Go template actions inside HTML email bodies.
//
// The visual editor never interprets template syntax: every action ({{...}},
// including trim markers, pipes, strings and comments) is cut out verbatim and
// replaced by a placeholder built from Unicode private-use characters, which an
// HTML parser keeps untouched in text and attribute values alike. The action
// text itself is put back byte for byte when the body is serialized.

export interface Segment {
  kind: 'text' | 'action'
  value: string
}

export interface Lexed {
  segments: Segment[]
  /** False when an action is not terminated (the rest is returned as text). */
  complete: boolean
}

const OPEN = ''
const CLOSE = ''
/** Matches a placeholder; group 1 is the action's index in its ActionTable. */
export const PLACEHOLDER = /(\d+)/g
const RESERVED = /[]/

/** Index just past the action opening at `open` ("{{"), or -1 when it never ends. */
function actionEnd(src: string, open: number): number {
  let i = open + 2
  if (src[i] === '-' && /\s/.test(src[i + 1] ?? '')) i += 2
  if (src.startsWith('/*', i)) {
    const close = src.indexOf('*/', i + 2)
    if (close < 0) return -1
    const j = close + 2
    if (src.startsWith('}}', j)) return j + 2
    if (/^\s-\}\}/.test(src.slice(j, j + 4))) return j + 4
    return -1
  }
  let quote = ''
  for (; i < src.length; i++) {
    const c = src[i]
    if (quote) {
      if (c === '\\' && quote !== '`') i++
      else if (c === quote) quote = ''
      else if (c === '\n' && quote !== '`') return -1
      continue
    }
    if (c === '"' || c === '`' || c === "'") quote = c
    else if (c === '}' && src[i + 1] === '}') return i + 2
  }
  return -1
}

/** Splits src into literal text and Go template actions ("{{" … "}}"), following the text/template lexer's quoting and comment rules. */
export function lexActions(src: string): Lexed {
  const segments: Segment[] = []
  let from = 0
  for (;;) {
    const open = src.indexOf('{{', from)
    if (open < 0) break
    const end = actionEnd(src, open)
    if (end < 0) {
      segments.push({ kind: 'text', value: src.slice(from) })
      return { segments, complete: false }
    }
    if (open > from) segments.push({ kind: 'text', value: src.slice(from, open) })
    segments.push({ kind: 'action', value: src.slice(open, end) })
    from = end
  }
  if (from < src.length) segments.push({ kind: 'text', value: src.slice(from) })
  return { segments, complete: true }
}

/** The actions of src, in order. */
export function actionsOf(src: string): string[] {
  return lexActions(src).segments.filter((s) => s.kind === 'action').map((s) => s.value)
}

/** Interns action texts and hands out their placeholders. */
export class ActionTable {
  readonly codes: string[] = []

  placeholder(code: string): string {
    let i = this.codes.indexOf(code)
    if (i < 0) i = this.codes.push(code) - 1
    return OPEN + i + CLOSE
  }

  /** Puts every placeholder's action back. */
  restore(s: string): string {
    return s.replace(PLACEHOLDER, (m, i: string) => this.codes[Number(i)] ?? m)
  }

  /** Replaces the complete actions of s with placeholders (an unterminated tail stays text). */
  protectLenient(s: string): string {
    return lexActions(s).segments.map((seg) => (seg.kind === 'action' ? this.placeholder(seg.value) : seg.value)).join('')
  }
}

/** Replaces every action of src with its placeholder, or null when src cannot be protected (an unterminated action, or reserved characters already present). */
export function protect(src: string, table: ActionTable): string | null {
  if (RESERVED.test(src)) return null
  const { segments, complete } = lexActions(src)
  if (!complete) return null
  return segments.map((s) => (s.kind === 'action' ? table.placeholder(s.value) : s.value)).join('')
}

/** A list item holding a single paragraph is written as the bare item (email clients add paragraph margins). */
export function unwrapListParagraphs(root: ParentNode): void {
  root.querySelectorAll('li').forEach((li) => {
    const only = li.children.length === 1 && li.firstElementChild?.tagName === 'P' && [...li.childNodes].every((n) => n === li.firstElementChild || (n.nodeType === Node.TEXT_NODE && !n.textContent?.trim()))
    if (only) li.replaceChildren(...li.firstElementChild!.childNodes)
  })
}

const SAME_TAG: Record<string, string> = { b: 'strong', i: 'em', strike: 's', del: 's' }
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

function canon(n: Node): string {
  if (n.nodeType === Node.TEXT_NODE) return esc((n.textContent ?? '').replace(/\s+/g, ' '))
  if (n.nodeType !== Node.ELEMENT_NODE) return '<!' + n.nodeName + '>'
  const el = n as Element
  const tag = el.tagName.toLowerCase()
  const attrs = [...el.attributes].map((a) => a.name + '="' + esc(a.value) + '"').sort().join(' ')
  return '<' + (SAME_TAG[tag] ?? tag) + (attrs ? ' ' + attrs : '') + '>' + [...el.childNodes].map(canon).join('') + '</' + (SAME_TAG[tag] ?? tag) + '>'
}

/**
 * A structural fingerprint of an HTML fragment: element names (b = strong,
 * i = em), sorted attributes and text, with formatting whitespace ignored.
 * Two fragments with the same fingerprint render the same.
 */
export function canonicalHtml(html: string): string {
  const doc = new DOMParser().parseFromString('<!DOCTYPE html><body>' + html, 'text/html')
  unwrapListParagraphs(doc.body)
  return [...doc.head.childNodes, ...doc.body.childNodes].map(canon).join('').replace(/\s*(<[^>]*>)\s*/g, '$1').trim()
}

/** Inserts text in place of value[start, end) and returns the caret position after it. */
export function insertAt(value: string, start: number, end: number, text: string): { value: string; caret: number } {
  return { value: value.slice(0, start) + text + value.slice(end), caret: start + text.length }
}
