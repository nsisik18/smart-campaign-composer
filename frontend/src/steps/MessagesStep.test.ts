import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import MessagesStep from './MessagesStep.vue'
import { fromSaved, initialGen } from '../lib/generation'

const ok = { template_length: 10, rendered_max: 11, limit: 160, ok: true }
const checks = { sms_text: ok, email_subject: { ...ok, limit: 80 }, email_body: { ...ok, limit: 5000 }, preview_vars: { first_name: 'Abdurrahmangazi', city: 'İstanbul' } }
const content = { sms_text: 'Merhaba {{first_name}}', email_subject: 'Konu', email_body: 'Gövde' }

describe('MessagesStep', () => {
  it('locks regenerate while streaming and shows validated content', async () => {
    const streaming = { ...initialGen(), phase: 'streaming' as const, fields: content }
    const w = mount(MessagesStep, { props: { gen: streaming, locked: false } })
    expect(w.find('[data-test="generate"]').attributes('disabled')).toBeDefined()

    await w.setProps({ gen: fromSaved(content, checks, 4) })
    expect(w.find('[data-test="generate"]').attributes('disabled')).toBeUndefined()
    expect(w.text()).toContain('Doğrulandı')
    expect(w.find('[data-test="sms"]').text()).toContain('Merhaba {{first_name}}')

    await w.find('input[type="checkbox"]').setValue(true)
    expect(w.find('[data-test="sms"]').text()).toContain('Merhaba Abdurrahmangazi')
  })

  it('shows the failing rendered length', () => {
    const bad = fromSaved(content, { ...checks, sms_text: { ...ok, rendered_max: 174, ok: false } }, 4)
    const w = mount(MessagesStep, { props: { gen: bad, locked: false } })
    expect(w.text()).toContain('174 / 160')
  })

  it('shows repairing state and issues', () => {
    const rep = { ...initialGen(), phase: 'repairing' as const, errors: [{ path: 'sms_text', code: 'too_long', message: '174 karakter' }] }
    const w = mount(MessagesStep, { props: { gen: rep, locked: false } })
    expect(w.find('[data-test="repairing"]').exists()).toBe(true)
    expect(w.text()).toContain('174 karakter')
  })
})

describe('MessagesStep failure counters', () => {
  it('shows the rendered length from backend checks after a failed validation', () => {
    const failed = {
      ...initialGen(),
      phase: 'failed' as const,
      fields: content,
      checks: { ...checks, email_subject: { template_length: 80, rendered_max: 81, limit: 80, ok: false } },
      errors: [{ path: 'email_subject', code: 'too_long', message: '81 karakter' }],
      failure: { code: 'invalid_output', message: 'x' },
    }
    const w = mount(MessagesStep, { props: { gen: failed, locked: false } })
    expect(w.text()).toContain('81 / 80')
  })
})
