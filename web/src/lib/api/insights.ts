import { request } from './request'
import type { CostTrendItem, ModelCostItem, SavingsResponse, TopExpensiveItem } from './types'

export async function getTopExpensive(): Promise<TopExpensiveItem[]> {
  return request<TopExpensiveItem[]>('/insights/top-expensive')
}

export async function getCostTrend(period?: string): Promise<CostTrendItem[]> {
  const qs = period ? `?period=${encodeURIComponent(period)}` : ''
  return request<CostTrendItem[]>(`/insights/cost-trend${qs}`)
}

export async function getCostByModel(): Promise<ModelCostItem[]> {
  return request<ModelCostItem[]>('/insights/cost-by-model')
}

export async function getSavingsOpportunity(): Promise<SavingsResponse> {
  return request<SavingsResponse>('/insights/savings-opportunity')
}
