import { describe, expect, it } from 'vitest'
import { parseSSE } from './sse'

function streamOf(parts: Uint8Array[]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(c) {
      parts.forEach((p) => c.enqueue(p))
      c.close()
    },
  })
}

describe('parseSSE', () => {
  it('handles arbitrary byte splits, comments and multibyte characters', async () => {
    const raw = ': ping\n\nevent: delta\ndata: {"field":"sms_text","text":"Şükrü 😀"}\n\nevent: validated\ndata: {"version":4}\n\n'
    const bytes = new TextEncoder().encode(raw)
    for (let i = 1; i < bytes.length; i++) {
      const out = []
      for await (const m of parseSSE(streamOf([bytes.slice(0, i), bytes.slice(i)]))) out.push(m)
      expect(out).toEqual([
        { event: 'delta', data: '{"field":"sms_text","text":"Şükrü 😀"}' },
        { event: 'validated', data: '{"version":4}' },
      ])
    }
  })
})
