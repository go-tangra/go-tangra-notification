import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Channel, ChannelInput, ChannelType, LogEntry, Page } from '@/api/types'

export const useChannels = defineStore('notification-channels', () => {
  const items = ref<Channel[]>([])
  const next = ref<string | undefined>()
  const loading = ref(false)
  const error = ref('')

  async function list(type?: ChannelType, cursor?: string): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const page = await api<Page<Channel>>('GET', 'channels', undefined, { query: { type, cursor, limit: 50 } })
      items.value = cursor ? [...items.value, ...page.items] : page.items
      next.value = page.next_cursor
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function get(id: string): Promise<Channel> {
    return api<Channel>('GET', 'channels/' + id)
  }

  async function create(input: ChannelInput): Promise<Channel> {
    const c = await api<Channel>('POST', 'channels', input)
    items.value = [c, ...items.value]
    return c
  }

  async function update(id: string, input: ChannelInput): Promise<Channel> {
    const c = await api<Channel>('PUT', 'channels/' + id, input)
    items.value = items.value.map((x) => (x.id === id ? c : x))
    if (c.is_default) items.value = items.value.map((x) => (x.id !== id && x.type === c.type ? { ...x, is_default: false } : x))
    return c
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'channels/' + id + '/remove')
    items.value = items.value.filter((x) => x.id !== id)
  }

  async function test(id: string, recipient: string): Promise<LogEntry> {
    return api<LogEntry>('POST', 'channels/' + id + '/test', { recipient })
  }

  return { items, next, loading, error, list, get, create, update, remove, test }
})
