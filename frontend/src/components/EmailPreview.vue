<script setup lang="ts">
import LengthBadge from './LengthBadge.vue'
import type { FieldCheck } from '../types/campaign'

defineProps<{
  subject: string
  body: string
  streaming: boolean
  subjectCheck: FieldCheck | null
}>()
</script>

<template>
  <div class="space-y-2">
    <h3 class="text-sm font-medium">Email</h3>
    <div class="overflow-hidden rounded-lg border border-slate-200">
      <div class="flex items-center justify-between gap-2 border-b border-slate-200 bg-slate-50 px-4 py-2">
        <p class="text-sm" data-test="subject">
          <span class="text-slate-500">Konu:</span> <span class="font-medium">{{ subject || '—' }}</span>
        </p>
        <LengthBadge :text="subject" :limit="80" :check="subjectCheck" />
      </div>
      <div class="min-h-40 px-4 py-3 text-sm whitespace-pre-wrap" data-test="body">
        {{ body }}<span v-if="streaming" class="ml-0.5 inline-block h-4 w-1.5 animate-pulse bg-indigo-500 align-middle" />
      </div>
    </div>
  </div>
</template>
