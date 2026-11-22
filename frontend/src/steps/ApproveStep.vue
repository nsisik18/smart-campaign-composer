<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, ApiError } from '../api/client'
import { LIMITS, localIssues, renderedLength } from '../lib/content'
import { render } from '../lib/rules'
import type { CampaignView, Content, ContentField, Issue } from '../types/campaign'

const props = defineProps<{ view: CampaignView }>()
const emit = defineEmits<{ updated: [view: CampaignView] }>()

const c = computed(() => props.view.campaign)
const vars = computed(() => props.view.checks?.preview_vars ?? null)

// Taslak: kayıtlı içerikten başlar; kampanya versiyonu değişince (kayıt,
// yeniden üretim) yeniden kurulur.
const draft = ref<Content>({ sms_text: '', email_subject: '', email_body: '' })
const serverIssues = ref<Issue[]>([]) // son kayıt denemesinde backend'in döndürdüğü alan hataları
watch(
  () => [c.value.version, c.value.content] as const,
  () => {
    if (c.value.content) draft.value = { ...c.value.content }
    serverIssues.value = []
  },
  { immediate: true },
)

const dirty = computed(() => {
  const saved = c.value.content
  return !!saved && (Object.keys(LIMITS) as ContentField[]).some((f) => draft.value[f] !== saved[f])
})
const local = computed(() => localIssues(draft.value, vars.value))
const issuesFor = (f: ContentField) => [...local.value, ...serverIssues.value].filter((i) => i.path === f)

const saving = ref(false)
const approving = ref(false)
const error = ref<ApiError | null>(null)
const confirmOpen = ref(false)

const canSave = computed(() => dirty.value && local.value.length === 0 && !saving.value)
const canApprove = computed(() => !dirty.value && local.value.length === 0 && serverIssues.value.length === 0 && !approving.value && c.value.status === 'generated')

async function save() {
  saving.value = true
  error.value = null
  try {
    emit('updated', await api.editContent(c.value.id, c.value.version, draft.value))
  } catch (e) {
    const err = e instanceof ApiError ? e : new ApiError(0, 'unknown', String(e))
    serverIssues.value = err.code === 'invalid_content' ? (err.details as Issue[]) : []
    error.value = err
  } finally {
    saving.value = false
  }
}

async function approve() {
  approving.value = true
  error.value = null
  try {
    emit('updated', await api.approve(c.value.id, c.value.version))
    confirmOpen.value = false
  } catch (e) {
    error.value = e instanceof ApiError ? e : new ApiError(0, 'unknown', String(e))
    confirmOpen.value = false
  } finally {
    approving.value = false
  }
}

const fields: { key: ContentField; label: string; rows: number }[] = [
  { key: 'sms_text', label: 'SMS', rows: 3 },
  { key: 'email_subject', label: 'Email konusu', rows: 1 },
  { key: 'email_body', label: 'Email gövdesi', rows: 8 },
]
const fmt = (n: number) => n.toLocaleString('tr-TR')
</script>

<template>
  <section class="space-y-4 rounded-lg border border-slate-200 bg-white p-6">
    <div>
      <h2 class="font-medium">Onay</h2>
      <p class="text-sm text-slate-600">
        Metinleri düzenleyebilirsiniz. Her değişiklik Claude çıktısıyla aynı kurallardan geçer; uzunluklar kitledeki en uzun
        adla<template v-if="vars"> ({{ vars.first_name }}, {{ vars.city }})</template> ölçülür.
      </p>
    </div>

    <div v-for="f in fields" :key="f.key" class="space-y-1">
      <div class="flex items-center justify-between">
        <label :for="f.key" class="text-sm font-medium">{{ f.label }}</label>
        <span
          class="font-mono text-xs"
          :data-test="`count-${f.key}`"
          :class="renderedLength(draft[f.key], vars) > LIMITS[f.key] ? 'text-rose-600' : 'text-slate-500'"
        >
          {{ renderedLength(draft[f.key], vars) }} / {{ LIMITS[f.key] }}
        </span>
      </div>
      <textarea
        :id="f.key"
        v-model="draft[f.key]"
        :rows="f.rows"
        :data-test="`edit-${f.key}`"
        class="w-full rounded-md border p-2 text-sm focus:outline-none"
        :class="issuesFor(f.key).length ? 'border-rose-400' : 'border-slate-300 focus:border-indigo-500'"
      />
      <ul v-if="issuesFor(f.key).length" class="text-xs text-rose-700">
        <li v-for="(i, idx) in issuesFor(f.key)" :key="idx">{{ i.message }}</li>
      </ul>
    </div>

    <div v-if="error" role="alert" class="rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-900">
      {{ error.message }}
      <span v-if="error.code === 'conflict'" class="block text-xs">Kampanya başka bir yerde değişmiş olabilir; sayfayı yenileyin.</span>
    </div>

    <div class="flex items-center justify-end gap-3 border-t border-slate-100 pt-4">
      <span v-if="dirty" class="text-xs text-amber-700">Kaydedilmemiş değişiklik var; onaydan önce kaydedin.</span>
      <button
        type="button"
        data-test="save"
        class="rounded-md border border-slate-300 px-4 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-50"
        :disabled="!canSave"
        @click="save"
      >
        {{ saving ? 'Kaydediliyor…' : 'Değişiklikleri kaydet' }}
      </button>
      <button
        type="button"
        data-test="approve"
        class="rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:bg-slate-300"
        :disabled="!canApprove"
        @click="confirmOpen = true"
      >
        Onayla ve gönder
      </button>
    </div>

    <div v-if="confirmOpen" class="fixed inset-0 z-10 flex items-center justify-center bg-slate-900/40 p-4" role="dialog" aria-modal="true">
      <div class="w-full max-w-lg space-y-4 rounded-lg bg-white p-6 shadow-xl" data-test="confirm">
        <h3 class="font-medium">Kampanyayı onayla ve gönder</h3>
        <dl v-if="view.reach" class="grid grid-cols-2 gap-3 text-sm">
          <div class="rounded bg-slate-50 p-3">
            <dt class="text-xs text-slate-500">SMS alıcısı</dt>
            <dd class="text-xl font-semibold">{{ fmt(view.reach.sms_eligible) }}</dd>
          </div>
          <div class="rounded bg-slate-50 p-3">
            <dt class="text-xs text-slate-500">Email alıcısı</dt>
            <dd class="text-xl font-semibold">{{ fmt(view.reach.email_eligible) }}</dd>
          </div>
        </dl>
        <div v-if="vars" class="space-y-2 text-sm">
          <p class="text-xs text-slate-500">Örnek render ({{ vars.first_name }}, {{ vars.city }}):</p>
          <p class="rounded bg-slate-50 p-2 whitespace-pre-wrap">{{ render(draft.sms_text, vars) }}</p>
          <p class="rounded bg-slate-50 p-2 font-medium">{{ render(draft.email_subject, vars) }}</p>
        </div>
        <p class="text-xs text-slate-600">
          Onaydan sonra metinler değiştirilemez. Alıcı listesi bu anda dondurulur; gönderim anında izni kapanmış kişiler atlanır.
        </p>
        <div class="flex justify-end gap-3">
          <button type="button" class="rounded-md px-4 py-2 text-sm" :disabled="approving" @click="confirmOpen = false">Vazgeç</button>
          <button
            type="button"
            data-test="confirm-approve"
            class="rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
            :disabled="approving"
            @click="approve"
          >
            {{ approving ? 'Kuyruğa alınıyor…' : 'Onayla ve gönder' }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
