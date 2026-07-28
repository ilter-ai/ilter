import { request } from './request'
import type { OpenAPISpec } from './types'

export async function getOpenAPISpecs(): Promise<OpenAPISpec[]> {
  const res = await request<{ specs: OpenAPISpec[]; total: number }>('/openapi-specs')
  const specs = res.specs || []
  // operations field comes from SQLite TEXT as a JSON string,
  // parse it into an array so consumers can .map() on it directly
  for (const s of specs) {
    if (typeof s.operations === 'string') {
      try {
        s.operations = JSON.parse(s.operations)
      } catch {
        s.operations = []
      }
    }
  }
  return specs
}

export async function createOpenAPISpec(data: {
  name: string
  description?: string
  spec_url: string
  operations?: string
  auth_type?: string
  auth_value?: string
  auth_key?: string
  timeout_ms?: number
  enabled?: boolean
}): Promise<{ id: string }> {
  return request<{ id: string }>('/openapi-specs', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateOpenAPISpec(
  id: string,
  data: {
    name?: string
    description?: string
    spec_url?: string
    operations?: string
    auth_type?: string
    auth_value?: string
    auth_key?: string
    timeout_ms?: number
    enabled?: boolean
  },
): Promise<void> {
  await request(`/openapi-specs/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ id, ...data }),
  })
}

export async function toggleOpenAPISpec(id: string): Promise<{ enabled: boolean }> {
  return request<{ enabled: boolean }>(`/openapi-specs/${encodeURIComponent(id)}/toggle`, { method: 'PATCH' })
}

export async function deleteOpenAPISpec(id: string): Promise<void> {
  await request(`/openapi-specs/${id}`, { method: 'DELETE' })
}

export async function validateOpenAPISpec(
  id: string,
): Promise<{ status: string; operations_count?: number; error?: string }> {
  return request(`/openapi-specs/${encodeURIComponent(id)}/validate`, { method: 'POST' })
}
