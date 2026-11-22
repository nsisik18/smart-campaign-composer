export interface ReadyResponse {
  status: 'ok' | 'unavailable'
  checks: Record<string, string>
}

// /health/ready 503'te de aynı gövdeyi döner; bu yüzden status koduna
// değil gövdeye bakarız. Ağ hatası ayrı bir durumdur (api hiç yok).
export async function fetchReady(signal?: AbortSignal): Promise<ReadyResponse> {
  const res = await fetch('/health/ready', { signal })
  if (!res.headers.get('content-type')?.includes('application/json')) {
    throw new Error(`unexpected response ${res.status}`)
  }
  return (await res.json()) as ReadyResponse
}
