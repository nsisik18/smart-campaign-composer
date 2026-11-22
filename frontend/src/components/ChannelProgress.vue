<script setup lang="ts">
import { computed } from 'vue'
import { done, progressPct } from '../lib/monitor'
import type { ChannelStats } from '../types/campaign'

const props = defineProps<{ label: string; c: ChannelStats; limit: number }>()
const pct = computed(() => progressPct(props.c))
const fmt = (n: number) => n.toLocaleString('tr-TR')
</script>

<template>
  <div class="space-y-1">
    <div class="flex items-baseline justify-between text-sm">
      <span class="font-medium">{{ label }}</span>
      <span class="font-mono text-xs text-slate-600" :data-test="`progress-${label}`">
        {{ fmt(done(c)) }} / {{ fmt(c.total) }} · %{{ pct }}
      </span>
    </div>
    <div class="flex h-3 overflow-hidden rounded-full bg-slate-100" role="progressbar" :aria-valuenow="pct" aria-valuemin="0" aria-valuemax="100">
      <div class="bg-emerald-500 transition-all" :style="{ width: `${(c.sent / Math.max(c.total, 1)) * 100}%` }" />
      <div class="bg-slate-400 transition-all" :style="{ width: `${(c.skipped / Math.max(c.total, 1)) * 100}%` }" />
      <div class="bg-rose-500 transition-all" :style="{ width: `${(c.failed / Math.max(c.total, 1)) * 100}%` }" />
    </div>
    <p class="text-xs text-slate-500">
      Gönderim {{ c.sent_per_sec.toFixed(0) }}/s · İstek {{ c.requests_per_sec.toFixed(0) }}/s
      <span class="text-slate-400">(sınır {{ limit }}/s)</span>
    </p>
  </div>
</template>
