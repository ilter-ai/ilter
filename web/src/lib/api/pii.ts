import { adaptPIIEvents } from './adapters'
import { buildUrl } from './queryBuilder'
import { request } from './request'
import type { PaginatedResponse, PIIEvent, PIIPattern, PIIStats } from './types'

interface GoPIIEventItem {
  id: number
  timestamp: string
  api_key_id: number
  key?: {
    id: number
    key_prefix?: string
    key_name?: string
    owner_type?: string
    owner_id?: number
    owner_name?: string
  }
  pii_type: string
  action_taken: string
  client_ip: string
  request_id?: number | null
  masked_prompt_preview?: string | null
  model?: string | null
  provider?: string | null
  latency_ms?: number | null
  total_cost?: number | null
  cache_hit?: boolean | null
}

interface GoPIIEventResponse {
  items: GoPIIEventItem[]
  total: number
  page: number
  limit: number
  total_pages: number
}

export async function getPIIEvents(params?: {
  page?: number
  per_page?: number
  blocked?: boolean
}): Promise<PaginatedResponse<PIIEvent>> {
  const queryParams = {
    page: params?.page,
    limit: params?.per_page,
    blocked: params?.blocked,
  }
  const go = await request<GoPIIEventResponse>(buildUrl('/pii-events', queryParams))
  return adaptPIIEvents(go, params?.page, params?.per_page)
}

export async function getPIIStats(): Promise<PIIStats> {
  return request<PIIStats>('/pii-stats')
}

export interface PIIConfig {
  enabled: boolean
}

export async function getPIIConfig(): Promise<PIIConfig> {
  return request<PIIConfig>('/pii/config')
}

export async function togglePII(enabled: boolean): Promise<PIIConfig> {
  return request<PIIConfig>('/pii/config', {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  })
}

export async function listPIIPatterns(): Promise<PIIPattern[]> {
  return request<PIIPattern[]>('/pii/patterns')
}

export async function createPIIPattern(data: {
  name: string
  regex: string
  enabled?: boolean
  action?: string
}): Promise<{ status: string; name: string }> {
  return request<{ status: string; name: string }>('/pii/patterns', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updatePIIPattern(
  name: string,
  data: { regex?: string; enabled?: boolean; action?: string },
): Promise<{ status: string; name: string }> {
  return request<{ status: string; name: string }>(`/pii/patterns/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deletePIIPattern(name: string): Promise<void> {
  return request<void>(`/pii/patterns/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
}

export async function reloadPIIPatterns(): Promise<{ status: string }> {
  return request<{ status: string }>('/pii/patterns/reload', {
    method: 'POST',
  })
}
