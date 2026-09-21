<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useTemplates } from '@/stores/templates'
import { useChannels } from '@/stores/channels'
import TemplateDrawer from '@/components/TemplateDrawer.vue'
import type { Template } from '@/api/types'

const store = useTemplates()
const channels = useChannels()
const drawer = ref(false)
const selected = ref<Template | null>(null)
const query = ref('')

onMounted(async () => {
  await Promise.all([store.list(), channels.list()])
})

let timer: ReturnType<typeof setTimeout> | undefined
watch(query, (q) => {
  clearTimeout(timer)
  timer = setTimeout(() => store.list(undefined, q || undefined), 200)
})

function open(t: Template | null): void {
  selected.value = t
  drawer.value = true
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <h1 class="text-h5">Templates</h1>
      <v-spacer />
      <v-text-field v-model="query" prepend-inner-icon="mdi-magnify" label="Search" density="compact" hide-details style="max-width: 240px" class="mr-3" data-test="template-search" />
      <v-btn color="primary" prepend-icon="mdi-plus" :disabled="!channels.items.length" data-test="template-new" @click="open(null)">New template</v-btn>
    </div>
    <v-alert v-if="store.error" type="error" variant="tonal" density="compact" class="mb-3">{{ store.error }}</v-alert>
    <v-table data-test="templates-table">
      <thead>
        <tr><th>Name</th><th>Channel</th><th>Subject</th><th>Default</th></tr>
      </thead>
      <tbody>
        <tr v-for="t in store.items" :key="t.id" class="cursor-pointer" :data-test="'template-row-' + t.id" @click="open(t)">
          <td>{{ t.name }}</td>
          <td>{{ t.channel_name }} <span class="text-medium-emphasis">({{ t.channel_type }})</span></td>
          <td class="text-truncate" style="max-width: 320px">{{ t.subject }}</td>
          <td><v-icon v-if="t.is_default" icon="mdi-star" size="small" color="amber" /></td>
        </tr>
        <tr v-if="!store.items.length && !store.loading">
          <td colspan="4" class="text-medium-emphasis">No templates.</td>
        </tr>
      </tbody>
    </v-table>
    <TemplateDrawer v-model="drawer" :template="selected" :channels="channels.items" @saved="store.list()" @removed="store.list()" />
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}
</style>
