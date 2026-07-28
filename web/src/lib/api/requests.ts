import { buildUrl } from './queryBuilder'
import { request } from './request'

// ── Types ──

export interface RequestSummary {
  id: number
  timestamp: string
  key_id: string
  model: string
  provider: string
  prompt_tokens: number
  completion_tokens: number
  total_cost: number
  latency_ms: number
  status_code: number
  cache_hit: boolean
  client_ip: string
  has_body: boolean
  trace_id: string | null
  prompt_preview: string
}

export interface RequestDetail extends RequestSummary {
  request_body: string | null
  response_body: string | null
  phase_latencies: {
    guardrail_latency_ms: number
    llm_latency_ms: number
    queued_latency_ms: number
  }
}

export interface Page<T> {
  items: T[]
  total: number
  page: number
  limit: number
}

// ── Query keys ──

export const requestsKeys = {
  all: ['requests'] as const,
  list: (params: Record<string, unknown>) => ['requests', 'list', params] as const,
  detail: (id: number) => ['requests', 'detail', id] as const,
  overview: ['requests', 'overview'] as const,
}

// ── API functions ──

export async function getRequests(params: {
  page?: number
  limit?: number
  status?: string
  model?: string
  provider?: string
  start?: string
  end?: string
}): Promise<Page<RequestSummary>> {
  return request<Page<RequestSummary>>(buildUrl('/requests', params))
}

export async function getRequestDetail(id: number): Promise<RequestDetail> {
  return request<RequestDetail>(`/requests/${id}`)
}

export async function getAnalyticsOverview(): Promise<{
  total_requests: number
  error_rate: number
  cost: number
  cache_hit_rate: number
}> {
  return request('/analytics/overview')
}
