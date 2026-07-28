import { adaptModelProvider } from './adapters'
import { request } from './request'
import type { ModelProvider } from './types'

interface GoModelResponseItem {
  name: string
  provider: string
  type: string
  owned_by: string
  active: boolean
  configured: boolean
  display_name?: string
  tier?: string
  cost_per_input_token?: number
  cost_per_output_token?: number
}

export async function getModelProviders(): Promise<ModelProvider[]> {
  const items = await request<GoModelResponseItem[]>('/models')
  return items.map(adaptModelProvider)
}

export async function toggleModel(name: string, active: boolean): Promise<void> {
  await request('/models/toggle', {
    method: 'POST',
    body: JSON.stringify({ name, active }),
  })
}

export async function updateModelTier(name: string, tier: string): Promise<void> {
  await request('/models/tier', {
    method: 'POST',
    body: JSON.stringify({ name, tier }),
  })
}
