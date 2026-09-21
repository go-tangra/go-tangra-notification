<script setup lang="ts">
import { ref } from 'vue'
import { useOps } from '@/stores/ops'
import { describe } from '@/api/client'
import type { BackupReport } from '@/api/types'

const model = defineModel<boolean>({ required: true })
const emit = defineEmits<{ imported: [] }>()
const ops = useOps()

const includeCredentials = ref(false)
const mode = ref<'skip' | 'overwrite'>('skip')
const file = ref<File[]>([])
const report = ref<BackupReport | null>(null)
const busy = ref(false)
const err = ref('')

async function doExport(): Promise<void> {
  busy.value = true
  err.value = ''
  try {
    await ops.exportBackup(includeCredentials.value)
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}

async function doImport(): Promise<void> {
  const f = file.value[0]
  if (!f) return
  busy.value = true
  err.value = ''
  report.value = null
  try {
    report.value = await ops.importBackup(f, mode.value)
    emit('imported')
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <v-dialog v-model="model" max-width="560" data-test="backup-dialog">
    <v-card title="Backup">
      <v-card-text>
        <div class="text-subtitle-2 mb-2">Export</div>
        <v-checkbox
          v-model="includeCredentials"
          density="compact"
          label="Include channel credentials (bulk disclosure — audited)"
          data-test="backup-include-credentials"
        />
        <v-btn color="primary" variant="tonal" :loading="busy" prepend-icon="mdi-download" data-test="backup-export" @click="doExport">Export</v-btn>
        <v-divider class="my-4" />
        <div class="text-subtitle-2 mb-2">Import</div>
        <v-file-input v-model="file" accept="application/json" label="Backup file" density="compact" data-test="backup-file" />
        <v-radio-group v-model="mode" inline density="compact" data-test="backup-mode">
          <v-radio label="Skip duplicates" value="skip" />
          <v-radio label="Overwrite duplicates" value="overwrite" />
        </v-radio-group>
        <v-btn color="primary" :loading="busy" :disabled="!file.length" prepend-icon="mdi-upload" data-test="backup-import" @click="doImport">Import</v-btn>
        <v-alert v-if="err" type="error" variant="tonal" density="compact" class="mt-3" data-test="backup-error">{{ err }}</v-alert>
        <v-table v-if="report" density="compact" class="mt-3" data-test="backup-report">
          <thead>
            <tr><th>Kind</th><th>Created</th><th>Skipped</th><th>Overwritten</th><th>Failed</th></tr>
          </thead>
          <tbody>
            <tr v-for="k in (['channels', 'templates', 'categories'] as const)" :key="k">
              <td>{{ k }}</td>
              <td>{{ report[k].created }}</td>
              <td>{{ report[k].skipped }}</td>
              <td>{{ report[k].overwritten }}</td>
              <td>{{ report[k].failed }}</td>
            </tr>
          </tbody>
        </v-table>
        <ul v-if="report && report.warnings.length" class="text-caption mt-2" data-test="backup-warnings">
          <li v-for="(w, i) in report.warnings" :key="i">{{ w }}</li>
        </ul>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="model = false">Close</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
