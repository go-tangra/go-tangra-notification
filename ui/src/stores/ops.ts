import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, ApiError, BASE } from '@/api/client'
import { downloadJSON, readFile } from '@/api/download'
import type { AuditFilter, AuditItem, BackupReport, Stats } from '@/api/types'
import { listSpec, pagedList } from './paged'

/** GET /audit pages newest first (the last 7 days without from/to). */
export const AUDIT_LIST = listSpec(['ts'], 'ts', 'desc', 50)

export const useOps = defineStore('notification-ops', () => {
  const stats = ref<Stats | null>(null)
  const auditPage = pagedList<AuditItem, AuditFilter>('audit', AUDIT_LIST.first)
  const error = ref('')

  async function loadStats(): Promise<void> {
    try {
      stats.value = await api<Stats>('GET', 'stats')
    } catch (e) {
      error.value = (e as Error).message
    }
  }

  /** Loads one page of the audit trail (server-paged and sorted). */
  const loadAudit = auditPage.list

  async function exportBackup(includeCredentials: boolean): Promise<number> {
    if (includeCredentials) {
      const doc = await api<unknown>('POST', 'backup/export', { include_credentials: true })
      const blob = JSON.stringify(doc, null, 2)
      const url = URL.createObjectURL(new Blob([blob], { type: 'application/json' }))
      const a = document.createElement('a')
      a.href = url
      a.download = 'notification-backup.json'
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      return blob.length
    }
    return downloadJSON(BASE + '/backup/export', 'notification-backup.json')
  }

  async function importBackup(file: File, mode: 'skip' | 'overwrite'): Promise<BackupReport> {
    const text = await readFile(file)
    let doc: unknown
    try {
      doc = JSON.parse(text)
    } catch {
      throw new ApiError(422, 'validation_failed')
    }
    return api<BackupReport>('POST', 'backup/import', doc, { query: { mode } })
  }

  return {
    stats,
    audit: auditPage.items,
    auditTotal: auditPage.total,
    loading: auditPage.loading,
    auditError: auditPage.error,
    error,
    loadStats,
    loadAudit,
    exportBackup,
    importBackup,
  }
})
