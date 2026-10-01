// Server paging and sorting of every notification table (go-tangra
// specs/032-server-side-tables): each table asks for one page with the
// server's sort fields, shows the total, keeps page / size / sort in the URL,
// adopts the server-clamped page and returns to page 1 on a filter change.
// Option lists (selects) load one large page instead of the table's store.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import type { Router } from 'vue-router'
import { plugins } from './helpers'
import Channels from '@/views/channels/index.vue'
import Templates from '@/views/templates/index.vue'
import Messages from '@/views/messages/index.vue'
import Categories from '@/views/categories/index.vue'
import Log from '@/views/log/index.vue'
import Permissions from '@/views/permissions/index.vue'
import { pagedList } from '@/stores/paged'

const BASE = '/api/notification/v1/'
const calls: string[] = []
const params = (url: string) => new URL(url, 'https://x').searchParams

/** A server of `total` rows per list that echoes the request and clamps the page. */
function server(total = 60) {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    calls.push(url)
    const q = params(url)
    let body: unknown = { items: [] }
    if (url.endsWith('/stats')) body = { channels: 1, templates: 1, notifications: {}, messages: {}, open_streams: 0, operations_24h: 0 }
    if (q.has('page')) {
      const size = Number(q.get('page_size'))
      const page = Math.min(Number(q.get('page')), Math.max(1, Math.ceil(total / size)))
      const row = { id: 'r' + page, name: 'Row ' + page, title: 'Msg ' + page, type: 'email', status: 'sent', settings: {}, recipients: { all: true }, created_at: '2026-01-01T00:00:00Z', ts: '2026-01-01T00:00:00Z', event_type: 'channel_created', actor_kind: 'user', outcome: 'ok', details: {} }
      body = { items: [row], total, page, page_size: size, sort: q.get('sort'), order: q.get('order') }
    }
    return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
}
const lists = (path: string) => calls.filter((u) => u.startsWith(BASE + path + '?'))
const last = (path: string) => params(lists(path).at(-1)!)
const header = (w: ReturnType<typeof mount>, label: string) => w.findAll('th button').find((b) => b.text().startsWith(label))
function mountView(view: unknown, rules?: Array<{ action: string; subject: string }>) {
  const p = plugins(rules)
  const w = mount(view as never, { global: { plugins: p }, attachTo: document.body })
  return { w, router: p[0] as Router }
}

beforeEach(() => {
  setActivePinia(createPinia())
  calls.length = 0
  document.cookie = '__Host-csrf=tok; Secure; Path=/'
  ;(globalThis as unknown as { __vw: number }).__vw = 1280
  vi.stubGlobal('EventSource', class { addEventListener() {} close() {} })
})

describe('simple tables', () => {
  const cases = [
    { name: 'channels', view: Channels, path: 'channels', first: 'page=1&page_size=25&sort=name&order=asc', sorts: { Type: ['type', 'asc'], Created: ['created_at', 'desc'] }, unsortable: ['Templates', 'Status'] },
    { name: 'templates', view: Templates, path: 'templates', first: 'page=1&page_size=25&sort=name&order=asc', sorts: { Channel: ['channel', 'asc'], Updated: ['updated_at', 'desc'] }, unsortable: ['Subject'] },
    { name: 'messages', view: Messages, path: 'messages', first: 'page=1&page_size=25&sort=created_at&order=desc', sorts: { Title: ['subject', 'asc'], Status: ['status', 'asc'] }, unsortable: ['Recipients', 'Read'] },
    { name: 'categories', view: Categories, path: 'categories', first: 'page=1&page_size=25&sort=sort_order&order=asc', sorts: { Name: ['name', 'asc'] }, unsortable: ['Description', 'Messages'] },
    { name: 'log', view: Log, path: 'notifications', first: 'page=1&page_size=25&sort=created_at&order=desc', sorts: { Status: ['status', 'asc'], Channel: ['channel', 'asc'] }, unsortable: ['Recipient', 'Subject'] },
  ] as const
  for (const c of cases) {
    it(`${c.name}: first page, total, header sort on the server, pager`, async () => {
      server(60)
      const { w } = mountView(c.view)
      await flushPromises()
      expect(lists(c.path)[0]).toBe(BASE + c.path + '?' + c.first)
      expect(w.text()).toContain('Showing 1–25 of 60')
      const sortable = w.findAll('th button').map((b) => b.text())
      for (const label of c.unsortable) expect(sortable.some((t) => t.startsWith(label)), label).toBe(false)
      for (const [label, [sort, order]] of Object.entries(c.sorts)) {
        await header(w, label)!.trigger('click')
        await flushPromises()
        expect([last(c.path).get('sort'), last(c.path).get('order'), last(c.path).get('page')]).toEqual([sort, order, '1'])
      }
      await w.find('[aria-label="Page 2"]').trigger('click')
      await flushPromises()
      expect(last(c.path).get('page')).toBe('2')
      expect(last(c.path).has('cursor') || last(c.path).has('limit')).toBe(false)
      w.unmount()
    })
  }
})

