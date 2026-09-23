<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAbility } from '@casl/vue'
import { UiPage, UiAlert, UiCard, UiButton, UiDataTable, UiStatusChip, UiBadge, UiTabs, UiForm, UiInput, UiSelect, UiCheckbox, UiFilePicker, UiStatGrid, UiStatTile, useToast, UiDrawer, type Column, type SelectOption, type TabItem } from '@freya/ui'
import { useZodForm } from '@freya/ui/forms'
import { useLog } from '@/stores/log'
import { useDirectory } from '@/stores/directory'
import { useOps } from '@/stores/ops'
import { describe } from '@/api/client'
import { backupImportSchema, logFilterSchema, BACKUP_MODES } from '@/schemas'
import type { AuditItem, BackupReport, LogEntry } from '@/api/types'

const store = useLog()
const dir = useDirectory()
const ops = useOps()
const toast = useToast()
const { can } = useAbility()
const detail = ref<LogEntry | null>(null)
const open = ref(false)
const backup = ref(false)
const tab = ref('log')
// The operations section (stats, audit, backup) is shown only to holders of
// the matching abilities; the module also enforces the permission server-side.
const canStats = can('read', 'NotificationStats')
const canBackup = can('manage', 'NotificationBackup')

const statusOptions: SelectOption[] = ['pending', 'sent', 'failed'].map((s) => ({ title: s, value: s }))
const filter = useZodForm(logFilterSchema, {
  initial: { recipient: '' },
  onSubmit: async (f) => {
    await store.list({ status: f.status, recipient: f.recipient || undefined })
    await dir.resolveUsers(store.items.map((i) => (i.sender_kind === 'user' ? i.sender_id : undefined)))
  },
})
const reload = () => void filter.submit()
onMounted(async () => {
  reload()
  if (canStats) {
    await Promise.all([ops.loadStats(), ops.loadAudit()])
    await dir.resolveUsers(ops.audit.map((i) => i.actor_id))
  }
})
const tabs: TabItem[] = [{ key: 'log', label: 'Log' }, { key: 'audit', label: 'Audit' }]
const sum = (m: Record<string, number>): number => Object.values(m).reduce((a, b) => a + b, 0)
async function show(e: LogEntry): Promise<void> {
  detail.value = await store.get(e.id)
  open.value = true
}
const logColumns: Column<LogEntry>[] = [
  { key: 'created_at', label: 'When', format: (e) => new Date(e.created_at).toLocaleString(), sortable: true },
  { key: 'recipient', label: 'Recipient' },
  { key: 'rendered_subject', label: 'Subject', hideOnStack: true },
  { key: 'status', label: 'Status', width: 'sm' },
  { key: 'sender_id', label: 'Sender', format: (e) => dir.userName(e.sender_id), hideOnStack: true },
]
const auditRows = computed(() => ops.audit.map((i: AuditItem, n: number) => ({ ...i, id: i.ts + ':' + n })))
const auditColumns: Column<(typeof auditRows.value)[number]>[] = [
  { key: 'ts', label: 'When', format: (i) => new Date(i.ts).toLocaleString() },
  { key: 'event_type', label: 'Event' },
  { key: 'actor', label: 'Actor', format: (i) => (i.actor_kind === 'system' ? 'system' : dir.userName(i.actor_id)) },
  { key: 'subject', label: 'Subject', format: (i) => i.subject_name || i.subject_id || i.subject_kind || '', hideOnStack: true },
  { key: 'outcome', label: 'Outcome', width: 'sm' },
]

// --- backup: export (optionally with credentials, audited) and import with a conflict mode ---
const includeCredentials = ref(false)
const report = ref<BackupReport | null>(null)
const exporting = ref(false)
const backupError = ref('')
async function doExport(): Promise<void> {
  exporting.value = true
  backupError.value = ''
  try {
    await ops.exportBackup(includeCredentials.value)
    toast.success('Backup exported')
  } catch (e) {
    backupError.value = describe(e)
  } finally {
    exporting.value = false
  }
}
const importForm = useZodForm(backupImportSchema, {
  initial: { mode: 'skip' },
  onSubmit: async (v) => {
    report.value = await ops.importBackup(v.file, v.mode)
  },
  onSuccess: () => {
    importForm.reset({ mode: 'skip' })
    reload()
  },
})
const reportRows = computed(() => (report.value ? (['channels', 'templates', 'categories'] as const).map((k) => ({ id: k, kind: k, ...report.value![k] })) : []))
const reportColumns: Column<(typeof reportRows.value)[number]>[] = [{ key: 'kind', label: 'Kind' }, { key: 'created', label: 'Created', align: 'end' }, { key: 'skipped', label: 'Skipped', align: 'end' }, { key: 'overwritten', label: 'Overwritten', align: 'end' }, { key: 'failed', label: 'Failed', align: 'end' }]
</script>

