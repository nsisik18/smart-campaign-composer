import { describe, expect, it } from 'vitest'
import { canApprove, initialGen, reduceGen, type GenEvent, type GenState } from './generation'
import type { Checks } from '../types/campaign'

const ok = { template_length: 10, rendered_max: 11, limit: 160, ok: true }
const checks: Checks = { sms_text: ok, email_subject: { ...ok, limit: 80 }, email_body: { ...ok, limit: 5000 }, preview_vars: { first_name: 'Abdurrahmangazi', city: 'İstanbul' } }

const run = (events: GenEvent[], s: GenState = initialGen()) => events.reduce(reduceGen, s)

// UI-02: delta'lar alan bazında doğru birleşir; onarım alanları sıfırlar.
describe('reduceGen', () => {
  it('accumulates deltas per field in order', () => {
    const s = run([
      { event: 'started', data: { version: 3 } },
      { event: 'delta', data: { field: 'sms_text', text: 'Merhaba ' } },
      { event: 'delta', data: { field: 'email_subject', text: 'Konu' } },
      { event: 'delta', data: { field: 'sms_text', text: '{{first_name}}' } },
      { event: 'delta', data: { field: 'email_body', text: 'Satır\n' } },
    ])
    expect(s.phase).toBe('streaming')
    expect(s.version).toBe(3)
    expect(s.fields).toEqual({ sms_text: 'Merhaba {{first_name}}', email_subject: 'Konu', email_body: 'Satır\n' })
    expect(canApprove(s)).toBe(false) // kısmi metin onaylanamaz
  })

  it('clears fields on repairing and keeps the issues visible', () => {
    const s = run([
      { event: 'started', data: { version: 3 } },
      { event: 'delta', data: { field: 'sms_text', text: 'çok uzun…' } },
      { event: 'repairing', data: { errors: [{ path: 'sms_text', code: 'too_long', message: '174 karakter' }] } },
      { event: 'delta', data: { field: 'sms_text', text: 'kısa' } },
    ])
    expect(s.phase).toBe('repairing')
    expect(s.fields.sms_text).toBe('kısa')
    expect(s.errors).toHaveLength(1)
  })

  it('validated content replaces streamed text and enables approve', () => {
    const s = run([
      { event: 'started', data: { version: 3 } },
      { event: 'delta', data: { field: 'sms_text', text: '  taslak  ' } },
      { event: 'validated', data: { content: { sms_text: 'taslak', email_subject: 'k', email_body: 'b' }, checks, version: 4 } },
    ])
    expect(s.fields.sms_text).toBe('taslak')
    expect(s.version).toBe(4)
    expect(canApprove(s)).toBe(true)
  })

  it('failed keeps approve disabled and ignores late deltas', () => {
    let s = run([
      { event: 'started', data: { version: 3 } },
      { event: 'delta', data: { field: 'sms_text', text: 'Merhaba' } },
      { event: 'failed', data: { code: 'llm_truncated', message: 'kesildi' } },
    ])
    s = reduceGen(s, { event: 'delta', data: { field: 'sms_text', text: 'X' } })
    expect(s.phase).toBe('failed')
    expect(s.fields.sms_text).toBe('Merhaba')
    expect(canApprove(s)).toBe(false)
  })

  it('a new start resets the previous run', () => {
    const s = run([
      { event: 'started', data: { version: 3 } },
      { event: 'delta', data: { field: 'sms_text', text: 'eski' } },
      { event: 'started', data: { version: 5 } },
    ])
    expect(s.fields.sms_text).toBe('')
    expect(s.version).toBe(5)
  })
})
