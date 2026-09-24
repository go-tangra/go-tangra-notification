import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useLive } from '@/stores/live'
import { useInbox } from '@/stores/inbox'

// A minimal EventSource double capturing listeners.
class FakeES {
  static last: FakeES | null = null
  url: string
  listeners: Record<string, (e: MessageEvent) => void> = {}
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(url: string) {
    this.url = url
    FakeES.last = this
  }
  addEventListener(t: string, fn: (e: MessageEvent) => void): void {
    this.listeners[t] = fn
  }
  close(): void {
    this.closed = true
  }
}

describe('live store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('EventSource', FakeES as unknown as typeof EventSource)
  })

  it('opens one shared stream and routes inbox/revoked/reset to the inbox', () => {
    const inbox = useInbox()
    inbox.items = [{ id: 'e1', message: { id: 'm1', title: 'A', content: 'b', type: 'notification' }, status: 'sent', created_at: 't' }]
    inbox.unread = 1
    const live = useLive()
    const release1 = live.connect()
    const release2 = live.connect() // shares the connection
    expect(FakeES.last?.url).toContain('/api/notification/v1/stream')
    FakeES.last?.onopen?.()
    expect(live.connected).toBe(true)
    live._emit('inbox', '{"message_id":"m2"}')
    expect(inbox.unread).toBe(2)
    live._emit('inbox.revoked', '{"message_id":"m1"}')
    expect(inbox.items.length).toBe(0)
    const seen: string[] = []
    const off = live.on((type) => seen.push(type))
    live._emit('warden.secret', '{}')
    expect(seen).toContain('warden.secret')
    off()
    release1()
    expect(FakeES.last?.closed).toBe(false)
    release2()
    expect(FakeES.last?.closed).toBe(true)
  })
})
