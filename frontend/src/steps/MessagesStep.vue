<script setup lang="ts">
import { computed, ref } from 'vue'
import SmsPreview from '../components/SmsPreview.vue'
import EmailPreview from '../components/EmailPreview.vue'
import type { GenState } from '../lib/generation'
import { render } from '../lib/rules'

const props = defineProps<{ gen: GenState; locked: boolean }>()
const emit = defineEmits<{ generate: [] }>()

const busy = computed(() => props.gen.phase === 'streaming' || props.gen.phase === 'repairing')
const validated = computed(() => props.gen.phase === 'validated')
// Doğrulama sonucu (başarılı ya da başarısız) geldiyse sayaçlar backend'in
// render edilmiş uzunluğunu gösterir; stream sırasında şablon uzunluğu.
const checked = computed(() => (props.gen.phase === 'validated' || props.gen.phase === 'failed' ? props.gen.checks : null))

// Önizleme modu: şablon ya da kitledeki en uzun ad/şehirle render.
const previewRendered = ref(false)
const vars = computed(() => props.gen.checks?.preview_vars ?? null)
const show = (tpl: string) => (previewRendered.value && vars.value && validated.value ? render(tpl, vars.value) : tpl)
</script>

<template>
  <section class="space-y-4 rounded-lg border border-slate-200 bg-white p-6">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h2 class="font-medium">Mesajlar</h2>
        <p class="text-sm text-slate-600">Claude SMS ve email metnini yazar; backend kuralları doğrulamadan onaylanamaz.</p>
      </div>
      <div class="flex items-center gap-3">
        <span v-if="gen.phase === 'streaming'" class="text-xs text-indigo-700">Yazılıyor…</span>
        <span v-else-if="gen.phase === 'repairing'" class="rounded bg-amber-100 px-2 py-0.5 text-xs text-amber-800" data-test="repairing">
          Kural ihlali bulundu, düzeltiliyor…
        </span>
        <span v-else-if="validated" class="rounded bg-emerald-100 px-2 py-0.5 text-xs text-emerald-800">Doğrulandı</span>
        <button
          type="button"
          data-test="generate"
          class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:bg-slate-300"
          :disabled="busy || locked"
          @click="emit('generate')"
        >
          {{ gen.phase === 'idle' ? 'Mesajları üret' : 'Yeniden üret' }}
        </button>
      </div>
    </div>

    <div v-if="gen.errors.length" class="rounded-md border p-3 text-sm"
      :class="gen.phase === 'failed' ? 'border-rose-200 bg-rose-50 text-rose-900' : 'border-amber-200 bg-amber-50 text-amber-900'">
      <p class="font-medium">{{ gen.phase === 'failed' ? 'Doğrulamadan geçmeyen kurallar:' : "Claude'un ilk taslağındaki sorunlar (otomatik düzeltiliyor):" }}</p>
      <ul class="mt-1 list-disc pl-5">
        <li v-for="(e, i) in gen.errors" :key="i"><code class="text-xs">{{ e.path }}</code> — {{ e.message }}</li>
      </ul>
    </div>
    <div v-else-if="gen.failure" role="alert" class="rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-900">
      {{ gen.failure.message }} <span class="text-xs text-rose-700">({{ gen.failure.code }})</span>
    </div>

    <template v-if="gen.phase !== 'idle'">
      <label v-if="validated && vars" class="flex items-center gap-2 text-xs text-slate-600">
        <input v-model="previewRendered" type="checkbox" />
        En uzun değerlerle önizle ({{ vars.first_name }}, {{ vars.city }})
      </label>
      <div class="grid gap-6 md:grid-cols-[auto_1fr]">
        <SmsPreview :text="show(gen.fields.sms_text)" :streaming="busy" :check="checked?.sms_text ?? null" />
        <EmailPreview
          :subject="show(gen.fields.email_subject)"
          :body="show(gen.fields.email_body)"
          :streaming="busy"
          :subject-check="checked?.email_subject ?? null"
        />
      </div>
    </template>
    <p v-else class="rounded-md bg-slate-50 p-4 text-sm text-slate-600">
      Henüz mesaj üretilmedi. Kitle hazır; “Mesajları üret” ile başlayın.
    </p>

  </section>
</template>
