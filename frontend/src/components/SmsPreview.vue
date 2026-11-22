<script setup lang="ts">
import LengthBadge from './LengthBadge.vue'
import type { FieldCheck } from '../types/campaign'

defineProps<{ text: string; streaming: boolean; check: FieldCheck | null }>()
</script>

<template>
  <div class="space-y-2">
    <div class="flex items-center justify-between">
      <h3 class="text-sm font-medium">SMS</h3>
      <LengthBadge :text="text" :limit="160" :check="check" />
    </div>
    <div class="mx-auto w-64 rounded-[2rem] border-4 border-slate-800 bg-slate-100 p-4 shadow-inner">
      <div class="min-h-24 rounded-2xl rounded-bl-sm bg-white px-3 py-2 text-sm whitespace-pre-wrap shadow-sm" data-test="sms">
        {{ text }}<span v-if="streaming" class="ml-0.5 inline-block h-4 w-1.5 animate-pulse bg-indigo-500 align-middle" />
        <span v-if="!text && !streaming" class="text-slate-400">—</span>
      </div>
    </div>
  </div>
</template>
