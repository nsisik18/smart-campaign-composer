import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ErrorPanel from './ErrorPanel.vue'
import { ApiError } from '../api/client'

describe('ErrorPanel', () => {
  it('explains unsupported criteria and lists what is supported', () => {
    const err = new ApiError(422, 'no_supported_criteria', 'x', [
      { unsupported_criteria: ['İstanbulkart kullananlar'], supported_fields: ['city', 'total_orders'] },
    ])
    const w = mount(ErrorPanel, { props: { error: err } })
    expect(w.find('[data-test="unsupported-error"]').exists()).toBe(true)
    expect(w.text()).toContain('İstanbulkart kullananlar')
    expect(w.text()).toContain('şehir, toplam sipariş sayısı')
    expect(w.text()).not.toContain('Kod:')
  })

  it('keeps field-level issues for other errors', () => {
    const err = new ApiError(422, 'invalid_segment', 'geçersiz', [{ path: 'rules[0].field', code: 'unknown_field', message: 'bilinmeyen alan' }])
    const w = mount(ErrorPanel, { props: { error: err } })
    expect(w.text()).toContain('rules[0].field')
    expect(w.text()).toContain('Kod: invalid_segment')
  })
})
