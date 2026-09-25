// jsdom lacks a few browser APIs the kit touches on mount.
import { expect } from 'vitest'
import * as axeMatchers from 'vitest-axe/matchers'
expect.extend(axeMatchers)

class RO {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
globalThis.ResizeObserver = RO as unknown as typeof ResizeObserver
;(globalThis as unknown as { __vw: number }).__vw ??= 1280
window.matchMedia = (query: string): MediaQueryList => {
  const m = /min-width:\s*(\d+)px/.exec(query)
  const matches = m ? (globalThis as unknown as { __vw: number }).__vw >= Number(m[1]) : false
  return { matches, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false } as MediaQueryList
}
// ProseMirror (the template body editor) measures ranges when scrolling the selection into view.
const rects = (): DOMRectList => Object.assign([], { item: () => null }) as unknown as DOMRectList
const rect = (): DOMRect => ({ x: 0, y: 0, top: 0, left: 0, bottom: 0, right: 0, width: 0, height: 0, toJSON: () => ({}) }) as DOMRect
Range.prototype.getClientRects ??= rects
Range.prototype.getBoundingClientRect ??= rect
document.elementFromPoint ??= () => null
