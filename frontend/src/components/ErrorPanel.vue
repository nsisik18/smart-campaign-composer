<script setup lang="ts">
import { computed } from 'vue'
import type { ApiError } from '../api/client'
import type { Issue } from '../types/campaign'

const props = defineProps<{ error: ApiError }>()

// invalid_segment detayları alan bazlı Issue listesidir; diğer hatalarda boş.
const issues = computed(() =>
  props.error.details.filter((d): d is Issue => typeof d === 'object' && d !== null && 'path' in d && 'message' in d),
)
</script>

<template>
  <div role="alert" class="rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-900">
    <p class="font-medium">{{ error.message }}</p>
    <ul v-if="issues.length" class="mt-2 list-disc space-y-1 pl-5">
      <li v-for="(i, idx) in issues" :key="idx">
        <code class="text-xs">{{ i.path }}</code> — {{ i.message }}
      </li>
    </ul>
    <p class="mt-2 text-xs text-rose-700">Kod: {{ error.code }}</p>
  </div>
</template>
