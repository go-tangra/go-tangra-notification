import { defineStore } from 'pinia'
import { api } from '@/api/client'
import type { LogEntry } from '@/api/types'
import { listSpec, pagedList } from './paged'

export interface LogFilter {
  channel_id?: string | undefined
  template_id?: string | undefined
  recipient?: string | undefined
  status?: string | undefined
  /** Without from/to the server pages the last 7 days. */
  from?: string | undefined
  to?: string | undefined
}

/** Sort fields of GET /notifications (channel = the channel type). */
export const LOG_LIST = listSpec(['created_at', 'status', 'channel'], 'created_at', 'desc')

export const useLog = defineStore('notification-log', () => {
  const page = pagedList<LogEntry, LogFilter>('notifications', LOG_LIST.first)

  async function get(id: string): Promise<LogEntry> {
    return api<LogEntry>('GET', 'notifications/' + id)
  }

  async function send(input: { template_id: string; channel_id?: string; recipient: string; variables?: Record<string, string> }): Promise<LogEntry> {
    return api<LogEntry>('POST', 'notifications/send', input)
  }

  return { ...page, get, send }
})
