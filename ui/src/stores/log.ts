import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { LogEntry, Page } from '@/api/types'

export interface LogFilter {
  channel_id?: string | undefined
  template_id?: string | undefined
  recipient?: string | undefined
  status?: string | undefined
  from?: string | undefined
  to?: string | undefined
}

export const useLog = defineStore('notification-log', () => {
  const items = ref<LogEntry[]>([])
  const next = ref<string | undefined>()
  const loading = ref(false)
  const error = ref('')

  async function list(filter: LogFilter = {}, cursor?: string): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const page = await api<Page<LogEntry>>('GET', 'notifications', undefined, { query: { ...filter, cursor, limit: 50 } })
      items.value = cursor ? [...items.value, ...page.items] : page.items
      next.value = page.next_cursor
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function get(id: string): Promise<LogEntry> {
    return api<LogEntry>('GET', 'notifications/' + id)
  }

  async function send(input: { template_id: string; channel_id?: string; recipient: string; variables?: Record<string, string> }): Promise<LogEntry> {
    return api<LogEntry>('POST', 'notifications/send', input)
  }

  return { items, next, loading, error, list, get, send }
})
