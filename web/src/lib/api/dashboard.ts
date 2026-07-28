import { adaptDashboardStats } from './adapters'
import { request } from './request'
import type { DashboardStats } from './types'

interface GoStatsResponse {
  total_requests: number
  total_cost: number
  estimated_savings: number
  success_count: number
  error_count: number
  cache_hits: number
  avg_latency_ms: number
  total_tokens: number
  active_keys_used: number
  active_keys: number
  total_keys: number
  blocked_requests_24h: number
  daily_stats: {
    date: string
    requests: number
    tokens_in: number
    tokens_out: number
    cost: number
  }[]
  provider_breakdown: {
    provider: string
    requests: number
    tokens: number
    cost: number
    pct: number
  }[]
  system_health: {
    name: string
    status: string
    value: string
    metric: string
  }[]
}

export async function getDashboardStats(): Promise<DashboardStats> {
  const go = await request<GoStatsResponse>('/stats')
  return adaptDashboardStats(go)
}
