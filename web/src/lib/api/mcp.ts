import { buildUrl } from './queryBuilder'
import { request } from './request'
import { extractArray } from './responseHelpers'
import type { MCPServer, MCPToolCallResult, MCPToolDefinition, McpAuditEntry, McpGrant, McpStats } from './types'

export async function getMCPServers(): Promise<MCPServer[]> {
  const res = await request<{ servers: MCPServer[]; total: number }>('/mcp-servers')
  return extractArray(res, 'servers')
}

export async function createMCPServer(data: {
  name: string
  url: string
  description?: string
  transport?: string
  command?: string
  args?: string
  env?: string
  handler?: string
  enabled?: boolean
  timeout_ms?: number
  max_retries?: number
  auth_type?: string
  auth_key_env?: string
}): Promise<{ id: string }> {
  return request<{ id: string }>('/mcp-servers', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateMCPServer(
  id: string,
  data: {
    name?: string
    url?: string
    description?: string
    transport?: string
    command?: string
    args?: string
    env?: string
    handler?: string
    enabled?: boolean
    timeout_ms?: number
    max_retries?: number
    auth_type?: string
    auth_key_env?: string
  },
): Promise<void> {
  await request(`/mcp-servers/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ id, ...data }),
  })
}

export async function toggleMCPServer(id: string): Promise<{ enabled: boolean }> {
  return request<{ enabled: boolean }>(`/mcp-servers/${id}/toggle`, { method: 'PATCH' })
}

export async function deleteMCPServer(id: string): Promise<void> {
  await request(`/mcp-servers/${id}`, { method: 'DELETE' })
}

export async function testMCPServer(
  id: string,
): Promise<{ status: string; tools_count: number; error?: string; stderr?: string; oauth_url?: string }> {
  return request<{ status: string; tools_count: number; error?: string; stderr?: string }>(`/mcp-servers/${id}/test`, {
    method: 'POST',
  })
}

export async function getMcpStats(): Promise<McpStats> {
  return request<McpStats>('/mcp/stats')
}

export async function getMcpAuditLog(params?: {
  limit?: number
  offset?: number
  tool?: string
  server_id?: string
  method?: string
  from?: string
  to?: string
  source?: 'mcp' | 'openapi'
}): Promise<{ items: McpAuditEntry[]; total: number; limit: number; offset: number }> {
  return request<{ items: McpAuditEntry[]; total: number; limit: number; offset: number }>(
    buildUrl('/mcp/audit', params),
  )
}

export async function listAllGrants(): Promise<McpGrant[]> {
  const res = await request<{ grants: McpGrant[] }>('/mcp/grants')
  return extractArray(res, 'grants')
}

export async function listServerGrants(serverId: string): Promise<McpGrant[]> {
  const res = await request<{ grants: McpGrant[] }>(`/mcp-servers/${serverId}/grants`)
  return extractArray(res, 'grants')
}

export async function createServerGrant(
  serverId: string,
  data: {
    subject_type: string
    subject_id: string
    tools: string
    effect?: string
  },
): Promise<{ id: string }> {
  return request<{ id: string }>(`/mcp-servers/${serverId}/grants`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function deleteServerGrant(serverId: string, grantId: string): Promise<void> {
  await request(`/mcp-servers/${serverId}/grants/${grantId}`, { method: 'DELETE' })
}

export async function getServerTools(id: string): Promise<{ server_id: string; tools: MCPToolDefinition[] }> {
  return request<{ server_id: string; tools: MCPToolDefinition[] }>(`/mcp-servers/${id}/tools`)
}

export async function callServerTool(
  serverId: string,
  name: string,
  args: Record<string, unknown>,
): Promise<MCPToolCallResult> {
  return request<MCPToolCallResult>(`/mcp-servers/${serverId}/tools/call`, {
    method: 'POST',
    body: JSON.stringify({ name, arguments: args }),
  })
}
