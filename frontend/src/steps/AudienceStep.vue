<script setup lang="ts">
import RuleChips from '../components/RuleChips.vue'
import ReachStats from '../components/ReachStats.vue'
import type { Reach, SegmentPlan } from '../types/campaign'

defineProps<{ segment: SegmentPlan; reach: Reach; repaired: boolean; busy: boolean; canReanalyze: boolean }>()
const emit = defineEmits<{ reanalyze: [] }>()
</script>

<template>
  <section class="space-y-4 rounded-lg border border-slate-200 bg-white p-6">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h2 class="font-medium">Hedef kitle</h2>
        <p class="text-sm text-slate-600">{{ segment.summary }}</p>
      </div>
      <button
        v-if="canReanalyze"
        type="button"
        class="shrink-0 rounded-md border border-slate-300 px-3 py-1.5 text-sm disabled:opacity-50"
        :disabled="busy"
        @click="emit('reanalyze')"
      >
        {{ busy ? 'Analiz ediliyor…' : 'Yeniden analiz et' }}
      </button>
    </div>

    <RuleChips :rules="segment.rules" />

    <div
      v-if="segment.unsupported_criteria.length"
      data-test="unsupported"
      class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900"
    >
      <p class="font-medium">Bu kriterler mevcut verilerle ifade edilemedi ve segmente dahil edilmedi:</p>
      <ul class="mt-1 list-disc pl-5">
        <li v-for="c in segment.unsupported_criteria" :key="c">{{ c }}</li>
      </ul>
    </div>

    <p v-if="repaired" class="text-xs text-slate-500">
      Claude'un ilk çıktısı doğrulamadan geçmedi; tek bir otomatik düzeltme ile geçerli segment elde edildi.
    </p>

    <div v-if="reach.matched === 0" class="rounded-md bg-slate-50 p-4 text-sm text-slate-600">
      Bu kurallara uyan müşteri yok. Brief'i genişletip yeni kampanya başlatabilirsiniz.
    </div>
    <ReachStats v-else :reach="reach" />

    <details class="text-sm">
      <summary class="cursor-pointer text-slate-500">Ham segment JSON</summary>
      <pre class="mt-2 overflow-x-auto rounded-md bg-slate-900 p-3 text-xs text-slate-100">{{ JSON.stringify(segment, null, 2) }}</pre>
    </details>
  </section>
</template>
