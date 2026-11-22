<script setup lang="ts">
import type { AttemptEvent } from '../types/campaign'

defineProps<{ events: AttemptEvent[]; errorCodes: Record<string, number> }>()

const badge = (e: AttemptEvent) => {
  if (e.outcome === 'sent') return 'bg-emerald-100 text-emerald-800'
  if (e.outcome === 'retry' || e.outcome === 'timeout') return 'bg-amber-100 text-amber-800'
  if (e.outcome === 'skipped') return 'bg-slate-100 text-slate-600'
  return 'bg-rose-100 text-rose-800'
}
const label = (e: AttemptEvent) =>
  ({ sent: 'gönderildi', retry: 'tekrar', timeout: 'zaman aşımı', failed: 'başarısız', skipped: 'atlandı' })[e.outcome]
const time = (iso: string) => new Date(iso).toLocaleTimeString('tr-TR', { hour12: false }) + '.' + String(new Date(iso).getMilliseconds()).padStart(3, '0')
</script>

<template>
  <div class="space-y-2">
    <div class="flex flex-wrap items-center gap-2 text-xs">
      <span class="text-slate-500">Hata kırılımı:</span>
      <span v-if="!Object.keys(errorCodes).length" class="text-slate-400">henüz yok</span>
      <span v-for="(n, code) in errorCodes" :key="code" class="rounded bg-slate-100 px-2 py-0.5 font-mono" :data-test="`code-${code}`">
        {{ code }} × {{ n }}
      </span>
    </div>
    <table class="w-full text-left text-xs">
      <thead class="text-slate-500">
        <tr>
          <th class="py-1 font-normal">Zaman</th>
          <th class="font-normal">Kanal</th>
          <th class="font-normal">Teslimat</th>
          <th class="font-normal">Deneme</th>
          <th class="font-normal">HTTP</th>
          <th class="font-normal">Sonuç</th>
          <th class="text-right font-normal">Süre</th>
        </tr>
      </thead>
      <tbody class="font-mono">
        <tr v-if="!events.length">
          <td colspan="7" class="py-3 text-center font-sans text-slate-400">Henüz deneme yok; worker kuyruğu almaya başladığında burada görünür.</td>
        </tr>
        <tr v-for="e in events" :key="`${e.delivery_id}-${e.attempt_no}`" class="border-t border-slate-100">
          <td class="py-1">{{ time(e.at) }}</td>
          <td>{{ e.channel }}</td>
          <td>#{{ e.delivery_id }}</td>
          <td>{{ e.attempt_no }}</td>
          <td>{{ e.http_status ?? '—' }}</td>
          <td><span class="rounded px-1.5 py-0.5 font-sans" :class="badge(e)">{{ label(e) }}</span></td>
          <td class="text-right">{{ e.duration_ms }} ms</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
