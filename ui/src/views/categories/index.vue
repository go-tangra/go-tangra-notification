<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { UiPage, UiAlert, UiCard, UiButton, UiDataTable, UiRecordDrawer, useConfirm, useListQuery, type Column } from '@go-tangra/ui'
import { zodToFields } from '@go-tangra/ui/forms'
import { CATEGORY_LIST, useCategories } from '@/stores/categories'
import { describe } from '@/api/client'
import { categorySchema } from '@/schemas'
import type { Category } from '@/api/types'

const store = useCategories()
const confirm = useConfirm()
const dialog = ref(false)
const editing = ref<Category | null>(null)
const error = ref('')
// --- server paging and sorting (page / size / sort in the URL: ?categories.page=…) ---
const lq = useListQuery('categories', CATEGORY_LIST.opts)
async function load(): Promise<void> {
  const res = await store.list({}, lq.query.value)
  if (res?.page) lq.clampTo(res.page) // a page beyond the end answers the last page
}
watch(lq.query, () => void load())
onMounted(() => void load())
const fields = zodToFields(categorySchema, { description: { type: 'textarea', cols: 12 }, sort: { label: 'Sort', cols: 4 } })
function open(c: Category | null): void {
  editing.value = c
  error.value = ''
  dialog.value = true
}
const submit = (v: Record<string, unknown>) => (editing.value ? store.update(editing.value.id, v as { name: string; description?: string; sort: number }) : store.create(v as { name: string; description?: string; sort: number }))
async function remove(c: Category): Promise<void> {
  if (!(await confirm.ask({ title: `Delete ${c.name}?`, danger: true, confirmLabel: 'Delete' }))) return
  error.value = ''
  try {
    await store.remove(c.id)
  } catch (e) {
    error.value = describe(e)
  }
}
const columns: Column<Category>[] = [
  { key: 'sort_order', label: 'Sort', width: 'sm', align: 'end', format: (c) => String(c.sort ?? 0), sortable: true },
  { key: 'name', label: 'Name', sortable: true },
  { key: 'description', label: 'Description', hideOnStack: true },
  { key: 'message_count', label: 'Messages', align: 'end', format: (c) => String(c.message_count ?? 0) },
]
</script>

<template>
  <UiPage title="Categories">
    <template #actions><UiButton icon="mdi-plus" data-test="category-new" @click="open(null)">New category</UiButton></template>
    <UiAlert v-if="error" kind="error" class="mb-3" data-test="category-error">{{ error }}</UiAlert>
    <UiCard :padded="false">
      <UiDataTable :items="store.items" :columns="columns" :loading="store.loading" :total="store.total" :page="lq.page.value" :page-size="lq.pageSize.value" :sort="lq.sort.value" caption="Categories" empty-title="No categories" clickable :row-attrs="(c) => ({ 'data-test': 'category-row-' + c.id })" data-test="categories-table" @row-click="open" @update:page="lq.setPage" @update:page-size="lq.setPageSize" @update:sort="lq.setSort">
        <template #actions="{ row }">
          <UiButton size="xs" variant="text" icon="mdi-pencil" icon-only label="Edit" :data-test="'category-edit-' + row.id" @click="open(row)" />
          <UiButton size="xs" variant="text" icon="mdi-delete-outline" icon-only label="Delete" :data-test="'category-delete-' + row.id" @click="remove(row)" />
        </template>
      </UiDataTable>
    </UiCard>
    <UiRecordDrawer v-model="dialog" close-on-save :title="editing ? 'Edit category' : 'New category'" :schema="categorySchema" :fields="fields" :initial="editing ?? { sort: 0 }" :submit="submit" size="md" data-test="category-dialog" />
  </UiPage>
</template>
