import { request } from './request'
import type { CircuitBreakerSummary } from './types'

export async function getCircuitBreakerSummary(): Promise<CircuitBreakerSummary> {
  return request<CircuitBreakerSummary>('/circuit-breaker/summary')
}

export async function toggleCircuitBreaker(enabled: boolean, reason?: string): Promise<void> {
  await request('/circuit-breaker/toggle', {
    method: 'POST',
    body: JSON.stringify({ enabled, reason }),
  })
}

export async function resetAllCircuits(reason?: string): Promise<void> {
  await request('/circuit-breaker/reset', {
    method: 'POST',
    body: JSON.stringify({ reason }),
  })
}

export async function forceOpenAllCircuits(reason?: string): Promise<void> {
  await request('/circuit-breaker/force-open', {
    method: 'POST',
    body: JSON.stringify({ reason }),
  })
}
