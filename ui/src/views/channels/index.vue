<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useChannels } from '@/stores/channels'
import ChannelDrawer from '@/components/ChannelDrawer.vue'
import type { Channel } from '@/api/types'

const store = useChannels()
const drawer = ref(false)
const selected = ref<Channel | null>(null)

onMounted(() => store.list())

function open(c: Channel | null): void {
  selected.value = c
  drawer.value = true
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <h1 class="text-h5">Channels</h1>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" data-test="channel-new" @click="open(null)">New channel</v-btn>
    </div>
    <v-alert v-if="store.error" type="error" variant="tonal" density="compact" class="mb-3">{{ store.error }}</v-alert>
    <v-table data-test="channels-table">
      <thead>
        <tr><th>Name</th><th>Type</th><th>Status</th><th>Default</th><th>Templates</th></tr>
      </thead>
      <tbody>
        <tr v-for="c in store.items" :key="c.id" class="cursor-pointer" :data-test="'channel-row-' + c.id" @click="open(c)">
          <td>{{ c.name }}</td>
          <td><v-chip size="x-small" variant="tonal">{{ c.type }}</v-chip></td>
          <td>
            <v-chip size="x-small" :color="c.enabled ? 'success' : undefined" variant="tonal">{{ c.enabled ? 'enabled' : 'disabled' }}</v-chip>
          </td>
          <td><v-icon v-if="c.is_default" icon="mdi-star" size="small" color="amber" /></td>
          <td>{{ c.template_count ?? 0 }}</td>
        </tr>
        <tr v-if="!store.items.length && !store.loading">
          <td colspan="5" class="text-medium-emphasis">No channels yet.</td>
        </tr>
      </tbody>
    </v-table>
    <ChannelDrawer v-model="drawer" :channel="selected" @saved="store.list()" @removed="store.list()" />
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}
</style>
