import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import StepBar from './StepBar.vue'

describe('StepBar', () => {
  it('marks the current step and renders all five steps', () => {
    const w = mount(StepBar, { props: { current: 'messages' } })
    const items = w.findAll('li')
    expect(items).toHaveLength(5)
    expect(items[2]!.attributes('aria-current')).toBe('step')
    expect(items[2]!.text()).toContain('Mesajlar')
  })
})
