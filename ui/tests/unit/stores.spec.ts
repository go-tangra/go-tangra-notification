import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { stubFetch } from './helpers'
import { useChannels } from '@/stores/channels'
import { useTemplates } from '@/stores/templates'
import { useLog } from '@/stores/log'
import { usePermissions } from '@/stores/permissions'
import { useCategories } from '@/stores/categories'
import { useMessages } from '@/stores/messages'
import { useInbox } from '@/stores/inbox'
import { useOps } from '@/stores/ops'
import { grantable } from '@/api/types'

const channel = { id: 'c1', name: 'relay', type: 'email' as const, settings: { host: 'h', password: '__set__' }, enabled: true, is_default: true, permissions: { delete: true } }

describe('channels store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('lists by type, never leaks credentials in the query, and moves the default on update', async () => {
    const fetch = stubFetch((url, init) => {
      if (url.includes('/channels?') && (!init || init.method === 'GET')) return { status: 200, body: { items: [channel, { ...channel, id: 'c2', name: 'second', is_default: false }], total: 2, page: 1, page_size: 25, sort: 'name', order: 'asc' } }
      if (url.endsWith('/channels/c2') && init?.method === 'PUT') return { status: 200, body: { ...channel, id: 'c2', name: 'second', is_default: true } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useChannels()
    await s.list({ type: 'email' })
    expect(s.items.length).toBe(2)
    expect(s.total).toBe(2)
    const url = String(fetch.mock.calls[0]?.[0])
    for (const p of ['type=email', 'page=1', 'page_size=25', 'sort=name', 'order=asc']) expect(url).toContain(p)
    expect(url).not.toContain('cursor')
    await s.update('c2', { name: 'second', type: 'email', settings: {}, is_default: true })
    expect(s.items.find((c) => c.id === 'c1')?.is_default).toBe(false)
    expect(s.items.find((c) => c.id === 'c2')?.is_default).toBe(true)
    for (const call of fetch.mock.calls) expect(String(call[0])).not.toContain('secret')
  })

  it('creates, removes and runs a test send', async () => {
    stubFetch((url, init) => {
      if (url.endsWith('/channels') && init?.method === 'POST') return { status: 201, body: channel }
      if (url.endsWith('/channels/c1/remove')) return { status: 204, body: null }
      if (url.endsWith('/channels/c1/test')) return { status: 200, body: { id: 'l1', status: 'sent', test: true } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useChannels()
    const created = await s.create({ name: 'relay', type: 'email', settings: {} })
    expect(created.id).toBe('c1')
    s.items = [created]
    const entry = await s.test('c1', 'ops@x.test')
    expect(entry.status).toBe('sent')
    await s.remove('c1')
    expect(s.items.length).toBe(0)
  })
})

describe('templates store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('lists with search, previews without a log entry', async () => {
    const fetch = stubFetch((url) => {
      if (url.includes('/templates?')) return { status: 200, body: { items: [{ id: 't1', name: 'welcome', channel_id: 'c1' }], total: 1, page: 1, page_size: 25, sort: 'name', order: 'asc' } }
      if (url.endsWith('/templates/preview')) return { status: 200, body: { rendered_subject: 'Hi Ana', rendered_body: '<b>&lt;Ana&gt;</b>' } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useTemplates()
    await s.list({ q: 'wel' })
    expect(String(fetch.mock.calls[0]?.[0])).toContain('q=wel')
    const p = await s.preview({ subject: 'Hi {{.Name}}', body: '<b>{{.Name}}</b>', variables: ['Name'], values: { Name: '<Ana>' } })
    expect(p.rendered_subject).toBe('Hi Ana')
    expect(p.rendered_body).toContain('&lt;Ana&gt;')
  })
  it('restores a system template to its built-in wording in place', async () => {
    const sys = { id: 's1', name: 'auth.invite', system_key: 'auth.invite', channel_id: null, subject: 'Edited', body: '{{.link}}', edited: true }
    const fetch = stubFetch((url, init) => {
      if (url.endsWith('/templates/s1/restore') && init?.method === 'POST') return { status: 200, body: { ...sys, subject: 'You are invited', edited: false } }
      if (url.includes('/templates?')) return { status: 200, body: { items: [sys] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useTemplates()
    await s.list()
    const t = await s.restore('s1')
    expect(t.edited).toBe(false)
    expect(s.items[0]?.subject).toBe('You are invited')
    expect(String(fetch.mock.calls[1]?.[0])).toContain('/templates/s1/restore')
  })
})

describe('log store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('filters and reads a single entry with the body', async () => {
    const fetch = stubFetch((url) => {
      if (url.includes('/notifications?')) return { status: 200, body: { items: [{ id: 'l1', status: 'failed', recipient: 'bo@x' }] } }
      if (url.endsWith('/notifications/l1')) return { status: 200, body: { id: 'l1', rendered_body: 'Hi Bo' } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useLog()
    await s.list({ status: 'failed', recipient: 'bo' })
    expect(String(fetch.mock.calls[0]?.[0])).toContain('status=failed')
    expect(String(fetch.mock.calls[0]?.[0])).toContain('sort=created_at&order=desc')
    const one = await s.get('l1')
    expect(one.rendered_body).toBe('Hi Bo')
  })
})

describe('permissions store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('grantable never exceeds the held relation', () => {
    expect(grantable('sharer')).toEqual(['viewer', 'sharer'])
    expect(grantable('owner')).toEqual(['viewer', 'sharer', 'editor', 'owner'])
    expect(grantable('')).toEqual([])
  })
  it('loads grants + effective, grants and revokes', async () => {
    stubFetch((url, init) => {
      if (url.includes('/grants?')) return { status: 200, body: { items: [{ id: 'g1', subject_type: 'user', subject_id: 'u1', relation: 'viewer' }] } }
      if (url.includes('/access/effective')) return { status: 200, body: { relation: 'owner', permissions: { share: true }, grants: [] } }
      if (url.endsWith('/grants') && init?.method === 'POST') return { status: 201, body: { id: 'g2', subject_type: 'user', subject_id: 'u2', relation: 'editor' } }
      if (url.endsWith('/grants/g1/revoke')) return { status: 204, body: null }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = usePermissions()
    await s.load('channel', 'c1')
    expect(s.grants.length).toBe(1)
    expect(s.effective?.relation).toBe('owner')
    await s.grant({ resource_type: 'channel', resource_id: 'c1', subject_type: 'user', subject_id: 'u2', relation: 'editor' })
    expect(s.grants.some((g) => g.id === 'g2')).toBe(true)
    await s.revoke('g1')
    expect(s.grants.some((g) => g.id === 'g1')).toBe(false)
  })
})

describe('categories store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('creates, reloads and removes', async () => {
    let created = false
    stubFetch((url, init) => {
      if (url.endsWith('/categories') && init?.method === 'POST') {
        created = true
        return { status: 201, body: { id: 'k1', name: 'Ops', sort: 1 } }
      }
      if (url.includes('/categories?') && (!init || init.method === 'GET')) return { status: 200, body: { items: created ? [{ id: 'k1', name: 'Ops', sort: 1 }] : [], total: created ? 1 : 0, page: 1, page_size: 25, sort: 'sort_order', order: 'asc' } }
      if (url.endsWith('/categories/k1/remove')) {
        created = false
        return { status: 204, body: null }
      }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useCategories()
    await s.create({ name: 'Ops', sort: 1 })
    expect(s.items.length).toBe(1)
    expect(s.total).toBe(1)
    await s.remove('k1')
    expect(s.items.length).toBe(0)
  })
})

describe('messages store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('creates a draft and sends it (SendResult, not a Message)', async () => {
    stubFetch((url, init) => {
      if (url.endsWith('/messages') && init?.method === 'POST') return { status: 201, body: { id: 'm1', title: 'Hi', status: 'draft', recipients: { all: true } } }
      if (url.endsWith('/messages/m1/send')) return { status: 200, body: { status: 'published', recipient_count: 3, dropped_recipients: [] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useMessages()
    const m = await s.create({ title: 'Hi', content: 'x', recipients: { all: true } })
    const res = await s.transition(m.id, 'send')
    expect('recipient_count' in res && res.recipient_count).toBe(3)
  })
})

describe('inbox store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('lists, marks read/unread, removes and reacts to live events', async () => {
    const entry = { id: 'e1', message: { id: 'm1', title: 'A', content: 'b' }, status: 'sent', created_at: 't' }
    stubFetch((url, init) => {
      if (url.includes('/inbox?')) return { status: 200, body: { items: [entry], unread: 1 } }
      if (url.endsWith('/inbox/unread')) return { status: 200, body: { unread: 0 } }
      if (url.endsWith('/inbox/status')) return { status: 200, body: { updated: 1, unread: String(init?.body).includes('unread') ? 2 : 0 } }
      if (url.endsWith('/inbox/remove')) return { status: 200, body: { updated: 1, unread: 0 } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useInbox()
    await s.list('all')
    expect(s.items.length).toBe(1)
    expect(s.unread).toBe(1)
    await s.setStatus(['e1'], 'unread')
    expect(s.unread).toBe(2)
    s.onLive()
    expect(s.unread).toBe(3)
    s.onRevoked('m1')
    expect(s.items.length).toBe(0)
    await s.remove(['e1'])
    expect(s.unread).toBe(0)
  })
})

describe('ops store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('loads stats and audit, imports a backup', async () => {
    stubFetch((url, init) => {
      if (url.endsWith('/stats')) return { status: 200, body: { channels: 1, templates: 2, notifications: { sent: 3 }, messages: {}, open_streams: 0, operations_24h: 5 } }
      if (url.includes('/audit')) return { status: 200, body: { items: [{ ts: 't', event_type: 'channel_created', actor_kind: 'user', outcome: 'ok', details: {} }], total: 1, page: 1, page_size: 50, sort: 'ts', order: 'desc' } }
      if (url.includes('/backup/import') && init?.method === 'POST') return { status: 200, body: { channels: { created: 1, skipped: 0, overwritten: 0, failed: 0 }, templates: { created: 0, skipped: 0, overwritten: 0, failed: 0 }, categories: { created: 0, skipped: 0, overwritten: 0, failed: 0 }, warnings: [] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const s = useOps()
    await s.loadStats()
    expect(s.stats?.operations_24h).toBe(5)
    await s.loadAudit({ event_type: 'channel_created' })
    expect(s.audit.length).toBe(1)
    expect(s.auditTotal).toBe(1)
    const file = new File([JSON.stringify({ version: 1 })], 'b.json', { type: 'application/json' })
    const rep = await s.importBackup(file, 'overwrite')
    expect(rep.channels.created).toBe(1)
  })
})
