import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Channel, ChannelInput, ChannelType, LogEntry } from '@/api/types'
import { listSpec, loadOptions, pagedList } from './paged'

/** Sort fields of GET /channels (contracts/sortable-fields.md). */
export const CHANNEL_LIST = listSpec(['name', 'type', 'created_at'], 'name')

export interface ChannelFilter {
  type?: ChannelType | undefined
}

export const useChannels = defineStore('notification-channels', () => {
  // One server page of the channels the caller may read (the server applies
  // the grants to the page and the total).
  const page = pagedList<Channel, ChannelFilter>('channels', CHANNEL_LIST.first)
  /** Up to 200 channels by name, for selects. */
  const options = ref<Channel[]>([])

  async function loadOptions_(): Promise<void> {
    try {
      options.value = await loadOptions<Channel>('channels', 'name')
    } catch (e) {
      page.error.value = (e as Error).message
    }
  }

  async function get(id: string): Promise<Channel> {
    return api<Channel>('GET', 'channels/' + id)
  }

  async function create(input: ChannelInput): Promise<Channel> {
    return api<Channel>('POST', 'channels', input)
  }

  async function update(id: string, input: ChannelInput): Promise<Channel> {
    const c = await api<Channel>('PUT', 'channels/' + id, input)
    page.items.value = page.items.value.map((x) => (x.id === id ? c : c.is_default && x.type === c.type ? { ...x, is_default: false } : x))
    return c
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'channels/' + id + '/remove')
    page.items.value = page.items.value.filter((x) => x.id !== id)
  }

  async function test(id: string, recipient: string): Promise<LogEntry> {
    return api<LogEntry>('POST', 'channels/' + id + '/test', { recipient })
  }

  return { ...page, options, loadOptions: loadOptions_, get, create, update, remove, test }
})
