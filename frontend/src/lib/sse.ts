export interface SSEMessage {
  event: string
  data: string
}

// parseSSE text/event-stream gövdesini olaylara böler. EventSource POST
// desteklemediği için /generate'i fetch + ReadableStream ile okuyoruz.
// Parça sınırları rastgeledir: satır ortası, çok baytlı karakter ortası.
// TextDecoder({stream:true}) yarım UTF-8'i bir sonraki parçaya taşır.
export async function* parseSSE(body: ReadableStream<Uint8Array>): AsyncGenerator<SSEMessage> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  try {
    for (;;) {
      const { value, done } = await reader.read()
      buf += done ? decoder.decode() : decoder.decode(value, { stream: true })
      let idx: number
      while ((idx = buf.search(/\r?\n\r?\n/)) >= 0) {
        const block = buf.slice(0, idx)
        buf = buf.slice(idx + (buf[idx] === '\r' ? 4 : 2))
        const msg = parseBlock(block)
        if (msg) yield msg
      }
      if (done) break
    }
  } finally {
    reader.releaseLock()
  }
}

function parseBlock(block: string): SSEMessage | null {
  let event = 'message'
  const data: string[] = []
  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith(':')) continue // yorum / keepalive
    const i = line.indexOf(':')
    const field = i < 0 ? line : line.slice(0, i)
    const value = i < 0 ? '' : line.slice(i + 1).replace(/^ /, '')
    if (field === 'event') event = value
    else if (field === 'data') data.push(value)
  }
  return data.length ? { event, data: data.join('\n') } : null
}
