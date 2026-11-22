import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import MonitorStep from './MonitorStep.vue'
import type { MonitorStats } from '../types/campaign'

const ch = (o: Partial<Record<string, number>> = {}) => ({
  total: 100, queued: 40, in_flight: 10, retry_wait: 5, sent: 40, failed: 3, skipped: 2, sent_per_sec: 97, requests_per_sec: 98, ...o,
})

const stats = (o: Partial<MonitorStats> = {}): MonitorStats => ({
  campaign_id: 1,
  status: 'sending',
  channels: { sms: ch(), email: ch({ total: 200, sent: 100, queued: 95, in_flight: 0, retry_wait: 0, failed: 5, skipped: 0 }) },
  error_codes: { '429': 12, '503': 4, timeout: 2, '422': 3 },
  recent: [
    { delivery_id: 9, channel: 'sms', attempt_no: 1, http_status: 429, outcome: 'retry', duration_ms: 31, at: '2026-11-22T12:00:00.120Z' },
    { delivery_id: 8, channel: 'email', attempt_no: 2, http_status: null, outcome: 'timeout', duration_ms: 5000, at: '2026-11-22T12:00:00.100Z' },
  ],
  approved_at: '2026-11-22T11:59:50Z',
  completed_at: null,
  at: '2026-11-22T12:00:00Z',
  ...o,
})

describe('MonitorStep', () => {
  it('shows per-channel progress, totals, rates and visible 429/503', () => {
    const w = mount(MonitorStep, { props: { stats: stats(), connection: 'live' } })
    // SMS: terminal = 40+3+2 = 45 / 100
    expect(w.find('[data-test="progress-SMS"]').text()).toContain('45 / 100 · %45')
    expect(w.find('[data-test="progress-Email"]').text()).toContain('105 / 200 · %52')
    expect(w.find('[data-test="count-sent"]').text()).toBe('140')
    expect(w.find('[data-test="code-429"]').text()).toContain('429 × 12')
    expect(w.text()).toContain('İstek 98/s')
    expect(w.text()).toContain('zaman aşımı')
    expect(w.text()).toContain('tekrar denenmeyi bekliyor')
    expect(w.find('[data-test="connection"]').text()).toBe('Canlı')
    expect(w.text()).toContain('Geçen süre: 10 s')
  })

  it('shows reconnecting and terminal states', async () => {
    const w = mount(MonitorStep, { props: { stats: stats(), connection: 'reconnecting' } })
    expect(w.find('[data-test="connection"]').text()).toContain('yeniden bağlanıyor')
    await w.setProps({ stats: stats({ status: 'completed_with_errors', completed_at: '2026-11-22T12:00:20Z' }), connection: 'closed' })
    expect(w.find('[data-test="connection"]').text()).toContain('hatalı teslimatlar var')
    expect(w.text()).toContain('Toplam süre: 30 s')
  })

  it('has loading and empty states', () => {
    expect(mount(MonitorStep, { props: { stats: null, connection: 'connecting' } }).text()).toContain('yükleniyor')
    const w = mount(MonitorStep, { props: { stats: stats({ recent: [], error_codes: {} }), connection: 'live' } })
    expect(w.text()).toContain('Henüz deneme yok')
    expect(w.text()).toContain('henüz yok')
  })
})
