import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useConfirm } from '@go-tangra/ui'
import { perms, plugins, stubFetch } from './helpers'
import Channels from '@/views/channels/index.vue'
import Templates from '@/views/templates/index.vue'
// Loaded ahead so the view's lazy body editor resolves within flushPromises.
import '@/components/TemplateBodyEditor.vue'
import Messages from '@/views/messages/index.vue'
import Inbox from '@/views/inbox/index.vue'
import Permissions from '@/views/permissions/index.vue'
import Log from '@/views/log/index.vue'
import HeaderBell from '@/components/HeaderBell.vue'
import { channelSchema, templateSchema, messageSchema, categorySchema, backupImportSchema } from '@/schemas'

class FakeES { static made = 0; onopen = null; onerror = null; constructor() { FakeES.made++ } addEventListener() {} close() {} }
const mountView = (c: unknown, rules?: Array<{ action: string; subject: string }>) => mount(c as never, { global: { plugins: plugins(rules) }, attachTo: document.body })
const drawer = () => document.body.querySelector('aside[role=dialog]')!
const set = (root: ParentNode, sel: string, v: string) => { const el = root.querySelector<HTMLInputElement>(sel)!; el.value = v; el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input')) }

describe('notification schemas', () => {
  it('channel: email needs host/port/from, others need account; template variables are identifiers; message recipients; category sort; backup file', () => {
    expect(channelSchema.safeParse({ name: 'relay', type: 'email', port: 587 }).success).toBe(false)
    expect(channelSchema.safeParse({ name: 'relay', type: 'email', host: 'h', port: 587, from: 'a@b.co' }).success).toBe(true)
    expect(channelSchema.safeParse({ name: 'relay', type: 'email', host: 'h', port: 70000, from: 'a@b.co' }).success).toBe(false)
    expect(channelSchema.safeParse({ name: 'sms', type: 'sms', port: 0 }).success).toBe(false)
    expect(channelSchema.parse({ name: 'sms', type: 'sms', port: 0, account: 'acc', api_key: '__set__' }).api_key).toBe('__set__')
    expect(templateSchema.safeParse({ name: 't', channel_id: 'c', subject: 's', body: 'b', variables: ['bad name'] }).success).toBe(false)
    expect(messageSchema.safeParse({ title: 't', content: 'c', type: 'notification', users: [] }).success).toBe(false)
    expect(messageSchema.safeParse({ title: 't', content: 'c', type: 'notification', everyone: true }).success).toBe(true)
    expect(messageSchema.safeParse({ title: 't', content: 'c', type: 'notification', everyone: true, scheduled_at: '2000-01-01T00:00' }).success).toBe(false)
    expect(categorySchema.parse({ name: 'ops', sort: '' })).toEqual({ name: 'ops', sort: 0 })
    expect(backupImportSchema.safeParse({ mode: 'skip' }).success).toBe(false)
  })
})

describe('channels view', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('keeps a stored password as the marker, sends only public fields on save, and runs a test send', async () => {
    const bodies: Array<Record<string, unknown>> = []
    const channel = { id: 'c1', name: 'relay', type: 'email' as const, settings: { host: 'h', port: 587, from: 'a@b.co', password: '__set__' }, enabled: true, is_default: true, permissions: { delete: true } }
    stubFetch((url, init) => {
      if (init?.method === 'PUT') {
        bodies.push(JSON.parse(String(init.body)))
        return { status: 200, body: channel }
      }
      if (url.endsWith('/channels/c1/test')) return { status: 200, body: { id: 'l1', status: 'sent', test: true } }
      if (url.includes('/channels')) return { status: 200, body: { items: [channel] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountView(Channels)
    await flushPromises()
    await w.find('[data-test="channel-row-c1"]').trigger('click')
    await flushPromises()
    const pwd = drawer().querySelector<HTMLInputElement>('[data-test="channel-password"] input')!
    expect(pwd.value).toBe('__set__')
    expect(pwd.type).toBe('password')
    set(drawer(), '[data-test="channel-test-recipient"] input', 'ops@x.test')
    ;(drawer().querySelector('[data-test="channel-test-send"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(drawer().querySelector('[data-test="channel-test-result"]')?.textContent).toContain('sent')
    ;(drawer().querySelector('[data-test="channel-save"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(bodies[0]?.settings).toMatchObject({ host: 'h', port: 587, from: 'a@b.co', password: '__set__' })
    expect(document.body.querySelector('aside[role=dialog]')).toBeNull() // closed after save
    w.unmount()
  })
  it('blocks an empty channel client-side and shows a scrubbed server error without the credential', async () => {
    const posts: string[] = []
    stubFetch((url, init) => {
      if (init?.method === 'POST') {
        posts.push(String(init.body))
        return { status: 422, body: { reason: 'validation_failed' } }
      }
      return { status: 200, body: { items: [] } }
    })
    const w = mountView(Channels)
    await flushPromises()
    await w.find('[data-test="channel-new"]').trigger('click')
    await flushPromises()
    ;(drawer().querySelector('[data-test="channel-save"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(posts.length).toBe(0)
    expect(drawer().querySelectorAll('[role=alert]').length).toBeGreaterThan(0)
    set(drawer(), 'input[data-field=name]', 'relay')
    set(drawer(), 'input[data-field=host]', 'smtp.test')
    set(drawer(), 'input[data-field=from]', 'a@b.co')
    set(drawer(), '[data-test="channel-password"] input', 'hunter2-secret')
    ;(drawer().querySelector('[data-test="channel-save"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(posts.length).toBe(1)
    expect(posts[0]).toContain('hunter2-secret')
    expect(drawer().textContent).toContain('highlighted')
    expect(drawer().textContent).not.toContain('hunter2-secret')
    w.unmount()
  })
  it('managed channel: badge, read-only drawer without save or delete, test send still available', async () => {
    const writes: string[] = []
    const managed = { id: 'm1', name: 'Platform email', type: 'email' as const, managed: true, settings: { host: 'mx01.example.net', port: 587, tls: 'starttls', from: 'tangra@example.net', password: '__set__' }, enabled: true, is_default: true, permissions: { read: true, write: false, delete: false, use: true } }
    stubFetch((url, init) => {
      if (url.endsWith('/channels/m1/test')) return { status: 200, body: { id: 'l1', status: 'sent', test: true } }
      if (init?.method === 'PUT' || url.endsWith('/remove')) {
        writes.push(url)
        return { status: 409, body: { reason: 'managed_channel' } }
      }
      if (url.includes('/channels')) return { status: 200, body: { items: [managed] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountView(Channels)
    await flushPromises()
    expect(w.find('[data-test="channel-managed-m1"]').exists()).toBe(true)
    await w.find('[data-test="channel-row-m1"]').trigger('click')
    await flushPromises()
    expect(drawer().querySelector('[data-test="channel-managed-note"]')?.textContent).toContain('configuration')
    expect(drawer().querySelector('[data-test="channel-save"]')).toBeNull()
    expect(drawer().querySelector('[data-test="channel-delete"]')).toBeNull()
    expect(drawer().querySelector<HTMLInputElement>('input[data-field=host]')?.disabled).toBe(true)
    set(drawer(), '[data-test="channel-test-recipient"] input', 'ops@x.test')
    ;(drawer().querySelector('[data-test="channel-test-send"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(drawer().querySelector('[data-test="channel-test-result"]')?.textContent).toContain('sent')
    expect(writes).toEqual([])
    w.unmount()
  })
})

describe('templates and messages', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('template drawer previews and surfaces an undeclared-variable error', async () => {
    const channel = { id: 'c1', name: 'relay', type: 'email', settings: {}, enabled: true, is_default: true }
    stubFetch((url) => {
      if (url.endsWith('/templates/preview')) return { status: 422, body: { reason: 'validation_failed', detail: { variable: 'Nope' } } }
      if (url.includes('/channels')) return { status: 200, body: { items: [channel] } }
      if (url.includes('/templates')) return { status: 200, body: { items: [{ id: 't1', name: 'w', channel_id: 'c1', subject: '{{.Nope}}', body: 'b', variables: [] }] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountView(Templates)
    await flushPromises()
    await w.find('[data-test="template-row-t1"]').trigger('click')
    await flushPromises()
    ;(drawer().querySelector('[data-test="template-preview"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(drawer().querySelector('[data-test="template-error"]')).toBeTruthy()
    w.unmount()
  })
  it('system template: badge, required variables shown, only subject/body saved, no delete, restore built-in', async () => {
    const puts: Array<Record<string, unknown>> = []
    const posts: string[] = []
    const sys = { id: 's1', name: 'auth.invite', system_key: 'auth.invite', channel_id: null, channel_type: 'email', subject: 'Edited', body: '<p>{{.link}} {{.valid_for}}</p>', variables: ['link', 'valid_for', 'tenant'], required_variables: ['link', 'valid_for'], secret_variables: ['link'], edited: true, permissions: { read: true, write: true, delete: false } }
    stubFetch((url, init) => {
      if (init?.method === 'PUT') {
        puts.push(JSON.parse(String(init.body)))
        return { status: 200, body: sys }
      }
      if (init?.method === 'POST') {
        posts.push(url)
        return { status: 200, body: { ...sys, subject: 'You are invited', edited: false } }
      }
      if (url.includes('/channels')) return { status: 200, body: { items: [] } }
      if (url.includes('/templates')) return { status: 200, body: { items: [sys] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountView(Templates)
    await flushPromises()
    expect(w.find('[data-test="template-system-s1"]').exists()).toBe(true)
    await w.find('[data-test="template-row-s1"]').trigger('click')
    await flushPromises()
    expect(drawer().querySelector('[data-test="template-delete"]')).toBeNull()
    expect(drawer().querySelector('[data-test="template-channel"]')).toBeNull()
    expect(drawer().querySelector('[data-test="template-required"]')?.textContent).toContain('link')
    expect(drawer().querySelector('[data-test="template-secret-note"]')?.textContent).toContain('link')
    set(drawer(), '[data-test="template-subject"] input', 'Welcome to Tangra')
    ;(drawer().querySelector('[data-test="template-save"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(puts[0]).toMatchObject({ name: 'auth.invite', channel_id: null, subject: 'Welcome to Tangra', body: '<p>{{.link}} {{.valid_for}}</p>' })
    expect(puts[0]?.is_default).toBeFalsy()
    await w.find('[data-test="template-row-s1"]').trigger('click')
    await flushPromises()
    ;(drawer().querySelector('[data-test="template-restore"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(posts).toEqual([]) // asks first: the edited wording is replaced
    useConfirm().answer(true)
    await flushPromises()
    expect(posts.some((u) => u.endsWith('/templates/s1/restore'))).toBe(true)
    w.unmount()
  })
  it('system template: a save dropping a required variable shows the refusal naming it', async () => {
    const sys = { id: 's1', name: 'auth.invite', system_key: 'auth.invite', channel_id: null, channel_type: 'email', subject: 'S', body: '{{.link}} {{.valid_for}}', variables: ['link', 'valid_for'], required_variables: ['link', 'valid_for'], secret_variables: ['link'], edited: false, permissions: { read: true, write: true } }
    stubFetch((url, init) => {
      if (init?.method === 'PUT') return { status: 422, body: { reason: 'missing_required_variable', detail: { variable: 'link' } } }
      if (url.includes('/channels')) return { status: 200, body: { items: [] } }
      if (url.includes('/templates')) return { status: 200, body: { items: [sys] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountView(Templates)
    await flushPromises()
    await w.find('[data-test="template-row-s1"]').trigger('click')
    await flushPromises()
    set(drawer(), '[data-test="template-body"] textarea', 'no link {{.valid_for}}')
    ;(drawer().querySelector('[data-test="template-save"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(drawer().querySelector('[data-test="template-error"]')?.textContent).toContain('link')
    w.unmount()
  })
  it('template body: email channels get the visual editor, other channel types the plain textarea', async () => {
    const email = { id: 'c1', name: 'relay', type: 'email', settings: {}, enabled: true, is_default: true }
    const sms = { id: 'c2', name: 'texts', type: 'sms', settings: {}, enabled: true, is_default: false }
    const tpl = (id: string, channel: string) => ({ id, name: id, channel_id: channel, subject: 's', body: '<p>Hi {{.name}}</p>', variables: ['name'], permissions: perms })
    const puts: Array<Record<string, unknown>> = []
    stubFetch((url, init) => {
      if (init?.method === 'PUT') {
        puts.push(JSON.parse(String(init.body)))
        return { status: 200, body: tpl('t1', 'c1') }
      }
      if (url.includes('/channels')) return { status: 200, body: { items: [email, sms] } }
      if (url.includes('/templates')) return { status: 200, body: { items: [tpl('t1', 'c1'), tpl('t2', 'c2')] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountView(Templates)
    await flushPromises()
    await w.find('[data-test="template-row-t2"]').trigger('click')
    await flushPromises()
    expect(drawer().querySelector<HTMLTextAreaElement>('[data-test="template-body"] textarea')?.value).toBe('<p>Hi {{.name}}</p>')
    expect(drawer().querySelector('[data-test="template-body-visual"]')).toBeNull()
    expect(drawer().querySelector('[data-test="body-mode"]')).toBeNull()
    await w.find('[data-test="template-row-t1"]').trigger('click')
    await flushPromises()
    expect(drawer().querySelector('[data-test="template-body-visual"] [data-go-action]')?.textContent).toBe('{{.name}}')
    // Switching the channel to sms turns the body into the plain textarea, content unchanged.
    set(drawer(), '[data-test="template-channel"] select', 'c2')
    await flushPromises()
    expect(drawer().querySelector<HTMLTextAreaElement>('[data-test="template-body"] textarea')?.value).toBe('<p>Hi {{.name}}</p>')
    ;(drawer().querySelector('[data-test="template-save"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(puts[0]).toMatchObject({ channel_id: 'c2', body: '<p>Hi {{.name}}</p>' })
    w.unmount()
  })
  it('system template: the visual editor shows the body and restore-to-built-in replaces it', async () => {
    const sys = { id: 's1', name: 'auth.invite', system_key: 'auth.invite', channel_id: null, channel_type: 'email', subject: 'S', body: '<p>Edited <a href="{{.link}}">go</a> {{.valid_for}}</p>', variables: ['link', 'valid_for', 'tenant'], required_variables: ['link', 'valid_for'], secret_variables: ['link'], edited: true, permissions: { read: true, write: true } }
    const builtin = { ...sys, body: '<p>Built-in <a href="{{.link}}">accept</a>, valid for {{.valid_for}}</p>', edited: false }
    const puts: Array<Record<string, unknown>> = []
    stubFetch((url, init) => {
      if (init?.method === 'PUT') {
        puts.push(JSON.parse(String(init.body)))
        return { status: 200, body: builtin }
      }
      if (init?.method === 'POST' && url.endsWith('/restore')) return { status: 200, body: builtin }
      if (url.includes('/channels')) return { status: 200, body: { items: [] } }
      if (url.includes('/templates')) return { status: 200, body: { items: [sys] } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountView(Templates)
    await flushPromises()
    await w.find('[data-test="template-row-s1"]').trigger('click')
    await flushPromises()
    const pm = () => drawer().querySelector('[data-test="template-body-visual"]')!
    expect(pm().textContent).toContain('Edited')
    const menu = drawer().querySelector('[data-test="body-insert-variable"]')!
    ;(menu.querySelector('button') as HTMLButtonElement).click()
    await flushPromises()
    expect([...document.querySelectorAll('[role="menuitem"]')].map((e) => e.textContent?.trim())).toEqual(['{{.link}}', '{{.valid_for}}', '{{.tenant}}'])
    ;(document.querySelector('[role="menuitem"]') as HTMLElement).click()
    await flushPromises()
    ;(drawer().querySelector('[data-test="template-restore"]') as HTMLButtonElement).click()
    await flushPromises()
    useConfirm().answer(true)
    await flushPromises()
    expect(pm().textContent).toContain('Built-in')
    expect(pm().querySelector('a')?.getAttribute('href')).toBe('{{.link}}')
    ;(drawer().querySelector('[data-test="template-save"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(puts[0]?.body).toBe(builtin.body)
    w.unmount()
  })
  it('message drawer requires recipients unless everyone; send transitions after save', async () => {
    const calls: string[] = []
    stubFetch((url, init) => {
      if (init?.method === 'POST') {
        calls.push(url)
        return { status: 201, body: { id: 'm1', title: 't', content: 'c', type: 'notification', status: 'draft', recipients: { all: true } } }
      }
      return { status: 200, body: { items: [] } }
    })
    const w = mountView(Messages)
    await flushPromises()
    await w.find('[data-test="message-new"]').trigger('click')
    await flushPromises()
    set(drawer(), 'input[data-field=title]', 'Hello')
    set(drawer(), 'textarea[data-field=content]', 'Body')
    ;(drawer().querySelector('[data-test="message-send"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(calls.length).toBe(0)
    expect(drawer().textContent).toContain('recipient')
    const everyone = drawer().querySelector<HTMLInputElement>('input[data-field=everyone]')!
    everyone.checked = true
    everyone.dispatchEvent(new Event('change'))
    ;(drawer().querySelector('[data-test="message-send"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(calls).toEqual(['/api/notification/v1/messages', '/api/notification/v1/messages/m1/send'])
    w.unmount()
  })
})

describe('inbox, permissions, log and header', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    ;(globalThis as { EventSource?: unknown }).EventSource = FakeES as unknown as typeof EventSource
  })
  it('inbox lists entries and opens a detail dialog', async () => {
    stubFetch((url) => (url.includes('/inbox/unread') ? { status: 200, body: { unread: 1 } } : url.includes('/inbox/e1') ? { status: 200, body: { id: 'e1', status: 'read', message: { title: 'Hi', content: 'Body text', category_name: 'ops' } } } : { status: 200, body: { items: [{ id: 'e1', status: 'unread', message: { title: 'Hi', content: 'Body text', category_name: 'ops' } }], unread: 1 } }))
    const w = mountView(Inbox)
    await flushPromises()
    await w.find('[data-test="inbox-item-e1"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[data-test="inbox-body"]')?.textContent).toBe('Body text')
    w.unmount()
  })
  it('permissions: offers only relations at or below the holder; hides the form without share', async () => {
    stubFetch((url) => {
      if (url.includes('/grants?')) return { status: 200, body: { items: [{ id: 'g1', subject_type: 'user', subject_id: 'u1', relation: 'viewer' }] } }
      if (url.includes('/access/effective')) return { status: 200, body: { relation: 'sharer', permissions: { share: true }, grants: [] } }
      if (url.endsWith('/api/v1/users/lookup')) return { status: 200, body: { items: [{ id: 'u1', display_name: 'Ana' }] } }
      if (url.startsWith('/api/v1/roles')) return { status: 200, body: [] }
      if (url.includes('/channels')) return { status: 200, body: { items: [{ id: 'c1', name: 'relay', type: 'email', settings: {}, enabled: true, is_default: true }] } }
      return { status: 200, body: { items: [] } }
    })
    const w = mountView(Permissions)
    await flushPromises()
    await w.find('[data-test="perm-channel-c1"]').trigger('click')
    await flushPromises()
    expect(Array.from(drawer().querySelectorAll('#perm-level option')).map((o) => o.textContent)).toEqual(['viewer', 'sharer'])
    expect(drawer().textContent).toContain('Ana')
    w.unmount()
  })
  it('header bell shows the unread badge and opens the shared stream on mount', async () => {
    FakeES.made = 0
    stubFetch((url) => (url.endsWith('/inbox/unread') ? { status: 200, body: { unread: 4 } } : { status: 200, body: { items: [], unread: 4 } }))
    const w = mountView(HeaderBell)
    await flushPromises()
    expect(w.find('[data-test="bell-badge"]').text()).toContain('4')
    expect(FakeES.made).toBeGreaterThan(0)
    expect(w.find('[style]').exists()).toBe(false)
    w.unmount()
  })
  it('log: stats tiles and audit rows with resolved names for ability holders; backup dialog gated', async () => {
    stubFetch((url) => {
      if (url.endsWith('/api/v1/users/lookup')) return { status: 200, body: { items: [{ id: 'u1', display_name: 'Ana' }] } }
      if (url.startsWith('/api/v1/roles')) return { status: 200, body: [] }
      if (url.includes('/stats')) return { status: 200, body: { channels: 2, templates: 3, notifications: { sent: 4 }, messages: { draft: 1 }, open_streams: 5, operations_24h: 6 } }
      if (url.includes('/audit')) return { status: 200, body: { items: [{ ts: '2024-01-01T00:00:00Z', event_type: 'channel_created', actor_kind: 'user', actor_id: 'u1', subject_name: 'relay', outcome: 'ok', details: {} }] } }
      return { status: 200, body: { items: [] } }
    })
    const w = mountView(Log)
    await flushPromises()
    expect(w.find('[data-test="stat-channels"]').text()).toContain('2')
    expect(w.find('[data-test="ops-backup"]').exists()).toBe(true)
    await w.findAll('[role=tab]')[1]!.trigger('click')
    await flushPromises()
    expect(w.find('[data-test="audit-table"]').text()).toContain('Ana')
    expect(w.find('[data-test="audit-table"]').text()).toContain('relay')
    w.unmount()
    const plain = mountView(Log, [{ action: 'read', subject: 'Inbox' }])
    await flushPromises()
    expect(plain.find('[data-test="stat-channels"]').exists()).toBe(false)
    expect(plain.find('[data-test="ops-backup"]').exists()).toBe(false)
    plain.unmount()
  })
})
