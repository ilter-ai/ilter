import { request } from './request'

export interface FallbackConfigSummary {
  enabled: boolean
  cooldown_duration: string
  model_downgrade: string
  allowed_models: string[]
  max_attempts: number
  active_cooldowns: Array<{
    provider: string
    model: string
    key_id: string
    expires_at: string
  }>
}

export interface UpdateFallbackConfigRequest {
  enabled?: boolean
  cooldown_duration?: string
  model_downgrade?: string
  allowed_models?: string[]
  max_attempts?: number
}

export async function getFallbackSummary(): Promise<FallbackConfigSummary> {
  return request<FallbackConfigSummary>('/fallback').catch(() => ({
    enabled: false,
    cooldown_duration: '5m',
    model_downgrade: 'none',
    allowed_models: [],
    max_attempts: 0,
    active_cooldowns: [],
  }))
}

export async function updateFallbackConfig(data: UpdateFallbackConfigRequest): Promise<FallbackConfigSummary> {
  return request<FallbackConfigSummary>('/fallback', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function toggleFallback(enabled: boolean): Promise<void> {
  await request('/fallback/toggle', {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  })
}

export async function clearCooldown(provider: string, model: string, keyId: string): Promise<void> {
  await request('/fallback/cooldown/clear', {
    method: 'POST',
    body: JSON.stringify({ provider, model, key_id: keyId }),
  })
}
