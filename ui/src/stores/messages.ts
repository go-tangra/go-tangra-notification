import { defineStore } from 'pinia'
import { api } from '@/api/client'
import type { Message, MessageInput, SendResult } from '@/api/types'
import { listSpec, pagedList } from './paged'

export interface MessageFilter {
  status?: string | undefined
  category_id?: string | undefined
  q?: string | undefined
}

/** Sort fields of GET /messages (subject = the title). */
export const MESSAGE_LIST = listSpec(['created_at', 'subject', 'status'], 'created_at', 'desc')

export const useMessages = defineStore('notification-messages', () => {
  // One server page; without messages:manage the server pages only the caller's own.
  const page = pagedList<Message, MessageFilter>('messages', MESSAGE_LIST.first)

  async function get(id: string): Promise<Message> {
    return api<Message>('GET', 'messages/' + id)
  }

  async function create(input: MessageInput): Promise<Message> {
    return api<Message>('POST', 'messages', input)
  }

  async function update(id: string, input: MessageInput): Promise<Message> {
    const m = await api<Message>('PUT', 'messages/' + id, input)
    page.items.value = page.items.value.map((x) => (x.id === id ? m : x))
    return m
  }

  async function transition(id: string, action: 'send' | 'cancel' | 'revoke' | 'archive'): Promise<Message | SendResult> {
    const out = await api<Message | SendResult>('POST', 'messages/' + id + '/' + action)
    if (action === 'send') return out as SendResult
    page.items.value = page.items.value.map((x) => (x.id === id ? (out as Message) : x))
    return out
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'messages/' + id + '/remove')
    page.items.value = page.items.value.filter((x) => x.id !== id)
  }

  return { ...page, get, create, update, transition, remove }
})
