<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { UiIcon } from '@go-tangra/ui'
import { useInbox } from '@/stores/inbox'
import { useLive } from '@/stores/live'

// Shown in the shell app bar (./header). Opens the shared live stream, keeps
// the unread badge current and offers a short preview of the newest entries.
// The shell only mounts it for a person who can reach the module (inbox:read).
const inbox = useInbox()
const live = useLive()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
let release: (() => void) | null = null
const recent = computed(() => inbox.items.slice(0, 6))

function onDoc(e: MouseEvent): void {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}
function onKey(e: KeyboardEvent): void {
  if (e.key === 'Escape') open.value = false
}
onMounted(async () => {
  document.addEventListener('mousedown', onDoc)
  document.addEventListener('keydown', onKey)
  await inbox.refreshUnread()
  release = live.connect()
})
onUnmounted(() => {
  document.removeEventListener('mousedown', onDoc)
  document.removeEventListener('keydown', onKey)
  release?.()
  release = null
})
async function toggle(): Promise<void> {
  open.value = !open.value
  if (open.value) await inbox.list(undefined)
}
</script>

<template>
  <div ref="root" class="relative" data-test="header-bell">
    <button type="button" class="btn btn-text btn-circle btn-sm relative" aria-label="Inbox" :aria-expanded="open" aria-haspopup="dialog" data-test="bell-button" @click="toggle">
      <UiIcon name="mdi-bell-outline" />
      <span v-if="inbox.unread > 0" class="badge badge-error badge-xs absolute -top-1 -end-1" data-test="bell-badge">{{ inbox.unread }}</span>
    </button>
    <div v-if="open" role="dialog" aria-label="Inbox" class="absolute end-0 z-50 mt-1 w-80 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
      <div class="mb-1 flex items-center gap-2 px-2">
        <span class="font-medium">Inbox</span>
        <span class="grow" />
        <RouterLink to="/notification/inbox" class="btn btn-text btn-xs" data-test="bell-open-inbox" @click="open = false">Open</RouterLink>
      </div>
      <ul class="menu w-full p-0" data-test="bell-list">
        <li v-for="e in recent" :key="e.id">
          <button type="button" class="flex items-start gap-2" :data-test="'bell-item-' + e.id" @click="inbox.read(e.id)">
            <UiIcon :name="e.status === 'read' ? 'mdi-email-open-outline' : 'mdi-email-outline'" size="sm" :class="e.status === 'read' ? 'text-base-content/70' : 'text-primary'" />
            <span class="min-w-0"><span class="block truncate text-sm" :class="e.status === 'read' ? '' : 'font-medium'">{{ e.message.title }}</span><span class="block truncate text-xs text-base-content/70">{{ e.message.content }}</span></span>
          </button>
        </li>
        <li v-if="!recent.length" class="px-2 py-1 text-xs text-base-content/70">No messages.</li>
      </ul>
    </div>
  </div>
</template>
