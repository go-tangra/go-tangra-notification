import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Page, Template, TemplateInput } from '@/api/types'

export interface Preview {
  rendered_subject: string
  rendered_body: string
}

export interface PreviewInput {
  template_id?: string | undefined
  channel_type?: string | undefined
  subject: string
  body: string
  variables?: string[]
  values?: Record<string, string>
}

export const useTemplates = defineStore('notification-templates', () => {
  const items = ref<Template[]>([])
  const next = ref<string | undefined>()
  const loading = ref(false)
  const error = ref('')

  async function list(channelId?: string, q?: string, cursor?: string): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const page = await api<Page<Template>>('GET', 'templates', undefined, { query: { channel_id: channelId, q, cursor, limit: 50 } })
      items.value = cursor ? [...items.value, ...page.items] : page.items
      next.value = page.next_cursor
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function get(id: string): Promise<Template> {
    return api<Template>('GET', 'templates/' + id)
  }

  async function create(input: TemplateInput): Promise<Template> {
    const t = await api<Template>('POST', 'templates', input)
    items.value = [t, ...items.value]
    return t
  }

  async function update(id: string, input: TemplateInput): Promise<Template> {
    const t = await api<Template>('PUT', 'templates/' + id, input)
    items.value = items.value.map((x) => (x.id === id ? t : x))
    if (t.is_default) items.value = items.value.map((x) => (x.id !== id && x.channel_id === t.channel_id ? { ...x, is_default: false } : x))
    return t
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'templates/' + id + '/remove')
    items.value = items.value.filter((x) => x.id !== id)
  }

  async function preview(input: PreviewInput): Promise<Preview> {
    return api<Preview>('POST', 'templates/preview', input)
  }

  return { items, next, loading, error, list, get, create, update, remove, preview }
})
