<script setup lang="ts">
import { computed } from 'vue'
import type { ApiError } from '../api/client'
import type { Issue } from '../types/campaign'

const props = defineProps<{ error: ApiError }>()

// invalid_segment detayları alan bazlı Issue listesidir; diğer hatalarda boş.
const issues = computed(() =>
  props.error.details.filter((d): d is Issue => typeof d === 'object' && d !== null && 'path' in d && 'message' in d),
)

// no_supported_criteria: model kriterleri veride karşılığı olmadığı için
// beyan etti. Bu bir hata değil, kullanıcının brief'i değiştirmesi gereken
// bir sonuç; neyin desteklenmediğini ve neyin desteklendiğini gösteririz.
interface Unsupported {
  unsupported_criteria: string[]
  supported_fields: string[]
}
const unsupported = computed<Unsupported | null>(() => {
  if (props.error.code !== 'no_supported_criteria') return null
  const d = props.error.details[0] as Partial<Unsupported> | undefined
  return { unsupported_criteria: d?.unsupported_criteria ?? [], supported_fields: d?.supported_fields ?? [] }
})

const fieldLabels: Record<string, string> = {
  city: 'şehir',
  cart_added_within_days: 'sepete ekleme zamanı',
  purchased_since_last_cart: 'sepetten sonra alışveriş yapıp yapmadığı',
  purchased_within_days: 'son alışveriş zamanı',
  total_orders: 'toplam sipariş sayısı',
}
</script>

<template>
  <div
    v-if="unsupported"
    role="alert"
    data-test="unsupported-error"
    class="space-y-2 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900"
  >
    <p class="font-medium">Bu brief'teki hedef kitle mevcut müşteri verisiyle ifade edilemiyor.</p>
    <div v-if="unsupported.unsupported_criteria.length">
      <p>Desteklenmeyen kriter:</p>
      <ul class="list-disc pl-5">
        <li v-for="c in unsupported.unsupported_criteria" :key="c">{{ c }}</li>
      </ul>
    </div>
    <p>
      Kitleyi şunlarla tarif edebilirsiniz:
      {{ unsupported.supported_fields.map((f) => fieldLabels[f] ?? f).join(', ') }}.
      Örnek: “İzmir'de son 14 günde sepete ekleyip almayanlar”.
    </p>
  </div>
  <div v-else role="alert" class="rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-900">
    <p class="font-medium">{{ error.message }}</p>
    <ul v-if="issues.length" class="mt-2 list-disc space-y-1 pl-5">
      <li v-for="(i, idx) in issues" :key="idx">
        <code class="text-xs">{{ i.path }}</code> — {{ i.message }}
      </li>
    </ul>
    <p class="mt-2 text-xs text-rose-700">Kod: {{ error.code }}</p>
  </div>
</template>
