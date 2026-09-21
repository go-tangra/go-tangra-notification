<script setup lang="ts">
import { computed } from 'vue'
import type { Stats } from '@/api/types'

const props = defineProps<{ stats: Stats | null }>()

const tiles = computed(() => {
  const s = props.stats
  if (!s) return []
  const sum = (m: Record<string, number>): number => Object.values(m).reduce((a, b) => a + b, 0)
  return [
    { key: 'channels', label: 'Channels', value: s.channels, icon: 'mdi-radio-tower' },
    { key: 'templates', label: 'Templates', value: s.templates, icon: 'mdi-file-document-edit-outline' },
    { key: 'notifications', label: 'Sent (24h)', value: sum(s.notifications), icon: 'mdi-send-outline' },
    { key: 'messages', label: 'Messages', value: sum(s.messages), icon: 'mdi-message-text-outline' },
    { key: 'streams', label: 'Open streams', value: s.open_streams, icon: 'mdi-broadcast' },
    { key: 'ops', label: 'Operations (24h)', value: s.operations_24h, icon: 'mdi-pulse' },
  ]
})
</script>

<template>
  <v-row data-test="stats-card">
    <v-col v-for="t in tiles" :key="t.key" cols="6" md="2">
      <v-card variant="tonal" :data-test="'stat-' + t.key">
        <v-card-text class="d-flex flex-column align-center">
          <v-icon :icon="t.icon" size="28" class="mb-1" />
          <div class="text-h5">{{ t.value }}</div>
          <div class="text-caption text-medium-emphasis">{{ t.label }}</div>
        </v-card-text>
      </v-card>
    </v-col>
  </v-row>
</template>
