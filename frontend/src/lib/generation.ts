import type { Checks, Content, ContentField, Issue } from '../types/campaign'

export type GenPhase = 'idle' | 'streaming' | 'repairing' | 'validated' | 'failed'

export interface GenState {
  phase: GenPhase
  fields: Content
  version: number | null
  errors: Issue[]
  checks: Checks | null
  failure: { code: string; message: string } | null
}

export type GenEvent =
  | { event: 'started'; data: { version: number } }
  | { event: 'delta'; data: { field: ContentField; text: string } }
  | { event: 'repairing'; data: { errors: Issue[] } }
  | { event: 'validated'; data: { content: Content; checks: Checks; version: number } }
  | { event: 'failed'; data: { code: string; message: string; errors?: Issue[]; checks?: Checks } }

const empty = (): Content => ({ sms_text: '', email_subject: '', email_body: '' })

export const initialGen = (): GenState => ({
  phase: 'idle',
  fields: empty(),
  version: null,
  errors: [],
  checks: null,
  failure: null,
})

// Kayıtlı içerikten başlangıç durumu (sayfa yenileme, status=generated).
export const fromSaved = (content: Content, checks: Checks | null, version: number): GenState => ({
  ...initialGen(),
  phase: 'validated',
  fields: { ...content },
  checks,
  version,
})

// reduceGen saf fonksiyon; stream olaylarını UI durumuna çevirir.
// "validated" otoriterdir: biriken delta'lar yerine backend'in doğruladığı
// (normalize edilmiş) içerik gösterilir.
export function reduceGen(s: GenState, e: GenEvent): GenState {
  switch (e.event) {
    case 'started':
      return { ...initialGen(), phase: 'streaming', version: e.data.version }
    case 'delta': {
      if (s.phase !== 'streaming' && s.phase !== 'repairing') return s
      const fields = { ...s.fields, [e.data.field]: s.fields[e.data.field] + e.data.text }
      return { ...s, fields }
    }
    case 'repairing':
      // Onarım çağrısı alanları baştan yazar; eski metin silinir, hatalar görünür kalır.
      return { ...s, phase: 'repairing', fields: empty(), errors: e.data.errors }
    case 'validated':
      return { ...s, phase: 'validated', fields: { ...e.data.content }, checks: e.data.checks, version: e.data.version, errors: [], failure: null }
    case 'failed':
      return {
        ...s,
        phase: 'failed',
        errors: e.data.errors ?? [],
        checks: e.data.checks ?? null,
        failure: { code: e.data.code, message: e.data.message },
      }
  }
}

export const canApprove = (s: GenState) => s.phase === 'validated' && !!s.checks && s.checks.sms_text.ok && s.checks.email_subject.ok && s.checks.email_body.ok
