<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useChannels } from '@/stores/channels'
import { useTemplates } from '@/stores/templates'
import PermissionDrawer from '@/components/PermissionDrawer.vue'
import type { ResourceType } from '@/api/types'

const channels = useChannels()
const templates = useTemplates()
const drawer = ref(false)
const target = ref<{ type: ResourceType; id: string; name: string } | null>(null)

onMounted(async () => {
  await Promise.all([channels.list(), templates.list()])
})

function open(type: ResourceType, id: string, name: string): void {
  target.value = { type, id, name }
  drawer.value = true
}
</script>

<template>
  <div>
    <h1 class="text-h5 mb-4">Permissions</h1>
    <v-row>
      <v-col cols="12" md="6">
        <v-card title="Channels" data-test="perm-channels">
          <v-list density="compact">
            <v-list-item v-for="c in channels.items" :key="c.id" :title="c.name" :subtitle="c.type" :data-test="'perm-channel-' + c.id" @click="open('channel', c.id, c.name)">
              <template #append><v-icon icon="mdi-shield-account-outline" size="small" /></template>
            </v-list-item>
          </v-list>
        </v-card>
      </v-col>
      <v-col cols="12" md="6">
        <v-card title="Templates" data-test="perm-templates">
          <v-list density="compact">
            <v-list-item v-for="t in templates.items" :key="t.id" :title="t.name" :subtitle="t.channel_name ?? ''" :data-test="'perm-template-' + t.id" @click="open('template', t.id, t.name)">
              <template #append><v-icon icon="mdi-shield-account-outline" size="small" /></template>
            </v-list-item>
          </v-list>
        </v-card>
      </v-col>
    </v-row>
    <PermissionDrawer v-if="target" v-model="drawer" :resource-type="target.type" :resource-id="target.id" :resource-name="target.name" />
  </div>
</template>
