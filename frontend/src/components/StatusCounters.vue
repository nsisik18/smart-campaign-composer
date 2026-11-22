<script setup lang="ts">
import type { ChannelStats } from '../types/campaign'

defineProps<{ c: ChannelStats }>()
const items: { key: keyof ChannelStats; label: string; cls: string }[] = [
  { key: 'sent', label: 'Gönderildi', cls: 'text-emerald-700' },
  { key: 'queued', label: 'Kuyrukta', cls: 'text-slate-700' },
  { key: 'in_flight', label: 'Gönderiliyor', cls: 'text-indigo-700' },
  { key: 'retry_wait', label: 'Tekrar bekliyor', cls: 'text-amber-700' },
  { key: 'failed', label: 'Başarısız', cls: 'text-rose-700' },
  { key: 'skipped', label: 'Atlandı (izin yok)', cls: 'text-slate-500' },
]
const fmt = (n: number) => n.toLocaleString('tr-TR')
</script>

<template>
  <dl class="grid grid-cols-3 gap-2 sm:grid-cols-6">
    <div v-for="i in items" :key="i.key" class="rounded-md bg-slate-50 p-2">
      <dt class="text-[11px] text-slate-500">{{ i.label }}</dt>
      <dd class="text-lg font-semibold" :class="i.cls" :data-test="`count-${i.key}`">{{ fmt(c[i.key] as number) }}</dd>
    </div>
  </dl>
</template>
