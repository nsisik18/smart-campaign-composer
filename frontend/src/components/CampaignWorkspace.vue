<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import StepBar, { type StepKey } from './StepBar.vue'
import ErrorPanel from './ErrorPanel.vue'
import BriefStep from '../steps/BriefStep.vue'
import AudienceStep from '../steps/AudienceStep.vue'
import MessagesStep from '../steps/MessagesStep.vue'
import ApproveStep from '../steps/ApproveStep.vue'
import MonitorStep from '../steps/MonitorStep.vue'
import { useCampaign } from '../composables/useCampaign'
import { useGenerationStream } from '../composables/useGenerationStream'
import { useCampaignEvents } from '../composables/useCampaignEvents'
import { fromSaved, initialGen } from '../lib/generation'

const { view, campaign, busy, error, repaired, load, refresh, setView, analyzeBrief, reanalyze, reset } = useCampaign()

// Adım tamamen backend durumundan türetilir.
const step = computed<StepKey>(() => {
  switch (campaign.value?.status) {
    case undefined:
    case 'draft':
      return 'brief'
    case 'analyzed':
      return 'audience'
    case 'generated':
      return 'approve'
    case 'approved':
    case 'sending':
    case 'completed':
    case 'completed_with_errors':
      return 'monitor'
  }
})

const canReanalyze = computed(() => campaign.value?.status === 'draft' || campaign.value?.status === 'analyzed')

// Üretim doğrulanınca kampanyayı yeniden okuyup status/version'ı senkronlarız.
const gen = useGenerationStream(() => {
  if (campaign.value) refresh(campaign.value.id)
})

// Kampanya değişince (yükleme, yeni kampanya) üretim durumunu kayıttan kur.
watch(
  () => campaign.value?.id,
  () => {
    const c = campaign.value
    gen.reset(c?.content ? fromSaved(c.content, view.value?.checks ?? null, c.version) : initialGen())
  },
)

// Düzenleme kaydı veya onay sonrası: görünümü güncelle, üretim panelini
// kayıtlı içerikle hizala (stream edilmiş eski metin kalmasın).
function onUpdated(v: typeof view.value) {
  if (!v) return
  setView(v)
  if (v.campaign.content) gen.reset(fromSaved(v.campaign.content, v.checks, v.campaign.version))
}

const showApprove = computed(() => campaign.value?.status === 'generated' && gen.state.value.phase === 'validated')

// Pano: onaydan sonra (sending/completed*) olayları dinle. Terminal duruma
// geçince kampanyayı yeniden oku (adım çubuğu ve durum güncellensin).
const monitoring = computed(() => ['sending', 'completed', 'completed_with_errors', 'approved'].includes(campaign.value?.status ?? ''))
const monitorId = computed(() => (monitoring.value && campaign.value ? campaign.value.id : null))
const events = useCampaignEvents(monitorId, () => {
  if (campaign.value) refresh(campaign.value.id)
})

const showMessages = computed(() => !!campaign.value?.segment && (view.value?.reach?.reachable ?? 0) > 0)
const messagesLocked = computed(() => !['analyzed', 'generated'].includes(campaign.value?.status ?? ''))

onMounted(() => {
  const id = Number(new URLSearchParams(window.location.search).get('c'))
  if (Number.isInteger(id) && id > 0) load(id)
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <StepBar :current="step" />
      <button v-if="campaign" type="button" class="text-sm text-slate-500 hover:underline" @click="reset">
        Yeni kampanya
      </button>
    </div>

    <p v-if="busy === 'loading'" class="text-sm text-slate-500">Kampanya yükleniyor…</p>

    <template v-else>
      <BriefStep
        :initial="campaign?.brief ?? ''"
        :busy="busy === 'analyzing' && !campaign?.segment"
        :locked="!!campaign?.segment"
        @analyze="analyzeBrief"
      />

      <ErrorPanel v-if="error" :error="error" />

      <AudienceStep
        v-if="campaign?.segment && view?.reach"
        :segment="campaign.segment"
        :reach="view.reach"
        :repaired="repaired"
        :busy="busy === 'analyzing'"
        :can-reanalyze="canReanalyze"
        @reanalyze="reanalyze"
      />

      <MessagesStep
        v-if="showMessages && campaign"
        :gen="gen.state.value"
        :locked="messagesLocked"
        @generate="gen.start(campaign.id)"
      />

      <ApproveStep v-if="showApprove && view" :view="view" @updated="onUpdated" />

      <MonitorStep v-if="monitoring" :stats="events.stats.value" :connection="events.connection.value" />
    </template>
  </div>
</template>
