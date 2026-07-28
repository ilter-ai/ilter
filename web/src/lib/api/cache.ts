import { request } from './request'
import type { SemanticCacheSummary } from './types'

export async function getSemanticCacheSummary(): Promise<SemanticCacheSummary> {
  return request<SemanticCacheSummary>('/semantic-cache/summary')
}

export async function flushCache(): Promise<{ status: string; message: string }> {
  return request<{ status: string; message: string }>('/cache/flush', {
    method: 'POST',
  })
}

export async function toggleCacheMode(enabled: boolean): Promise<{ status: string; mode: string }> {
  return request<{ status: string; mode: string }>('/cache/mode', {
    method: 'POST',
    body: JSON.stringify({ mode: enabled ? 'enabled' : 'disabled' }),
  })
}
