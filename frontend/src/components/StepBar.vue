<script setup lang="ts">
export type StepKey = 'brief' | 'audience' | 'messages' | 'approve' | 'monitor'

const props = defineProps<{ current: StepKey }>()

const steps: { key: StepKey; label: string }[] = [
  { key: 'brief', label: 'Brief' },
  { key: 'audience', label: 'Kitle' },
  { key: 'messages', label: 'Mesajlar' },
  { key: 'approve', label: 'Onay' },
  { key: 'monitor', label: 'İzleme' },
]

const currentIndex = () => steps.findIndex((s) => s.key === props.current)
</script>

<template>
  <ol class="flex gap-2 text-sm" aria-label="Kampanya adımları">
    <li
      v-for="(s, i) in steps"
      :key="s.key"
      :aria-current="s.key === current ? 'step' : undefined"
      class="flex items-center gap-2 rounded-full px-3 py-1"
      :class="
        i < currentIndex()
          ? 'bg-emerald-100 text-emerald-800'
          : s.key === current
            ? 'bg-indigo-600 text-white'
            : 'bg-slate-200 text-slate-500'
      "
    >
      <span class="font-mono text-xs">{{ i + 1 }}</span>
      {{ s.label }}
    </li>
  </ol>
</template>
