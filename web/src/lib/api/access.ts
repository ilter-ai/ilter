import { buildUrl } from './queryBuilder'
import { request } from './request'
import { extractArray } from './responseHelpers'
import type { ConfigAuditEntry, CreateGrantInput, McpGrant, UpdateGrantInput } from './types'

export async function listGrants(): Promise<McpGrant[]> {
  const res = await request<{ grants: McpGrant[] }>('/access/mcp')
  return extractArray(res, 'grants')
}

export async function getGrant(id: string): Promise<McpGrant> {
  return request<McpGrant>(`/access/mcp/${encodeURIComponent(id)}`)
}

export async function createGrant(data: CreateGrantInput): Promise<{ id: string }> {
  return request<{ id: string }>('/access/mcp', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateGrant(id: string, data: UpdateGrantInput): Promise<McpGrant> {
  return request<McpGrant>(`/access/mcp/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteGrant(id: string): Promise<void> {
  await request(`/access/mcp/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function toggleGrant(id: string): Promise<McpGrant> {
  return request<McpGrant>(`/access/mcp/${encodeURIComponent(id)}/toggle`, {
    method: 'PATCH',
  })
}

export async function getDefaultPolicy(): Promise<string> {
  const res = await request<{ default_policy: string }>('/access/mcp/default-policy')
  return res.default_policy
}

export async function setDefaultPolicy(policy: 'allow' | 'deny'): Promise<void> {
  await request('/access/mcp/default-policy', {
    method: 'PUT',
    body: JSON.stringify({ default_policy: policy }),
  })
}

export async function listGrantsByServer(serverId: string): Promise<McpGrant[]> {
  const res = await request<{ grants: McpGrant[] }>(`/access/mcp/server/${encodeURIComponent(serverId)}`)
  return extractArray(res, 'grants')
}

export async function listConfigAuditLog(params: {
  page?: number
  limit?: number
  entity_type?: string
  action?: string
  start?: string
  end?: string
}): Promise<{ items: ConfigAuditEntry[]; total: number; page: number; limit: number }> {
  return request(buildUrl('/access/audit', params))
}