describe('state and filters', () => {
  it('page, size and sort live in the URL; the server-clamped page is adopted; unknown sorts fall back', async () => {
    server(31)
    const p = plugins()
    const router = p[0] as Router
    await router.push('/notification/channels?channels.page=9&channels.size=10&channels.sort=created_at&channels.order=desc')
    await router.isReady()
    const w = mount(Channels as never, { global: { plugins: p } })
    await flushPromises()
    const first = params(lists('channels')[0]!)
    expect([first.get('page'), first.get('page_size'), first.get('sort'), first.get('order')]).toEqual(['9', '10', 'created_at', 'desc'])
    expect(router.currentRoute.value.query['channels.page']).toBe('4') // server clamped 9 → 4
    w.unmount()
    calls.length = 0
    await router.push('/notification/channels?channels.sort=settings')
    const w2 = mount(Channels as never, { global: { plugins: p } })
    await flushPromises()
    const q2 = params(lists('channels')[0]!)
    expect([q2.get('page'), q2.get('sort'), q2.get('order')]).toEqual(['1', 'name', 'asc'])
    w2.unmount()
  })

  it('a log filter returns to page 1 and keeps the sort', async () => {
    server(60)
    const { w } = mountView(Log)
    await flushPromises()
    await header(w, 'Status')!.trigger('click')
    await flushPromises()
    await w.find('[aria-label="Page 2"]').trigger('click')
    await flushPromises()
    expect(last('notifications').get('page')).toBe('2')
    const input = w.find<HTMLInputElement>('[data-test="log-recipient"] input, input[data-test="log-recipient"]')
    await input.setValue('ana')
    await input.trigger('keyup', { key: 'Enter' })
    await flushPromises()
    expect([last('notifications').get('recipient'), last('notifications').get('page'), last('notifications').get('sort')]).toEqual(['ana', '1', 'status'])
    w.unmount()
  })

  it('the audit tab pages on its own key, newest first, 50 per page', async () => {
    server(120)
    const { w } = mountView(Log)
    await flushPromises()
    expect(lists('audit')[0]).toBe(BASE + 'audit?page=1&page_size=50&sort=ts&order=desc')
    w.unmount()
  })

  it('selects load channels and categories by their order up to the largest page', async () => {
    server(3)
    const t = mountView(Templates)
    await flushPromises()
    const ch = lists('channels').map(params)
    expect(ch.some((q) => q.get('page_size') === '200' && q.get('sort') === 'name')).toBe(true)
    t.w.unmount()
    const m = mountView(Messages)
    await flushPromises()
    const cats = lists('categories').map(params)
    expect(cats.some((q) => q.get('page_size') === '200' && q.get('sort') === 'sort_order')).toBe(true)
    m.w.unmount()
  })

  it('permissions: channels and templates page independently', async () => {
    server(60)
    const { w, router } = mountView(Permissions)
    await flushPromises()
    expect(lists('channels')[0]).toBe(BASE + 'channels?page=1&page_size=25&sort=name&order=asc')
    expect(lists('templates')[0]).toBe(BASE + 'templates?page=1&page_size=25&sort=name&order=asc')
    const pagers = w.findAll('[aria-label="Page 2"]')
    await pagers[1]!.trigger('click')
    await flushPromises()
    expect(last('templates').get('page')).toBe('2')
    expect(last('channels').get('page')).toBe('1')
    expect(router.currentRoute.value.query['perm-templates.page']).toBe('2')
    w.unmount()
  })
})

describe('pagedList', () => {
  it('drops a superseded response and keeps the newest page', async () => {
    let resolveFirst: (r: Response) => void = () => {}
    const page = (n: number) => new Response(JSON.stringify({ items: [{ id: 'p' + n }], total: 2, page: n, page_size: 1, sort: 'name', order: 'asc' }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    vi.stubGlobal('fetch', vi.fn().mockImplementationOnce(() => new Promise<Response>((r) => (resolveFirst = r))).mockImplementationOnce(async () => page(2)))
    const l = pagedList<{ id: string }>('channels', { page: 1, page_size: 1, sort: 'name', order: 'asc' })
    const first = l.list({}, { page: 1, page_size: 1, sort: 'name', order: 'asc' })
    const second = await l.list({}, { page: 2, page_size: 1, sort: 'name', order: 'asc' })
    resolveFirst(page(1))
    expect(await first).toBeNull()
    expect(second?.page).toBe(2)
    expect(l.items.value[0]?.id).toBe('p2')
    expect(l.total.value).toBe(2)
  })

  it('surfaces a failed page as an error without rows', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ reason: 'validation_failed', detail: { param: 'sort' } }), { status: 422, headers: { 'Content-Type': 'application/json' } })))
    const l = pagedList('channels', { page: 1, page_size: 25, sort: 'name', order: 'asc' })
    expect(await l.list()).toBeNull()
    expect(l.error.value).not.toBe('')
    expect(l.loading.value).toBe(false)
  })
})
