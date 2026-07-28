import { adaptLoopEvents } from './adapters'
import { buildUrl } from './queryBuilder'
import { request } from './request'
import type { LoopEvent, LoopSettingsConfig, PaginatedResponse } from './types'

interface GoLoopEventItem {
  id: number
  detected_at: string
  api_key_id: number
  key?: {
    id: number
    key_prefix?: string
    key_name?: string
    owner_type?: string
    owner_id?: number
    owner_name?: string
  }
  client_ip: string
  prompt_hash: string
  repeat_count: number
  window_seconds: number
  action_taken: string
  resolved_at: string | null
}

export async function getLoopEvents(params?: {
  page?: number
  per_page?: number
}): Promise<PaginatedResponse<LoopEvent>> {
  const items = await request<GoLoopEventItem[]>(buildUrl('/loops', params))
  return adaptLoopEvents(items, params?.page, params?.per_page)
}

export async function getLoopSettings(): Promise<LoopSettingsConfig> {
  return request<LoopSettingsConfig>('/loop-settings')
}

export async function updateLoopSettings(settings: Partial<LoopSettingsConfig>): Promise<LoopSettingsConfig> {
  return request<LoopSettingsConfig>('/loop-settings', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
}
