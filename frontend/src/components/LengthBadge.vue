<script setup lang="ts">
import { computed } from 'vue'
import { runeLength } from '../lib/rules'
import type { FieldCheck } from '../types/campaign'

// Stream sırasında şablonun canlı rune sayısı; doğrulandıktan sonra
// backend'in en uzun değerlerle render ettiği uzunluk gösterilir.
const props = defineProps<{ text: string; limit: number; check: FieldCheck | null }>()

const shown = computed(() => (props.check ? props.check.rendered_max : runeLength(props.text)))
const over = computed(() => shown.value > props.limit)
</script>

<template>
  <span
    class="font-mono text-xs"
    :class="over ? 'text-rose-600' : 'text-slate-500'"
    :title="check ? 'En uzun ad ve şehirle render edilmiş uzunluk' : 'Şablon uzunluğu (karakter)'"
  >
    {{ shown }} / {{ limit }}<template v-if="check"> · render</template>
  </span>
</template>
