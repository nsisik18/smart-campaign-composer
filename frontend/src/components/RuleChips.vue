<script setup lang="ts">
import { ruleLabel } from '../lib/rules'
import type { Rule } from '../types/campaign'

defineProps<{ rules: Rule[] }>()
</script>

<template>
  <ul class="flex flex-wrap gap-2" aria-label="Segment kuralları">
    <li
      v-for="(r, i) in rules"
      :key="i"
      class="rounded-full border border-indigo-200 bg-indigo-50 px-3 py-1 text-sm text-indigo-900"
    >
      {{ ruleLabel(r) }}
    </li>
  </ul>
</template>
