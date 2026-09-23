<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { UiPage, UiBadge, UiCard, UiTabs, UiIcon, UiEmptyState, UiButton, UiLiveIndicator, UiDrawer, type TabItem } from '@freya/ui'
import { useInbox } from '@/stores/inbox'
import { useLive } from '@/stores/live'
import type { InboxEntry } from '@/api/types'

const inbox = useInbox()
const live = useLive()
const filter = ref('all')
const open = ref(false)
const detail = ref<InboxEntry | null>(null)
const tabs: TabItem[] = [{ key: 'all', label: 'All' }, { key: 'unread', label: 'Unread' }, { key: 'read', label: 'Read' }]
let release: (() => void) | null = null
onMounted(async () => {
  await reload()
  release = live.connect()
})
onUnmounted(() => release?.())
async function reload(): Promise<void> {
  await inbox.list(filter.value === 'all' ? undefined : filter.value)
}
async function show(e: InboxEntry): Promise<void> {
  detail.value = await inbox.read(e.id)
  open.value = true
}
async function remove(): Promise<void> {
  if (!detail.value) return
  await inbox.remove([detail.value.id])
  open.value = false
}
</script>

<template>
  <UiPage title="Inbox">
    <template #badges><UiBadge v-if="inbox.unread" color="error" data-test="inbox-unread">{{ inbox.unread }} unread</UiBadge><UiLiveIndicator :connected="live.connected" /></template>
    <template #filters><UiTabs v-model="filter" :tabs="tabs" data-test="inbox-filter" @update:model-value="reload" /></template>
    <UiCard :padded="false">
      <UiEmptyState v-if="!inbox.items.length && !inbox.loading" title="Your inbox is empty" icon="mdi-email-open-outline" />
      <ul v-else class="divide-y divide-base-300" data-test="inbox-list">
        <li v-for="e in inbox.items" :key="e.id">
          <button type="button" class="flex w-full items-start gap-3 px-4 py-3 text-start hover:bg-base-200" :data-test="'inbox-item-' + e.id" @click="show(e)">
            <UiIcon :name="e.status === 'read' ? 'mdi-email-open-outline' : 'mdi-email-outline'" :class="e.status === 'read' ? 'text-base-content/70' : 'text-primary'" class="mt-0.5 shrink-0" />
            <span class="min-w-0 grow"><span class="block truncate" :class="e.status === 'read' ? '' : 'font-medium'">{{ e.message.title }}</span><span class="block truncate text-sm text-base-content/70">{{ e.message.content }}</span></span>
            <span class="shrink-0 text-xs text-base-content/70">{{ e.message.category_name }}</span>
          </button>
        </li>
      </ul>
    </UiCard>
    <UiDrawer v-model="open" :title="detail?.message.title ?? ''" size="md" data-test="inbox-detail">
      <template v-if="detail">
        <p class="mb-2 text-xs text-base-content/70">{{ detail.message.category_name }}</p>
        <p class="whitespace-pre-wrap break-words" data-test="inbox-body">{{ detail.message.content }}</p>
      </template>
      <template #actions>
        <UiButton v-if="detail" variant="text" color="error" :data-test="'inbox-remove-' + detail.id" @click="remove">Delete</UiButton>
        <UiButton @click="open = false">Close</UiButton>
      </template>
    </UiDrawer>
  </UiPage>
</template>
