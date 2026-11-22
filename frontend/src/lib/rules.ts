import type { Rule } from '../types/campaign'

const cityNames: Record<string, string> = {
  istanbul: 'İstanbul',
  ankara: 'Ankara',
  izmir: 'İzmir',
  bursa: 'Bursa',
  antalya: 'Antalya',
}

export const cityName = (key: string) => cityNames[key] ?? key

// ruleLabel kuralı pazarlamacının okuyacağı kısa etikete çevirir.
// Exhaustive switch: yeni alan eklenirse TypeScript burada hata verir.
export function ruleLabel(r: Rule): string {
  switch (r.field) {
    case 'city':
      return 'Şehir: ' + (r.op === 'eq' ? cityName(r.value) : r.value.map(cityName).join(', '))
    case 'cart_added_within_days':
      return `Sepet: son ${r.value} gün`
    case 'purchased_since_last_cart':
      return r.value ? 'Sepetten sonra alışveriş var' : 'Sepetten sonra alışveriş yok'
    case 'purchased_within_days':
      return r.op === 'lte' ? `Alışveriş: son ${r.value} günde` : `Son ${r.value} gündür alışveriş yok`
    case 'total_orders': {
      const sym = { gte: '≥', lte: '≤', eq: '=' }[r.op]
      return `Sipariş sayısı ${sym} ${r.value}`
    }
  }
}

// Rune (code point) sayısı: backend utf8.RuneCountInString ile aynı.
export const runeLength = (s: string) => Array.from(s).length

// render backend'deki content.Render ile aynı: düz string replace.
export function render(tpl: string, v: { first_name: string; city: string }): string {
  return tpl.split('{{first_name}}').join(v.first_name).split('{{city}}').join(v.city)
}
