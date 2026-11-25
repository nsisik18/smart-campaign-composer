import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ApproveStep from './ApproveStep.vue'
import type { CampaignView } from '../types/campaign'

const ok = { template_length: 10, rendered_max: 11, limit: 160, ok: true }
const view = (): CampaignView => ({
  campaign: {
    id: 7, brief: 'b', status: 'generated', version: 5, segment: null, created_at: '', updated_at: '',
    content: { sms_text: 'Merhaba {{first_name}}', email_subject: 'Konu', email_body: 'Gövde' },
  },
  reach: { matched: 450, sms_eligible: 302, email_eligible: 356, reachable: 405 },
  checks: { sms_text: ok, email_subject: ok, email_body: ok, preview_vars: { first_name: 'Abdurrahmangazi', city: 'İstanbul' } },
  queued: null,
})

const btn = (w: ReturnType<typeof mount>, t: string) => w.find(`[data-test="${t}"]`)

afterEach(() => vi.restoreAllMocks())

// UI-01: rune bazlı sayaç (en uzun adla render) ve geçersiz içerikte onay kapalı.
describe('ApproveStep', () => {
  it('counts rendered runes and disables approve/save for invalid content', async () => {
    const w = mount(ApproveStep, { props: { view: view() } })
    // "Merhaba " (8) + "Abdurrahmangazi" (15) = 23
    expect(btn(w, 'count-sms_text').text()).toBe('23 / 160')
    expect(btn(w, 'approve').attributes('disabled')).toBeUndefined()

    // placeholder + boşluk + 145 'ş': şablon 160, render 161 -> kırmızı, kaydet/onay kapalı.
    await btn(w, 'edit-sms_text').setValue('{{first_name}} ' + 'ş'.repeat(145))
    expect(btn(w, 'count-sms_text').text()).toBe('161 / 160')
    expect(btn(w, 'count-sms_text').classes()).toContain('text-rose-600')
    expect(btn(w, 'save').attributes('disabled')).toBeDefined()
    expect(btn(w, 'approve').attributes('disabled')).toBeDefined()

    await btn(w, 'edit-sms_text').setValue('Selam {{last_name}}')
    expect(w.text()).toContain('bilinmeyen placeholder {{last_name}}')
    expect(btn(w, 'save').attributes('disabled')).toBeDefined()
  })

  it('requires saving a valid edit before approving', async () => {
    const w = mount(ApproveStep, { props: { view: view() } })
    await btn(w, 'edit-sms_text').setValue('Selam {{first_name}}')
    expect(btn(w, 'save').attributes('disabled')).toBeUndefined()
    expect(btn(w, 'approve').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('Kaydedilmemiş değişiklik')
  })

  it('sends expected_version on approve and shows server conflicts', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'conflict', message: 'campaign changed' } }), { status: 409 }),
    )
    const w = mount(ApproveStep, { props: { view: view() } })
    await btn(w, 'approve').trigger('click')
    expect(btn(w, 'confirm').text()).toContain('302')
    await btn(w, 'confirm-approve').trigger('click')
    await flushPromises()
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/campaigns/7/approve')
    expect(JSON.parse(String((init as RequestInit).body))).toEqual({ expected_version: 5 })
    expect(w.text()).toContain('sayfayı yenileyin')
    expect(w.emitted('updated')).toBeUndefined()
  })

  it('flags a suffix attached to a placeholder', async () => {
    const w = mount(ApproveStep, { props: { view: view() } })
    await btn(w, 'edit-sms_text').setValue("Merhaba {{first_name}}, {{city}}'da indirim")
    expect(w.text()).toContain("placeholder'a ek getirilmiş")
    expect(btn(w, 'save').attributes('disabled')).toBeDefined()
  })

  it('requires acknowledging unapplied criteria before approving', async () => {
    const v = view()
    v.campaign.segment = { summary: 'İzmir', rules: [], unsupported_criteria: ['VIP müşteriler'] }
    const w = mount(ApproveStep, { props: { view: v } })
    await btn(w, 'approve').trigger('click')
    expect(btn(w, 'confirm-unsupported').text()).toContain('VIP müşteriler')
    expect(btn(w, 'confirm-approve').attributes('disabled')).toBeDefined()
    await btn(w, 'ack-unsupported').setValue(true)
    expect(btn(w, 'confirm-approve').attributes('disabled')).toBeUndefined()
  })
})
