import { describe, expect, it } from 'vitest'
import { parseError } from './client'

describe('parseError', () => {
  it('reads the backend error envelope with field issues', async () => {
    const res = new Response(
      JSON.stringify({
        error: {
          code: 'invalid_segment',
          message: 'AI çıktısı geçersiz',
          details: [{ path: 'rules[0].field', code: 'unknown_field', message: 'bilinmeyen alan' }],
        },
      }),
      { status: 422 },
    )
    const e = await parseError(res)
    expect(e.status).toBe(422)
    expect(e.code).toBe('invalid_segment')
    expect(e.details).toHaveLength(1)
  })

  it('falls back on non-JSON bodies', async () => {
    const e = await parseError(new Response('<html>bad gateway</html>', { status: 502 }))
    expect(e.code).toBe('http_502')
  })
})
