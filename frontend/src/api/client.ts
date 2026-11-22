import type { AnalyzeResult, CampaignView, Content } from '../types/campaign'

// ApiError backend'in tek hata formatını taşır:
// {"error":{"code","message","details"}}. Ağ hatası code="network" olur.
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly details: unknown[] = [],
  ) {
    super(message)
  }
}

export async function parseError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as { error?: { code?: string; message?: string; details?: unknown[] } }
    if (body.error?.code) {
      return new ApiError(res.status, body.error.code, body.error.message ?? '', body.error.details ?? [])
    }
  } catch {
    /* gövde JSON değil */
  }
  return new ApiError(res.status, 'http_' + res.status, `Beklenmeyen sunucu cevabı (${res.status})`)
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', "Sunucuya ulaşılamıyor. Backend'in çalıştığından emin olun.")
  }
  if (!res.ok) throw await parseError(res)
  return (await res.json()) as T
}

export const api = {
  createCampaign: (brief: string) => request<CampaignView>('POST', '/api/campaigns', { brief }),
  getCampaign: (id: number) => request<CampaignView>('GET', `/api/campaigns/${id}`),
  analyze: (id: number) => request<AnalyzeResult>('POST', `/api/campaigns/${id}/analyze`),
  editContent: (id: number, expectedVersion: number, c: Content) =>
    request<CampaignView>('PATCH', `/api/campaigns/${id}/content`, { expected_version: expectedVersion, ...c }),
  approve: (id: number, expectedVersion: number) =>
    request<CampaignView>('POST', `/api/campaigns/${id}/approve`, { expected_version: expectedVersion }),
}
