import { request } from './request'
import type { FeatureFlag } from './types'

export async function getFeatures(): Promise<FeatureFlag[]> {
  return request<FeatureFlag[]>('/features')
}

export async function toggleFeature(featureKey: string, enabled: boolean): Promise<void> {
  await request('/features/toggle', {
    method: 'POST',
    body: JSON.stringify({ feature_key: featureKey, enabled }),
  })
}
