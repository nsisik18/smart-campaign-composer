import { describe, expect, it } from 'vitest'
import { ruleLabel, runeLength } from './rules'

describe('ruleLabel', () => {
  it('formats every field', () => {
    expect(ruleLabel({ field: 'city', op: 'eq', value: 'istanbul' })).toBe('Şehir: İstanbul')
    expect(ruleLabel({ field: 'city', op: 'in', value: ['ankara', 'izmir'] })).toBe('Şehir: Ankara, İzmir')
    expect(ruleLabel({ field: 'cart_added_within_days', op: 'lte', value: 30 })).toBe('Sepet: son 30 gün')
    expect(ruleLabel({ field: 'purchased_since_last_cart', op: 'eq', value: false })).toBe('Sepetten sonra alışveriş yok')
    expect(ruleLabel({ field: 'purchased_within_days', op: 'gt', value: 90 })).toBe('Son 90 gündür alışveriş yok')
    expect(ruleLabel({ field: 'total_orders', op: 'gte', value: 3 })).toBe('Sipariş sayısı ≥ 3')
  })
})

describe('runeLength', () => {
  it('counts code points, not UTF-16 units', () => {
    expect(runeLength('şğü')).toBe(3)
    expect(runeLength('😀')).toBe(1) // '😀'.length === 2
  })
})
