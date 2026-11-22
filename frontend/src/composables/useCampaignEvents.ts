import { onUnmounted, ref, watch, type Ref } from 'vue'
import { isTerminal } from '../lib/monitor'
import type { MonitorStats } from '../types/campaign'

export type Connection = 'idle' | 'connecting' | 'live' | 'reconnecting' | 'closed'

// useCampaignEvents GET /events akışını EventSource ile dinler.
//
// - snapshot ve stats olaylarının ikisi de TAM durumdur; kaçırılan olay
//   sorun değil, son gelen geçerlidir.
// - Bağlantı koparsa EventSource kendiliğinden yeniden bağlanır; sunucu
//   yeni bağlantıda yeni bir snapshot gönderir. Bu sırada UI "yeniden
//   bağlanıyor" gösterir.
// - Kampanya terminal duruma geçince akışı BİZ kapatırız; aksi halde
//   sunucu akışı bitirdiğinde EventSource sonsuza dek yeniden bağlanırdı.
export function useCampaignEvents(id: Ref<number | null>, onTerminal: () => void) {
  const stats = ref<MonitorStats | null>(null)
  const connection = ref<Connection>('idle')
  let es: EventSource | null = null

  function close() {
    es?.close()
    es = null
  }

  function open(cid: number) {
    close()
    connection.value = 'connecting'
    es = new EventSource(`/api/campaigns/${cid}/events`)
    const onData = (e: MessageEvent<string>) => {
      const s = JSON.parse(e.data) as MonitorStats
      stats.value = s
      connection.value = 'live'
      if (isTerminal(s)) {
        close()
        connection.value = 'closed'
        onTerminal()
      }
    }
    es.addEventListener('snapshot', onData as EventListener)
    es.addEventListener('stats', onData as EventListener)
    es.onerror = () => {
      if (connection.value !== 'closed') connection.value = 'reconnecting'
    }
  }

  watch(
    id,
    (cid) => {
      stats.value = null
      if (cid === null) {
        close()
        connection.value = 'idle'
      } else open(cid)
    },
    { immediate: true },
  )
  onUnmounted(close)

  return { stats, connection }
}
