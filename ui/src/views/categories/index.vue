<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useCategories } from '@/stores/categories'
import { describe } from '@/api/client'
import type { Category } from '@/api/types'

const store = useCategories()
const dialog = ref(false)
const editing = ref<Category | null>(null)
const form = reactive({ name: '', description: '', sort: 0 })
const err = ref('')
const busy = ref(false)

onMounted(() => store.list())

function open(c: Category | null): void {
  editing.value = c
  form.name = c?.name ?? ''
  form.description = c?.description ?? ''
  form.sort = c?.sort ?? 0
  err.value = ''
  dialog.value = true
}

async function save(): Promise<void> {
  busy.value = true
  err.value = ''
  try {
    if (editing.value) await store.update(editing.value.id, { name: form.name, description: form.description, sort: form.sort })
    else await store.create({ name: form.name, description: form.description, sort: form.sort })
    dialog.value = false
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}

async function remove(c: Category): Promise<void> {
  err.value = ''
  try {
    await store.remove(c.id)
  } catch (e) {
    err.value = describe(e)
  }
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <h1 class="text-h5">Categories</h1>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" data-test="category-new" @click="open(null)">New category</v-btn>
    </div>
    <v-alert v-if="err" type="error" variant="tonal" density="compact" class="mb-3" data-test="category-error">{{ err }}</v-alert>
    <v-table data-test="categories-table">
      <thead>
        <tr><th>Sort</th><th>Name</th><th>Description</th><th>Messages</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="c in store.items" :key="c.id" :data-test="'category-row-' + c.id">
          <td>{{ c.sort }}</td>
          <td class="cursor-pointer" @click="open(c)">{{ c.name }}</td>
          <td>{{ c.description }}</td>
          <td>{{ c.message_count ?? 0 }}</td>
          <td class="text-right">
            <v-btn icon="mdi-pencil" size="x-small" variant="text" :data-test="'category-edit-' + c.id" @click="open(c)" />
            <v-btn icon="mdi-delete-outline" size="x-small" variant="text" :data-test="'category-delete-' + c.id" @click="remove(c)" />
          </td>
        </tr>
        <tr v-if="!store.items.length && !store.loading">
          <td colspan="5" class="text-medium-emphasis">No categories.</td>
        </tr>
      </tbody>
    </v-table>
    <v-dialog v-model="dialog" max-width="480" data-test="category-dialog">
      <v-card :title="editing ? 'Edit category' : 'New category'">
        <v-card-text>
          <v-text-field v-model="form.name" label="Name" density="compact" data-test="category-name" />
          <v-textarea v-model="form.description" label="Description" rows="2" density="compact" data-test="category-description" />
          <v-text-field v-model.number="form.sort" label="Sort" type="number" density="compact" data-test="category-sort" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="dialog = false">Cancel</v-btn>
          <v-btn color="primary" :loading="busy" data-test="category-save" @click="save">Save</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}
</style>
