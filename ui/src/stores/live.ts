import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useInbox } from '@/stores/inbox'

// A single shared EventSource per signed-in person relays the module's live
// events through the gateway. `inbox` bumps the unread badge; `inbox.revoked`
// hides a withdrawn message; `reset` re-syncs the inbox; other event types are
// handed to registered listeners. The stream is reference-counted so several
// views share one connection.
export type Listener = (type: string, data: unknown) => void

export const useLive = defineStore('notification-live', () => {
  const connected = ref(false)
  let source: EventSource | null = null
  let refs = 0
  const listeners = new Set<Listener>()

  function handle(type: string, raw: string): void {
    let data: unknown = {}
    try {
      data = JSON.parse(raw)
    } catch {
      /* non-JSON payloads are ignored */
    }
    const inbox = useInbox()
    if (type === 'inbox') inbox.onLive()
    else if (type === 'inbox.revoked') inbox.onRevoked(String((data as { message_id?: string }).message_id ?? ''))
    else if (type === 'reset') void inbox.refreshUnread()
    for (const l of listeners) l(type, data)
  }

  function open(): void {
    if (source) return
    source = new EventSource('/api/notification/v1/stream', { withCredentials: true })
    source.onopen = () => (connected.value = true)
    source.onerror = () => (connected.value = false)
    for (const t of ['inbox', 'inbox.revoked', 'reset']) source.addEventListener(t, (e) => handle(t, (e as MessageEvent).data))
    source.addEventListener('message', (e) => handle((e as MessageEvent).type, (e as MessageEvent).data))
  }

  /** Opens the stream (first caller) and returns a release function. */
  function connect(): () => void {
    refs += 1
    open()
    return () => {
      refs -= 1
      if (refs <= 0) close()
    }
  }

  function close(): void {
    refs = 0
    source?.close()
    source = null
    connected.value = false
  }

  function on(l: Listener): () => void {
    listeners.add(l)
    return () => listeners.delete(l)
  }

  // Exposed for tests: inject a fake event.
  function _emit(type: string, raw: string): void {
    handle(type, raw)
  }

  return { connected, connect, close, on, _emit }
})
