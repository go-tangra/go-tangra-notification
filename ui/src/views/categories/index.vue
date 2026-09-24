<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { UiPage, UiAlert, UiCard, UiButton, UiDataTable, UiRecordDrawer, useConfirm, type Column } from '@go-tangra/ui'
import { zodToFields } from '@go-tangra/ui/forms'
import { useCategories } from '@/stores/categories'
import { describe } from '@/api/client'
import { categorySchema } from '@/schemas'
import type { Category } from '@/api/types'

const store = useCategories()
const confirm = useConfirm()
const dialog = ref(false)
const editing = ref<Category | null>(null)
const error = ref('')
onMounted(() => store.list())
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
  { key: 'sort', label: 'Sort', width: 'sm', align: 'end', sortable: true },
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
      <UiDataTable :items="store.items" :columns="columns" :loading="store.loading" caption="Categories" empty-title="No categories" clickable :row-attrs="(c) => ({ 'data-test': 'category-row-' + c.id })" data-test="categories-table" @row-click="open">
        <template #actions="{ row }">
          <UiButton size="xs" variant="text" icon="mdi-pencil" icon-only label="Edit" :data-test="'category-edit-' + row.id" @click="open(row)" />
          <UiButton size="xs" variant="text" icon="mdi-delete-outline" icon-only label="Delete" :data-test="'category-delete-' + row.id" @click="remove(row)" />
        </template>
      </UiDataTable>
    </UiCard>
    <UiRecordDrawer v-model="dialog" close-on-save :title="editing ? 'Edit category' : 'New category'" :schema="categorySchema" :fields="fields" :initial="editing ?? { sort: 0 }" :submit="submit" size="md" data-test="category-dialog" />
  </UiPage>
</template>
