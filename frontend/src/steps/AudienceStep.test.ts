import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AudienceStep from './AudienceStep.vue'
import BriefStep from './BriefStep.vue'

const segment = {
  summary: 'İstanbullular',
  rules: [{ field: 'city' as const, op: 'eq' as const, value: 'istanbul' }],
  unsupported_criteria: ['VIP müşteriler'],
}
const reach = { matched: 450, sms_eligible: 290, email_eligible: 360, reachable: 405 }

describe('AudienceStep', () => {
  it('shows counts, chips and the unsupported warning', () => {
    const w = mount(AudienceStep, { props: { segment, reach, repaired: false, busy: false, canReanalyze: true } })
    expect(w.find('[data-test="matched"]').text()).toBe('450')
    expect(w.text()).toContain('Şehir: İstanbul')
    expect(w.find('[data-test="unsupported"]').text()).toContain('VIP müşteriler')
  })

  it('shows an empty state instead of zeros', () => {
    const w = mount(AudienceStep, {
      props: { segment, reach: { matched: 0, sms_eligible: 0, email_eligible: 0, reachable: 0 }, repaired: false, busy: false, canReanalyze: true },
    })
    expect(w.text()).toContain('uyan müşteri yok')
    expect(w.find('[data-test="matched"]').exists()).toBe(false)
  })
})

describe('BriefStep', () => {
  it('disables analyze for empty or overlong brief (rune based)', async () => {
    const w = mount(BriefStep, { props: { initial: '', busy: false, locked: false } })
    const btn = () => w.find('[data-test="analyze"]')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('textarea').setValue('ş'.repeat(2000))
    expect(btn().attributes('disabled')).toBeUndefined()
    await w.find('textarea').setValue('ş'.repeat(2001))
    expect(btn().attributes('disabled')).toBeDefined()
  })
})
