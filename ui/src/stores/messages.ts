import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Message, MessageInput, Page, SendResult } from '@/api/types'

export interface MessageFilter {
  status?: string | undefined
  category_id?: string | undefined
  q?: string | undefined
}

export const useMessages = defineStore('notification-messages', () => {
  const items = ref<Message[]>([])
  const next = ref<string | undefined>()
  const loading = ref(false)
  const error = ref('')

  async function list(filter: MessageFilter = {}, cursor?: string): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const page = await api<Page<Message>>('GET', 'messages', undefined, { query: { ...filter, cursor, limit: 50 } })
      items.value = cursor ? [...items.value, ...page.items] : page.items
      next.value = page.next_cursor
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function get(id: string): Promise<Message> {
    return api<Message>('GET', 'messages/' + id)
  }

  async function create(input: MessageInput): Promise<Message> {
    const m = await api<Message>('POST', 'messages', input)
    items.value = [m, ...items.value]
    return m
  }

  async function update(id: string, input: MessageInput): Promise<Message> {
    const m = await api<Message>('PUT', 'messages/' + id, input)
    items.value = items.value.map((x) => (x.id === id ? m : x))
    return m
  }

  async function transition(id: string, action: 'send' | 'cancel' | 'revoke' | 'archive'): Promise<Message | SendResult> {
    const out = await api<Message | SendResult>('POST', 'messages/' + id + '/' + action)
    if (action === 'send') return out as SendResult
    items.value = items.value.map((x) => (x.id === id ? (out as Message) : x))
    return out
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'messages/' + id + '/remove')
    items.value = items.value.filter((x) => x.id !== id)
  }

  return { items, next, loading, error, list, get, create, update, transition, remove }
})
