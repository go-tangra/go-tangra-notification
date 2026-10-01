import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Category, CategoryInput } from '@/api/types'
import { listSpec, loadOptions, pagedList } from './paged'

/** Sort fields of GET /categories (sort_order = the administrator's order). */
export const CATEGORY_LIST = listSpec(['name', 'sort_order'], 'sort_order')

export const useCategories = defineStore('notification-categories', () => {
  const page = pagedList<Category>('categories', CATEGORY_LIST.first)
  /** Up to 200 categories in their sort order, for selects. */
  const options = ref<Category[]>([])

  async function loadOptions_(): Promise<void> {
    try {
      options.value = await loadOptions<Category>('categories', 'sort_order')
    } catch (e) {
      page.error.value = (e as Error).message
    }
  }

  async function create(input: CategoryInput): Promise<Category> {
    const c = await api<Category>('POST', 'categories', input)
    await page.reload()
    return c
  }

  async function update(id: string, input: CategoryInput): Promise<Category> {
    const c = await api<Category>('PUT', 'categories/' + id, input)
    await page.reload()
    return c
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'categories/' + id + '/remove')
    await page.reload()
  }

  return { ...page, options, loadOptions: loadOptions_, create, update, remove }
})
