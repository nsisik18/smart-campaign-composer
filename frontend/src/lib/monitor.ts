import type { ChannelStats, MonitorStats } from '../types/campaign'

export const isTerminal = (s: { status: string } | null) => s?.status === 'completed' || s?.status === 'completed_with_errors'

// done: terminal durumdaki teslimatlar (sent + failed + skipped).
export const done = (c: ChannelStats) => c.sent + c.failed + c.skipped

export const progressPct = (c: ChannelStats) => (c.total === 0 ? 100 : Math.floor((done(c) / c.total) * 100))

export function totals(s: MonitorStats): ChannelStats {
  const z: ChannelStats = { total: 0, queued: 0, in_flight: 0, retry_wait: 0, sent: 0, failed: 0, skipped: 0, sent_per_sec: 0, requests_per_sec: 0 }
  for (const c of Object.values(s.channels)) {
    for (const k of Object.keys(z) as (keyof ChannelStats)[]) z[k] += c[k]
  }
  return z
}
