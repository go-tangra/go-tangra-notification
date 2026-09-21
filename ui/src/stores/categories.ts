import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Category, CategoryInput } from '@/api/types'

export const useCategories = defineStore('notification-categories', () => {
  const items = ref<Category[]>([])
  const loading = ref(false)
  const error = ref('')

  async function list(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const out = await api<{ items: Category[] }>('GET', 'categories')
      items.value = out.items
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function create(input: CategoryInput): Promise<Category> {
    const c = await api<Category>('POST', 'categories', input)
    await list()
    return c
  }

  async function update(id: string, input: CategoryInput): Promise<Category> {
    const c = await api<Category>('PUT', 'categories/' + id, input)
    await list()
    return c
  }

  async function remove(id: string): Promise<void> {
    await api('POST', 'categories/' + id + '/remove')
    items.value = items.value.filter((x) => x.id !== id)
  }

  return { items, loading, error, list, create, update, remove }
})
