<script setup lang="ts">
import { computed } from 'vue'
import ChannelProgress from '../components/ChannelProgress.vue'
import StatusCounters from '../components/StatusCounters.vue'
import RecentAttempts from '../components/RecentAttempts.vue'
import { isTerminal, totals } from '../lib/monitor'
import type { Connection } from '../composables/useCampaignEvents'
import type { MonitorStats } from '../types/campaign'

const props = defineProps<{ stats: MonitorStats | null; connection: Connection }>()

const all = computed(() => (props.stats ? totals(props.stats) : null))
const terminal = computed(() => isTerminal(props.stats))
const elapsed = computed(() => {
  const s = props.stats
  if (!s?.approved_at) return null
  const end = s.completed_at ? new Date(s.completed_at) : new Date(s.at)
  return Math.max(0, Math.round((end.getTime() - new Date(s.approved_at).getTime()) / 1000))
})
</script>

<template>
  <section class="space-y-5 rounded-lg border border-slate-200 bg-white p-6" data-test="monitor">
    <div class="flex items-center justify-between">
      <div>
        <h2 class="font-medium">Canlı gönderim</h2>
        <p v-if="elapsed !== null" class="text-xs text-slate-500">{{ terminal ? 'Toplam süre' : 'Geçen süre' }}: {{ elapsed }} s</p>
      </div>
      <span
        class="rounded px-2 py-0.5 text-xs"
        data-test="connection"
        :class="{
          'bg-emerald-100 text-emerald-800': stats?.status === 'completed',
          'bg-rose-100 text-rose-800': stats?.status === 'completed_with_errors',
          'bg-indigo-100 text-indigo-800': !terminal && connection === 'live',
          'bg-amber-100 text-amber-800': connection === 'reconnecting',
          'bg-slate-100 text-slate-600': connection === 'connecting',
        }"
      >
        <template v-if="stats?.status === 'completed'">Tamamlandı</template>
        <template v-else-if="stats?.status === 'completed_with_errors'">Tamamlandı (hatalı teslimatlar var)</template>
        <template v-else-if="connection === 'reconnecting'">Bağlantı koptu, yeniden bağlanıyor…</template>
        <template v-else-if="connection === 'live'">Canlı</template>
        <template v-else>Bağlanıyor…</template>
      </span>
    </div>

    <p v-if="!stats" class="text-sm text-slate-500">Gönderim durumu yükleniyor…</p>
    <template v-else>
      <div class="grid gap-5 md:grid-cols-2">
        <ChannelProgress v-if="stats.channels.sms" label="SMS" :c="stats.channels.sms" :limit="100" />
        <ChannelProgress v-if="stats.channels.email" label="Email" :c="stats.channels.email" :limit="100" />
      </div>
      <StatusCounters v-if="all" :c="all" />
      <p v-if="all && all.retry_wait > 0" class="text-xs text-amber-700">
        {{ all.retry_wait }} teslimat geçici hata (429/503/zaman aşımı) sonrası tekrar denenmeyi bekliyor; aynı Idempotency-Key ile gönderilecek.
      </p>
      <RecentAttempts :events="stats.recent" :error-codes="stats.error_codes" />
    </template>
  </section>
</template>
