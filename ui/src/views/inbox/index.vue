<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useInbox } from '@/stores/inbox'
import { useLive } from '@/stores/live'
import type { InboxEntry } from '@/api/types'

const inbox = useInbox()
const live = useLive()
const filter = ref('all')
const open = ref(false)
const detail = ref<InboxEntry | null>(null)
let release: (() => void) | null = null

onMounted(async () => {
  await inbox.list(filter.value === 'all' ? undefined : filter.value)
  release = live.connect()
})

onUnmounted(() => {
  release?.()
})

async function reload(): Promise<void> {
  await inbox.list(filter.value === 'all' ? undefined : filter.value)
}

async function show(e: InboxEntry): Promise<void> {
  detail.value = await inbox.read(e.id)
  open.value = true
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <h1 class="text-h5">Inbox</h1>
      <v-chip v-if="inbox.unread" color="error" size="small" class="ml-3" data-test="inbox-unread">{{ inbox.unread }} unread</v-chip>
      <v-spacer />
      <v-btn-toggle v-model="filter" density="compact" mandatory data-test="inbox-filter" @update:model-value="reload">
        <v-btn value="all" size="small">All</v-btn>
        <v-btn value="unread" size="small">Unread</v-btn>
        <v-btn value="read" size="small">Read</v-btn>
      </v-btn-toggle>
    </div>
    <v-list lines="two" data-test="inbox-list">
      <v-list-item
        v-for="e in inbox.items"
        :key="e.id"
        :title="e.message.title"
        :subtitle="e.message.content"
        :data-test="'inbox-item-' + e.id"
        @click="show(e)"
      >
        <template #prepend>
          <v-icon :icon="e.status === 'read' ? 'mdi-email-open-outline' : 'mdi-email-outline'" :color="e.status === 'read' ? undefined : 'primary'" />
        </template>
        <template #append>
          <span class="text-caption text-medium-emphasis">{{ e.message.category_name }}</span>
        </template>
      </v-list-item>
      <v-list-item v-if="!inbox.items.length && !inbox.loading" title="Your inbox is empty." />
    </v-list>
    <v-dialog v-model="open" max-width="600" data-test="inbox-detail">
      <v-card v-if="detail" :title="detail.message.title">
        <v-card-text>
          <div class="text-caption text-medium-emphasis mb-2">{{ detail.message.category_name }}</div>
          <div style="white-space: pre-wrap" data-test="inbox-body">{{ detail.message.content }}</div>
        </v-card-text>
        <v-card-actions>
          <v-btn variant="text" color="error" :data-test="'inbox-remove-' + detail.id" @click="inbox.remove([detail.id]); open = false">Delete</v-btn>
          <v-spacer />
          <v-btn @click="open = false">Close</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
