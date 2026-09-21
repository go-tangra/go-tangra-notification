<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useMessages } from '@/stores/messages'
import { useCategories } from '@/stores/categories'
import { describe } from '@/api/client'
import MessageDrawer from '@/components/MessageDrawer.vue'
import type { Message } from '@/api/types'

const store = useMessages()
const categories = useCategories()
const drawer = ref(false)
const selected = ref<Message | null>(null)
const status = ref('')
const err = ref('')

async function reload(): Promise<void> {
  await store.list({ status: status.value || undefined })
}

onMounted(async () => {
  await Promise.all([reload(), categories.list()])
})

function open(m: Message | null): void {
  selected.value = m
  drawer.value = true
}

const statusColor: Record<string, string> = { draft: 'grey', scheduled: 'info', published: 'success', revoked: 'warning', archived: 'grey' }

async function act(m: Message, action: 'cancel' | 'revoke' | 'archive' | 'remove'): Promise<void> {
  err.value = ''
  try {
    if (action === 'remove') await store.remove(m.id)
    else await store.transition(m.id, action)
    await reload()
  } catch (e) {
    err.value = describe(e)
  }
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <h1 class="text-h5">Messages</h1>
      <v-spacer />
      <v-select v-model="status" :items="['', 'draft', 'scheduled', 'published', 'revoked', 'archived']" label="Status" density="compact" hide-details style="max-width: 180px" class="mr-3" data-test="message-status-filter" @update:model-value="reload" />
      <v-btn color="primary" prepend-icon="mdi-plus" data-test="message-new" @click="open(null)">New message</v-btn>
    </div>
    <v-alert v-if="err || store.error" type="error" variant="tonal" density="compact" class="mb-3" data-test="message-list-error">{{ err || store.error }}</v-alert>
    <v-table data-test="messages-table">
      <thead>
        <tr><th>Title</th><th>Status</th><th>Category</th><th>Recipients</th><th>Read</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="m in store.items" :key="m.id" :data-test="'message-row-' + m.id">
          <td class="cursor-pointer" @click="open(m)">{{ m.title }}</td>
          <td><v-chip size="x-small" :color="statusColor[m.status]" variant="tonal">{{ m.status }}</v-chip></td>
          <td>{{ m.category_name }}</td>
          <td>{{ m.recipients.all ? 'everyone' : m.recipient_count ?? (m.recipients.users?.length ?? 0) }}</td>
          <td>{{ m.read_count ?? 0 }}</td>
          <td class="text-right text-no-wrap">
            <v-btn v-if="m.status === 'scheduled'" size="x-small" variant="text" :data-test="'message-cancel-' + m.id" @click="act(m, 'cancel')">Cancel</v-btn>
            <v-btn v-if="m.status === 'published'" size="x-small" variant="text" :data-test="'message-revoke-' + m.id" @click="act(m, 'revoke')">Revoke</v-btn>
            <v-btn v-if="m.status === 'published' || m.status === 'revoked'" size="x-small" variant="text" :data-test="'message-archive-' + m.id" @click="act(m, 'archive')">Archive</v-btn>
            <v-btn v-if="m.status === 'draft' || m.status === 'archived' || m.status === 'revoked'" icon="mdi-delete-outline" size="x-small" variant="text" :data-test="'message-remove-' + m.id" @click="act(m, 'remove')" />
          </td>
        </tr>
        <tr v-if="!store.items.length && !store.loading">
          <td colspan="6" class="text-medium-emphasis">No messages.</td>
        </tr>
      </tbody>
    </v-table>
    <MessageDrawer v-model="drawer" :message="selected" :categories="categories.items" @saved="reload" @sent="reload" />
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}
</style>
