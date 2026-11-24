import type { EventStream } from '@app/lib/api'

/**
 * In-memory stand-in for an EventSource: same surface as the store uses (readyState, onopen, onerror,
 * addEventListener, close). It "connects" on the next tick, then receives what the demo server emits on its path.
 */
export class DemoEventStream implements EventStream {
  readyState: number = 0 // EventSource.CONNECTING
  onopen: ((ev: Event) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  private listeners = new Map<string, Set<(ev: MessageEvent) => void>>()

  constructor(
    readonly path: string,
    readonly params: Record<string, string | string[]>,
    private onClose: (s: DemoEventStream) => void,
    onOpen: (s: DemoEventStream) => void,
  ) {
    setTimeout(() => {
      if (this.readyState !== 0) return
      this.readyState = 1 // EventSource.OPEN
      this.onopen?.(new Event('open'))
      onOpen(this)
    }, 30)
  }

  addEventListener(type: string, listener: (ev: MessageEvent) => void) {
    let set = this.listeners.get(type)
    if (!set) this.listeners.set(type, (set = new Set()))
    set.add(listener)
  }

  /** Delivers one SSE message, its data serialized as JSON like the daemon's. */
  emit(type: string, data: unknown) {
    if (this.readyState !== 1) return
    const ev = new MessageEvent(type, { data: JSON.stringify(data) })
    this.listeners.get(type)?.forEach((fn) => fn(ev))
  }

  close() {
    if (this.readyState === 2) return
    this.readyState = 2 // EventSource.CLOSED
    this.onClose(this)
  }
}
