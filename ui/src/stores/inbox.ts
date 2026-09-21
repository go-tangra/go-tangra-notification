import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { InboxEntry, InboxPage } from '@/api/types'

export const useInbox = defineStore('notification-inbox', () => {
  const items = ref<InboxEntry[]>([])
  const unread = ref(0)
  const next = ref<string | undefined>()
  const loading = ref(false)
  const error = ref('')

  async function list(status?: string, cursor?: string): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const page = await api<InboxPage>('GET', 'inbox', undefined, { query: { status, cursor, limit: 50 } })
      items.value = cursor ? [...items.value, ...page.items] : page.items
      unread.value = page.unread
      next.value = page.next_cursor
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function refreshUnread(): Promise<void> {
    try {
      const out = await api<{ unread: number }>('GET', 'inbox/unread')
      unread.value = out.unread
    } catch {
      /* keep the last count */
    }
  }

  async function read(id: string): Promise<InboxEntry> {
    const e = await api<InboxEntry>('GET', 'inbox/' + id)
    items.value = items.value.map((x) => (x.id === id ? e : x))
    await refreshUnread()
    return e
  }

  async function setStatus(ids: string[], status: 'read' | 'unread' | 'received'): Promise<void> {
    const out = await api<{ updated: number; unread: number }>('POST', 'inbox/status', { ids, status })
    unread.value = out.unread
    items.value = items.value.map((x) => (ids.includes(x.id) ? { ...x, status: status === 'unread' ? 'sent' : status === 'read' ? 'read' : x.status } : x))
  }

  async function remove(ids: string[]): Promise<void> {
    const out = await api<{ updated: number; unread: number }>('POST', 'inbox/remove', { ids })
    unread.value = out.unread
    items.value = items.value.filter((x) => !ids.includes(x.id))
  }

  /** Applied when a live inbox event arrives: bump the counter and prepend. */
  function onLive(entry?: InboxEntry): void {
    unread.value += 1
    if (entry) items.value = [entry, ...items.value]
  }

  function onRevoked(messageId: string): void {
    const before = items.value.length
    items.value = items.value.filter((x) => !(x.message.id === messageId && x.status !== 'read'))
    if (items.value.length < before) void refreshUnread()
  }

  return { items, unread, next, loading, error, list, refreshUnread, read, setStatus, remove, onLive, onRevoked }
})
