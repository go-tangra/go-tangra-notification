<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useAbility } from '@casl/vue'
import { useLog } from '@/stores/log'
import { useDirectory } from '@/stores/directory'
import { useOps } from '@/stores/ops'
import StatsCard from '@/components/StatsCard.vue'
import AuditTable from '@/components/AuditTable.vue'
import BackupDialog from '@/components/BackupDialog.vue'
import type { LogEntry } from '@/api/types'

const store = useLog()
const dir = useDirectory()
const ops = useOps()
const { can } = useAbility()
const status = ref('')
const recipient = ref('')
const detail = ref<LogEntry | null>(null)
const open = ref(false)
const backup = ref(false)
const tab = ref<'log' | 'audit'>('log')

// The operations section (stats, audit, backup) is shown only to holders of
// the matching abilities; the module also enforces the permission server-side.
const canStats = can('read', 'NotificationStats')
const canBackup = can('manage', 'NotificationBackup')

async function reload(): Promise<void> {
  await store.list({ status: status.value || undefined, recipient: recipient.value || undefined })
  await dir.resolveUsers(store.items.map((i) => (i.sender_kind === 'user' ? i.sender_id : undefined)))
}

onMounted(async () => {
  await reload()
  if (canStats) await Promise.all([ops.loadStats(), ops.loadAudit()])
})

async function show(e: LogEntry): Promise<void> {
  detail.value = await store.get(e.id)
  open.value = true
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <h1 class="text-h5">Notifications</h1>
      <v-spacer />
      <v-btn v-if="canBackup" variant="tonal" prepend-icon="mdi-database-arrow-up-outline" class="mr-2" data-test="ops-backup" @click="backup = true">Backup</v-btn>
    </div>
    <StatsCard v-if="canStats && ops.stats" :stats="ops.stats" class="mb-4" />
    <v-tabs v-if="canStats" v-model="tab" density="compact" class="mb-3" data-test="ops-tabs">
      <v-tab value="log">Log</v-tab>
      <v-tab value="audit">Audit</v-tab>
    </v-tabs>
    <AuditTable v-if="canStats && tab === 'audit'" :items="ops.audit" />
    <template v-if="tab === 'log'">
      <div class="d-flex align-center mb-4">
        <v-spacer />
        <v-select v-model="status" :items="['', 'pending', 'sent', 'failed']" label="Status" density="compact" hide-details style="max-width: 160px" class="mr-2" data-test="log-status" @update:model-value="reload" />
        <v-text-field v-model="recipient" label="Recipient" density="compact" hide-details style="max-width: 200px" data-test="log-recipient" @keyup.enter="reload" />
      </div>
      <v-alert v-if="store.error" type="error" variant="tonal" density="compact" class="mb-3">{{ store.error }}</v-alert>
      <v-table data-test="log-table">
        <thead>
          <tr><th>When</th><th>Recipient</th><th>Subject</th><th>Status</th><th>Sender</th></tr>
        </thead>
        <tbody>
          <tr v-for="e in store.items" :key="e.id" class="cursor-pointer" :data-test="'log-row-' + e.id" @click="show(e)">
            <td class="text-no-wrap">{{ new Date(e.created_at).toLocaleString() }}</td>
            <td>{{ e.recipient }}</td>
            <td class="text-truncate" style="max-width: 280px">{{ e.rendered_subject }}</td>
            <td>
              <v-chip size="x-small" :color="e.status === 'sent' ? 'success' : e.status === 'failed' ? 'error' : undefined" variant="tonal">{{ e.status }}</v-chip>
              <v-chip v-if="e.test" size="x-small" variant="tonal" class="ml-1">test</v-chip>
            </td>
            <td>{{ dir.userName(e.sender_id) }}</td>
          </tr>
          <tr v-if="!store.items.length && !store.loading">
            <td colspan="5" class="text-medium-emphasis">No entries.</td>
          </tr>
        </tbody>
      </v-table>
    </template>
    <BackupDialog v-if="canBackup" v-model="backup" @imported="reload" />
    <v-dialog v-model="open" max-width="640" data-test="log-detail">
      <v-card v-if="detail" :title="detail.rendered_subject">
        <v-card-text>
          <div class="text-caption text-medium-emphasis">To {{ detail.recipient }} · {{ detail.status }}</div>
          <v-alert v-if="detail.error" type="error" variant="tonal" density="compact" class="my-2">{{ detail.error }}</v-alert>
          <pre class="log-body" data-test="log-body">{{ detail.rendered_body }}</pre>
        </v-card-text>
        <v-card-actions><v-spacer /><v-btn @click="open = false">Close</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}
.log-body {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
}
</style>