<template>
  <UiPage title="Notifications">
    <template #actions><UiButton v-if="canBackup" variant="soft" icon="mdi-database-arrow-up-outline" data-test="ops-backup" @click="backup = true">Backup</UiButton></template>
    <UiStatGrid v-if="canStats && ops.stats" class="mb-4" :cols="6" data-test="stats-card">
      <UiStatTile title="Channels" :value="ops.stats.channels" icon="mdi-radio-tower" color="primary" data-test="stat-channels" />
      <UiStatTile title="Templates" :value="ops.stats.templates" icon="mdi-file-document-edit-outline" color="info" data-test="stat-templates" />
      <UiStatTile title="Sent (24h)" :value="sum(ops.stats.notifications)" icon="mdi-send-outline" color="success" data-test="stat-notifications" />
      <UiStatTile title="Messages" :value="sum(ops.stats.messages)" icon="mdi-message-text-outline" data-test="stat-messages" />
      <UiStatTile title="Open streams" :value="ops.stats.open_streams" icon="mdi-broadcast" data-test="stat-streams" />
      <UiStatTile title="Operations (24h)" :value="ops.stats.operations_24h" icon="mdi-pulse" data-test="stat-ops" />
    </UiStatGrid>
    <UiTabs v-if="canStats" v-model="tab" :tabs="tabs" class="mb-3" data-test="ops-tabs" />
    <UiCard v-if="canStats && tab === 'audit'" :padded="false">
      <UiDataTable :items="auditRows" :columns="auditColumns" :loading="ops.loading" caption="Audit events" empty-title="No events" data-test="audit-table">
        <template #cell-outcome="{ row }"><UiStatusChip :status="row.outcome" :colors="{ ok: 'success', refused: 'warning', denied: 'error' }" /></template>
      </UiDataTable>
    </UiCard>
    <template v-if="tab === 'log'">
      <UiForm :form="filter" class="mb-3">
        <div class="grid grid-cols-2 gap-2 md:max-w-md">
          <UiSelect v-bind="filter.field('status')" label="Status" :options="statusOptions" size="sm" data-test="log-status" @update:model-value="reload" />
          <UiInput v-bind="filter.field('recipient')" label="Recipient" size="sm" data-test="log-recipient" @enter="reload" />
        </div>
      </UiForm>
      <UiAlert v-if="store.error" kind="error" class="mb-3">{{ store.error }}</UiAlert>
      <UiCard :padded="false">
        <UiDataTable :items="store.items" :columns="logColumns" :loading="store.loading" caption="Delivery log" empty-title="No entries" clickable :row-attrs="(e) => ({ 'data-test': 'log-row-' + e.id })" data-test="log-table" @row-click="show">
          <template #cell-status="{ row }"><UiStatusChip :status="row.status" :colors="{ pending: 'neutral' }" /> <UiBadge v-if="row.test" size="xs">test</UiBadge></template>
        </UiDataTable>
      </UiCard>
    </template>
    <UiDrawer v-model="open" :title="detail?.rendered_subject ?? ''" size="lg" data-test="log-detail">
      <template v-if="detail">
        <p class="text-xs text-base-content/70">To {{ detail.recipient }} · {{ detail.status }}</p>
        <UiAlert v-if="detail.error" kind="error" class="my-2">{{ detail.error }}</UiAlert>
        <pre class="whitespace-pre-wrap break-words font-sans" data-test="log-body">{{ detail.rendered_body }}</pre>
      </template>
      <template #actions><UiButton @click="open = false">Close</UiButton></template>
    </UiDrawer>
    <UiDrawer v-if="canBackup" v-model="backup" title="Backup" size="md" data-test="backup-dialog">
      <h3 class="mb-2 text-sm font-medium">Export</h3>
      <UiCheckbox id="backup-include-credentials" v-model="includeCredentials" label="Include channel credentials (bulk disclosure — audited)" data-test="backup-include-credentials" />
      <UiButton class="mt-2" variant="soft" icon="mdi-download" :loading="exporting" data-test="backup-export" @click="doExport">Export</UiButton>
      <div class="divider my-4" />
      <h3 class="mb-2 text-sm font-medium">Import</h3>
      <UiForm :form="importForm">
        <div class="flex flex-col gap-3">
          <UiFilePicker v-bind="importForm.field('file')" label="Backup file" accept="application/json" required data-test="backup-file" />
          <UiSelect v-bind="importForm.field('mode')" label="On duplicates" :options="BACKUP_MODES.map((m) => ({ title: m === 'skip' ? 'Skip duplicates' : 'Overwrite duplicates', value: m }))" :clearable="false" data-test="backup-mode" />
          <div><UiButton type="submit" icon="mdi-upload" :loading="importForm.submitting.value" data-test="backup-import">Import</UiButton></div>
        </div>
      </UiForm>
      <UiAlert v-if="backupError" kind="error" class="mt-3" data-test="backup-error">{{ backupError }}</UiAlert>
      <UiDataTable v-if="report" class="mt-3" :items="reportRows" :columns="reportColumns" caption="Import report" data-test="backup-report" />
      <ul v-if="report && report.warnings.length" class="mt-2 list-disc ps-5 text-xs" data-test="backup-warnings"><li v-for="(w, i) in report.warnings" :key="i">{{ w }}</li></ul>
      <template #actions><UiButton @click="backup = false">Close</UiButton></template>
    </UiDrawer>
  </UiPage>
</template>
