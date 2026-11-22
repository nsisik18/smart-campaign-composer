import { ref } from 'vue'
import { parseError } from '../api/client'
import { parseSSE } from '../lib/sse'
import { initialGen, reduceGen, type GenEvent, type GenState } from '../lib/generation'

// useGenerationStream POST /generate'i açar ve olayları reducer'dan geçirir.
// Yeni bir start() öncekini iptal eder (backend tarafında da eski sonuç
// versiyon kontrolüyle reddedilir).
export function useGenerationStream(onValidated: () => void) {
  const state = ref<GenState>(initialGen())
  let ctrl: AbortController | null = null

  function dispatch(e: GenEvent) {
    state.value = reduceGen(state.value, e)
  }

  async function start(id: number) {
    ctrl?.abort()
    const myCtrl = new AbortController()
    ctrl = myCtrl
    state.value = { ...initialGen(), phase: 'streaming' }
    try {
      const res = await fetch(`/api/campaigns/${id}/generate`, { method: 'POST', signal: myCtrl.signal })
      if (!res.ok || !res.body) {
        const err = await parseError(res)
        dispatch({ event: 'failed', data: { code: err.code, message: err.message } })
        return
      }
      for await (const msg of parseSSE(res.body)) {
        dispatch({ event: msg.event, data: JSON.parse(msg.data) } as GenEvent)
      }
      if (state.value.phase === 'validated') onValidated()
      else if (state.value.phase === 'streaming' || state.value.phase === 'repairing') {
        dispatch({ event: 'failed', data: { code: 'stream_closed', message: 'Bağlantı üretim bitmeden kapandı. Tekrar deneyin.' } })
      }
    } catch (e) {
      if (myCtrl.signal.aborted) return
      dispatch({ event: 'failed', data: { code: 'network', message: "Sunucuya ulaşılamıyor: " + String(e) } })
    } finally {
      if (ctrl === myCtrl) ctrl = null
    }
  }

  function reset(next: GenState) {
    ctrl?.abort()
    state.value = next
  }

  return { state, start, reset }
}
