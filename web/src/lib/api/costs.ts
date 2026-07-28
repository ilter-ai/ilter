import { adaptCostSummary } from './adapters'
import { request } from './request'
import type { CostSummary } from './types'

interface GoProviderCostBreakdown {
  provider: string
  cost: number
  count: number
  pct: number
}

interface GoModelCostBreakdown {
  model: string
  cost: number
  count: number
  pct: number
}

interface GoTimeCostItem {
  period: string
  cost: number
  count: number
}

interface GoCostAttributionResponse {
  total_cost: number
  total_requests: number
  avg_cost_per_request: number
  period: string
  by_provider: GoProviderCostBreakdown[]
  by_model: GoModelCostBreakdown[]
  time_series: GoTimeCostItem[]
  savings_summary: unknown
}

export async function getCostSummary(period?: string): Promise<CostSummary> {
  const qs = period ? `?period=${encodeURIComponent(period)}` : ''
  const go = await request<GoCostAttributionResponse>(`/costs${qs}`)
  return adaptCostSummary(go)
}

export async function getCostByKey(period?: string): Promise<{
  period: string
  by_key: { key_id: string; api_key_name: string; cost: number; count: number; pct: number }[]
}> {
  const qs = period ? `?period=${encodeURIComponent(period)}` : ''
  return request(`/costs/by-key${qs}`)
}
