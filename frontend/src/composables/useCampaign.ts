import { computed, ref } from 'vue'
import { api, ApiError } from '../api/client'
import type { CampaignView } from '../types/campaign'

// useCampaign kampanya durumunun tek kaynağıdır. Adım ve buton aktifliği
// buradaki state'ten hesaplanır; bileşenler kendi kopyasını tutmaz.
export function useCampaign() {
  const view = ref<CampaignView | null>(null)
  const busy = ref<null | 'loading' | 'analyzing'>(null)
  const error = ref<ApiError | null>(null)
  const repaired = ref(false)

  const campaign = computed(() => view.value?.campaign ?? null)

  function remember(id: number | null) {
    const url = new URL(window.location.href)
    if (id === null) url.searchParams.delete('c')
    else url.searchParams.set('c', String(id))
    window.history.replaceState(null, '', url)
  }

  async function run<T>(kind: 'loading' | 'analyzing', fn: () => Promise<T>): Promise<T | undefined> {
    busy.value = kind
    error.value = null
    try {
      return await fn()
    } catch (e) {
      error.value = e instanceof ApiError ? e : new ApiError(0, 'unknown', String(e))
      return undefined
    } finally {
      busy.value = null
    }
  }

  // load sayfa yenilendiğinde URL'deki kampanyayı geri getirir.
  async function load(id: number) {
    const v = await run('loading', () => api.getCampaign(id))
    if (v) view.value = v
    else remember(null)
  }

  // refresh ekranı "yükleniyor"a almadan kampanyayı yeniden okur
  // (örn. üretim doğrulandıktan sonra status/version senkronu).
  async function refresh(id: number) {
    try {
      view.value = await api.getCampaign(id)
    } catch (e) {
      if (e instanceof ApiError) error.value = e
    }
  }

  // analyzeBrief: kampanya yoksa oluşturur, sonra analiz eder. Analiz
  // başarısız olursa kampanya draft'ta kalır; tekrar deneme aynı kampanyayı kullanır.
  async function analyzeBrief(brief: string) {
    await run('analyzing', async () => {
      if (!view.value || view.value.campaign.brief !== brief) {
        view.value = await api.createCampaign(brief)
        remember(view.value.campaign.id)
      }
      const res = await api.analyze(view.value.campaign.id)
      view.value = { campaign: res.campaign, reach: res.reach, checks: res.checks, queued: res.queued }
      repaired.value = res.repaired
    })
  }

  async function reanalyze() {
    if (!view.value) return
    const id = view.value.campaign.id
    await run('analyzing', async () => {
      const res = await api.analyze(id)
      view.value = { campaign: res.campaign, reach: res.reach, checks: res.checks, queued: res.queued }
      repaired.value = res.repaired
    })
  }

  // setView alt adımların (düzenleme, onay) döndürdüğü güncel görünümü yazar.
  function setView(v: CampaignView) {
    view.value = v
  }

  function reset() {
    view.value = null
    error.value = null
    repaired.value = false
    remember(null)
  }

  return { view, campaign, busy, error, repaired, load, refresh, setView, analyzeBrief, reanalyze, reset }
}
