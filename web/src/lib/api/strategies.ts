import { request } from './request'
import type { ActiveStrategyResponse, RoutingStrategy, StrategyListResponse } from './types'

export async function fetchStrategies(): Promise<StrategyListResponse> {
  return request<StrategyListResponse>('/smart-router/strategies')
}

export async function fetchStrategy(name: string): Promise<RoutingStrategy> {
  return request<RoutingStrategy>(`/smart-router/strategies/${encodeURIComponent(name)}`)
}

export async function saveStrategy(name: string, strategy: RoutingStrategy): Promise<{ status: string; name: string }> {
  return request<{ status: string; name: string }>(`/smart-router/strategies/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify(strategy),
  })
}

export async function deleteStrategy(name: string): Promise<void> {
  await request(`/smart-router/strategies/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

export async function fetchActiveStrategy(): Promise<ActiveStrategyResponse> {
  return request<ActiveStrategyResponse>('/smart-router/active')
}

export async function setActiveStrategy(name: string): Promise<{ status: string; active: string }> {
  return request<{ status: string; active: string }>('/smart-router/active', {
    method: 'PUT',
    body: JSON.stringify({ name }),
  })
}
