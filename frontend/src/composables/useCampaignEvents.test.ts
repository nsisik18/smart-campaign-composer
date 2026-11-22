import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useCampaignEvents } from './useCampaignEvents'

// Sahte EventSource: olayları testten tetikleriz.
class FakeES {
  static last: FakeES | null = null
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  onerror: (() => void) | null = null
  closed = false
  constructor(public url: string) {
    FakeES.last = this
  }
  addEventListener(name: string, fn: (e: MessageEvent) => void) {
    ;(this.listeners[name] ??= []).push(fn)
  }
  emit(name: string, data: unknown) {
    this.listeners[name]?.forEach((fn) => fn(new MessageEvent(name, { data: JSON.stringify(data) })))
  }
  close() {
    this.closed = true
  }
}

afterEach(() => vi.unstubAllGlobals())

function setup(id: number | null) {
  vi.stubGlobal('EventSource', FakeES)
  const onTerminal = vi.fn()
  const cid = ref<number | null>(id)
  let api!: ReturnType<typeof useCampaignEvents>
  mount(defineComponent({ setup: () => ((api = useCampaignEvents(cid, onTerminal)), () => null) }))
  return { api, onTerminal, cid }
}

describe('useCampaignEvents', () => {
  it('goes live on snapshot, survives errors, closes itself on terminal status', () => {
    const { api, onTerminal } = setup(7)
    const es = FakeES.last!
    expect(es.url).toBe('/api/campaigns/7/events')
    expect(api.connection.value).toBe('connecting')

    es.emit('snapshot', { status: 'sending', channels: {} })
    expect(api.connection.value).toBe('live')

    es.onerror?.()
    expect(api.connection.value).toBe('reconnecting')
    expect(es.closed).toBe(false) // tarayıcı yeniden bağlanır; biz kapatmayız

    es.emit('stats', { status: 'completed', channels: {} })
    expect(api.connection.value).toBe('closed')
    expect(es.closed).toBe(true)
    expect(onTerminal).toHaveBeenCalledOnce()
  })

  it('does not connect without a campaign', () => {
    FakeES.last = null
    const { api } = setup(null)
    expect(FakeES.last).toBeNull()
    expect(api.connection.value).toBe('idle')
  })
})
