<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { fetchReady, type ReadyResponse } from '../api/health'

type State = { kind: 'loading' } | { kind: 'ok' | 'degraded'; data: ReadyResponse } | { kind: 'down' }

const state = ref<State>({ kind: 'loading' })
let timer: ReturnType<typeof setInterval> | undefined

async function poll() {
  try {
    const data = await fetchReady()
    state.value = { kind: data.status === 'ok' ? 'ok' : 'degraded', data }
  } catch {
    state.value = { kind: 'down' }
  }
}

onMounted(() => {
  poll()
  timer = setInterval(poll, 5000)
})
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <span
    class="inline-flex items-center gap-2 rounded-md px-2 py-1 text-xs font-medium"
    :class="{
      'bg-slate-100 text-slate-500': state.kind === 'loading',
      'bg-emerald-100 text-emerald-800': state.kind === 'ok',
      'bg-amber-100 text-amber-800': state.kind === 'degraded',
      'bg-rose-100 text-rose-800': state.kind === 'down',
    }"
    :title="'data' in state ? JSON.stringify(state.data.checks) : undefined"
  >
    <span class="h-2 w-2 rounded-full bg-current" />
    <template v-if="state.kind === 'loading'">Kontrol ediliyor…</template>
    <template v-else-if="state.kind === 'ok'">Backend hazır</template>
    <template v-else-if="state.kind === 'degraded'">Bağımlılık erişilemiyor</template>
    <template v-else>Backend'e ulaşılamıyor</template>
  </span>
</template>
