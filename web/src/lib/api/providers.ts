import { request } from './request'
import type { ProviderInfo } from './types'

export async function getProviders(): Promise<ProviderInfo[]> {
  return (await request<ProviderInfo[] | null>('/providers')) || []
}

export async function updateProvider(
  name: string,
  baseUrl: string,
  apiKey: string | null,
  apiKeys?: string[],
): Promise<void> {
  const body: Record<string, unknown> = { name, base_url: baseUrl }
  // null = don't change the key, "" = clear the key
  if (apiKey !== null) body.api_key = apiKey
  if (apiKeys && apiKeys.length > 0) body.api_keys = apiKeys
  await request('/providers', { method: 'POST', body: JSON.stringify(body) })
}
