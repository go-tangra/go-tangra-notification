<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useInbox } from '@/stores/inbox'
import { useLive } from '@/stores/live'

// Shown in the shell app bar. Opens the shared live stream, keeps the unread
// badge current and offers a short preview of the newest entries. The shell
// only mounts it for a person who can reach the module (inbox:read).
const inbox = useInbox()
const live = useLive()
const open = ref(false)
let release: (() => void) | null = null

const recent = computed(() => inbox.items.slice(0, 6))

onMounted(async () => {
  await inbox.refreshUnread()
  release = live.connect()
})

onUnmounted(() => {
  release?.()
  release = null
})

async function toggle(): Promise<void> {
  open.value = !open.value
  if (open.value) await inbox.list(undefined)
}

async function markRead(id: string): Promise<void> {
  await inbox.read(id)
}
</script>

<template>
  <v-menu v-model="open" :close-on-content-click="false" location="bottom end" data-test="header-bell">
    <template #activator="{ props: menuProps }">
      <v-btn v-bind="menuProps" icon variant="text" aria-label="Inbox" data-test="bell-button" @click="toggle">
        <v-badge :model-value="inbox.unread > 0" :content="inbox.unread" color="error" data-test="bell-badge">
          <v-icon icon="mdi-bell-outline" />
        </v-badge>
      </v-btn>
    </template>
    <v-card min-width="340" max-width="420">
      <v-card-title class="d-flex align-center">
        Inbox
        <v-spacer />
        <v-btn size="small" variant="text" to="/notification/inbox" data-test="bell-open-inbox" @click="open = false">Open</v-btn>
      </v-card-title>
      <v-list density="compact" data-test="bell-list">
        <v-list-item
          v-for="e in recent"
          :key="e.id"
          :title="e.message.title"
          :subtitle="e.message.content"
          :data-test="'bell-item-' + e.id"
          @click="markRead(e.id)"
        >
          <template #prepend>
            <v-icon :icon="e.status === 'read' ? 'mdi-email-open-outline' : 'mdi-email-outline'" :color="e.status === 'read' ? undefined : 'primary'" />
          </template>
        </v-list-item>
        <v-list-item v-if="!recent.length" title="No messages." />
      </v-list>
    </v-card>
  </v-menu>
</template>
