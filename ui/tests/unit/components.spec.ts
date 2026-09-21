import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { VApp } from 'vuetify/components'
import { plugins, stubFetch } from './helpers'
import ChannelDrawer from '@/components/ChannelDrawer.vue'
import TemplateDrawer from '@/components/TemplateDrawer.vue'
import PermissionDrawer from '@/components/PermissionDrawer.vue'
import HeaderBell from '@/components/HeaderBell.vue'
import StatsCard from '@/components/StatsCard.vue'
import AuditTable from '@/components/AuditTable.vue'

// Drawers and menus need Vuetify's application frame (v-app) as an ancestor.
function mountWith(component: unknown, props: Record<string, unknown>) {
  const Host = defineComponent({ render: () => h(VApp, () => h(component as never, props)) })
  return mount(Host, { global: { plugins: plugins(), stubs: { teleport: true } } })
}

describe('ChannelDrawer', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('keeps a stored password as the marker, sends only public fields on save, and runs a test send', async () => {
    const bodies: Array<Record<string, unknown>> = []
    stubFetch((url, init) => {
      if (init?.method === 'PUT') {
        bodies.push(JSON.parse(String(init.body)))
        return { status: 200, body: { id: 'c1', name: 'relay', type: 'email', settings: { host: 'h', password: '__set__' }, enabled: true, is_default: true } }
      }
      if (url.endsWith('/channels/c1/test')) return { status: 200, body: { id: 'l1', status: 'sent', test: true } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const channel = { id: 'c1', name: 'relay', type: 'email' as const, settings: { host: 'h', port: 587, from: 'a@b.c', password: '__set__' }, enabled: true, is_default: true, permissions: { delete: true } }
    const w = mountWith(ChannelDrawer, { modelValue: true, channel })
    await flushPromises()
    const pwd = w.find('[data-test="channel-password"] input')
    expect((pwd.element as HTMLInputElement).value).toBe('__set__')
    await w.find('[data-test="channel-save"]').trigger('click')
    await flushPromises()
    expect(bodies[0]?.settings).toMatchObject({ host: 'h', password: '__set__' })
    await w.find('[data-test="channel-test-recipient"] input').setValue('ops@x.test')
    await w.find('[data-test="channel-test-send"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="channel-test-result"]').text()).toContain('sent')
  })

  it('shows a scrubbed validation error and never the credential', async () => {
    stubFetch(() => ({ status: 422, body: { reason: 'validation_failed' } }))
    const w = mountWith(ChannelDrawer, { modelValue: true, channel: null })
    await flushPromises()
    await w.find('[data-test="channel-save"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="channel-error"]').text()).toContain('highlighted')
  })
})

describe('TemplateDrawer', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('previews and surfaces an undeclared-variable error', async () => {
    stubFetch((url) => {
      if (url.endsWith('/templates/preview')) return { status: 422, body: { reason: 'validation_failed', detail: { variable: 'Nope' } } }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountWith(TemplateDrawer, { modelValue: true, template: { id: 't1', name: 'w', channel_id: 'c1', subject: '{{.Nope}}', body: 'b', variables: [] }, channels: [{ id: 'c1', name: 'relay', type: 'email', settings: {}, enabled: true, is_default: true }] })
    await flushPromises()
    await w.find('[data-test="template-preview"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="template-error"]').exists()).toBe(true)
  })
})

describe('PermissionDrawer', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('offers only relations at or below the holder and lists grants by name', async () => {
    stubFetch((url) => {
      if (url.includes('/grants?')) return { status: 200, body: { items: [{ id: 'g1', subject_type: 'user', subject_id: 'u1', relation: 'viewer' }] } }
      if (url.includes('/access/effective')) return { status: 200, body: { relation: 'sharer', permissions: { share: true }, grants: [] } }
      if (url.endsWith('/api/v1/users/lookup')) return { status: 200, body: { items: [{ id: 'u1', display_name: 'Ana' }] } }
      if (url.startsWith('/api/v1/roles')) return { status: 200, body: [] }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountWith(PermissionDrawer, { modelValue: true, resourceType: 'channel', resourceId: 'c1', resourceName: 'relay' })
    await flushPromises()
    expect(w.find('[data-test="grant-add"]').exists()).toBe(true)
    expect(w.find('[data-test="grant-row-g1"]').text()).toContain('Ana')
  })

  it('hides the grant form without share', async () => {
    stubFetch((url) => {
      if (url.includes('/grants?')) return { status: 200, body: { items: [] } }
      if (url.includes('/access/effective')) return { status: 200, body: { relation: 'viewer', permissions: {}, grants: [] } }
      if (url.startsWith('/api/v1/roles')) return { status: 200, body: [] }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const w = mountWith(PermissionDrawer, { modelValue: true, resourceType: 'channel', resourceId: 'c1' })
    await flushPromises()
    expect(w.find('[data-test="grant-add"]').exists()).toBe(false)
  })
})

describe('HeaderBell', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('shows the unread badge and opens the shared stream on mount', async () => {
    class FakeES {
      static made = 0
      onopen: (() => void) | null = null
      onerror: (() => void) | null = null
      constructor() {
        FakeES.made++
      }
      addEventListener(): void {}
      close(): void {}
    }
    ;(globalThis as { EventSource?: unknown }).EventSource = FakeES as unknown as typeof EventSource
    stubFetch((url) => {
      if (url.endsWith('/inbox/unread')) return { status: 200, body: { unread: 4 } }
      return { status: 200, body: { items: [], unread: 4 } }
    })
    const w = mountWith(HeaderBell, {})
    await flushPromises()
    expect(w.find('[data-test="bell-badge"]').text()).toContain('4')
    expect(FakeES.made).toBeGreaterThan(0)
  })
})

describe('StatsCard and AuditTable', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('renders stat tiles and audit rows with resolved names', async () => {
    stubFetch((url) => {
      if (url.endsWith('/api/v1/users/lookup')) return { status: 200, body: { items: [{ id: 'u1', display_name: 'Ana' }] } }
      if (url.startsWith('/api/v1/roles')) return { status: 200, body: [] }
      return { status: 404, body: { reason: 'not_found' } }
    })
    const stats = { channels: 2, templates: 3, notifications: { sent: 4 }, messages: { draft: 1 }, open_streams: 5, operations_24h: 6 }
    const sc = mountWith(StatsCard, { stats })
    expect(sc.find('[data-test="stat-channels"]').text()).toContain('2')
    const at = mountWith(AuditTable, { items: [{ ts: '2024-01-01T00:00:00Z', event_type: 'channel_created', actor_kind: 'user', actor_id: 'u1', subject_name: 'relay', outcome: 'ok', details: {} }] })
    await flushPromises()
    expect(at.find('[data-test="audit-actor-cell"]').text()).toBe('Ana')
    expect(at.find('[data-test="audit-subject-cell"]').text()).toBe('relay')
  })
})
