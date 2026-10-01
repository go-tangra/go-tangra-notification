import { defineStore } from 'pinia'
import { api } from '@/api/client'
import type { Template, TemplateInput } from '@/api/types'
import { listSpec, pagedList } from './paged'

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

/** Sort fields of GET /templates (contracts/sortable-fields.md). */
export const TEMPLATE_LIST = listSpec(['name', 'channel', 'updated_at'], 'name')

export interface TemplateFilter {
  channel_id?: string | undefined
  q?: string | undefined
}

export const useTemplates = defineStore('notification-templates', () => {
  // One server page of the templates the caller may read.
  const page = pagedList<Template, TemplateFilter>('templates', TEMPLATE_LIST.first)

  async function get(id: string): Promise<Template> {
    return api<Template>('GET', 'templates/' + id)
  }

  async function create(input: TemplateInput): Promise<Template> {
    return api<Template>('POST', 'templates', input)
  }

  async function update(id: string, input: TemplateInput): Promise<Template> {
    const t = await api<Template>('PUT', 'templates/' + id, input)
    page.items.value = page.items.value.map((x) => (x.id === id ? t : t.is_default && x.channel_id === t.channel_id ? { ...x, is_default: false } : x))
    return t
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'templates/' + id + '/remove')
    page.items.value = page.items.value.filter((x) => x.id !== id)
  }

  /** Resets a system template to its built-in subject and body. */
  async function restore(id: string): Promise<Template> {
    const t = await api<Template>('POST', 'templates/' + id + '/restore')
    page.items.value = page.items.value.map((x) => (x.id === id ? t : x))
    return t
  }

  async function preview(input: PreviewInput): Promise<Preview> {
    return api<Preview>('POST', 'templates/preview', input)
  }

  return { ...page, get, create, update, remove, restore, preview }
})
