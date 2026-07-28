import { buildUrl } from './queryBuilder'
import { request } from './request'
import type { APIKey, APIKeySummary, APIKeyUsage } from './types'

export async function getAPIKeys(): Promise<APIKey[]> {
  const go = await request<{ api_keys: APIKey[] }>('/api-keys')
  return go.api_keys || []
}

export async function getAPIKey(id: string): Promise<APIKey> {
  return request<APIKey>(`/api-keys/${id}`)
}

export async function createAPIKey(data: {
  name: string
  group_id?: number
  user_id?: number
  rate_limit_rpm?: number
  rate_limit_tpm?: number
  allowed_models?: string[]
  allowed_providers?: string[]
  tags?: Record<string, string>
}): Promise<{ id: string; name: string; key: string }> {
  return request('/api-keys', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateAPIKey(
  id: string,
  data: Partial<Omit<APIKey, 'group_id' | 'user_id'>> & { group_id?: number | null; user_id?: number | null },
): Promise<void> {
  await request(`/api-keys/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteAPIKey(id: string): Promise<void> {
  await request(`/api-keys/${id}`, { method: 'DELETE' })
}

export async function getAPIKeyUsage(
  id: string,
  from?: string,
  to?: string,
): Promise<{ key_id: string; from: string; to: string; items: APIKeyUsage[] }> {
  return request(buildUrl(`/api-keys/${id}/usage`, { from, to }))
}

export async function getAPIKeysSummary(): Promise<APIKeySummary> {
  return request<APIKeySummary>('/api-keys/summary')
}
