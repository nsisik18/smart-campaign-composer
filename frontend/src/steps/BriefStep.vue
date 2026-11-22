<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { runeLength } from '../lib/rules'

const props = defineProps<{ initial: string; busy: boolean; locked: boolean }>()
const emit = defineEmits<{ analyze: [brief: string] }>()

const MAX = 2000
const EXAMPLE =
  "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."

const brief = ref(props.initial)
watch(
  () => props.initial,
  (v) => (brief.value = v),
)

const length = computed(() => runeLength(brief.value.trim()))
const canSubmit = computed(() => !props.busy && !props.locked && length.value > 0 && length.value <= MAX)
</script>

<template>
  <section class="space-y-3 rounded-lg border border-slate-200 bg-white p-6">
    <div class="flex items-center justify-between">
      <label for="brief" class="font-medium">Kampanya brief'i</label>
      <button
        type="button"
        class="text-sm text-indigo-600 hover:underline disabled:opacity-50"
        :disabled="busy || locked"
        @click="brief = EXAMPLE"
      >
        Örnek brief
      </button>
    </div>
    <textarea
      id="brief"
      v-model="brief"
      rows="4"
      :readonly="locked"
      class="w-full rounded-md border border-slate-300 p-3 text-sm focus:border-indigo-500 focus:outline-none read-only:bg-slate-50"
      placeholder="Kime, ne göndermek istiyorsunuz? Örn. son 30 günde sepete ekleyip almayan İstanbullulara %30 indirim…"
    />
    <div class="flex items-center justify-between">
      <span class="text-xs" :class="length > MAX ? 'text-rose-600' : 'text-slate-500'">{{ length }} / {{ MAX }}</span>
      <button
        type="button"
        data-test="analyze"
        class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:bg-slate-300"
        :disabled="!canSubmit"
        @click="emit('analyze', brief.trim())"
      >
        {{ busy ? 'Claude kitleyi çıkarıyor…' : 'Analiz et' }}
      </button>
    </div>
    <p v-if="locked" class="text-xs text-slate-500">Brief kaydedildi. Farklı bir brief için yeni kampanya başlatın.</p>
  </section>
</template>
