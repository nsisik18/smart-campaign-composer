<script setup lang="ts">
import type { Reach } from '../types/campaign'

defineProps<{ reach: Reach }>()

const fmt = (n: number) => n.toLocaleString('tr-TR')
</script>

<template>
  <dl class="grid grid-cols-2 gap-3 sm:grid-cols-4">
    <div class="rounded-md bg-slate-50 p-3">
      <dt class="text-xs text-slate-500">Eşleşen</dt>
      <dd class="text-2xl font-semibold" data-test="matched">{{ fmt(reach.matched) }}</dd>
    </div>
    <div class="rounded-md bg-slate-50 p-3">
      <dt class="text-xs text-slate-500">SMS'e uygun</dt>
      <dd class="text-2xl font-semibold">{{ fmt(reach.sms_eligible) }}</dd>
    </div>
    <div class="rounded-md bg-slate-50 p-3">
      <dt class="text-xs text-slate-500">Email'e uygun</dt>
      <dd class="text-2xl font-semibold">{{ fmt(reach.email_eligible) }}</dd>
    </div>
    <div class="rounded-md bg-emerald-50 p-3">
      <dt class="text-xs text-emerald-700">En az bir kanaldan ulaşılabilir</dt>
      <dd class="text-2xl font-semibold text-emerald-900">{{ fmt(reach.reachable) }}</dd>
    </div>
  </dl>
  <p class="mt-2 text-xs text-slate-500">
    Kanal izni olmayan kişilere o kanaldan mesaj gönderilmez; sayılar izin filtresi uygulanmış haldedir.
  </p>
</template>
