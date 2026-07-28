import { buildUrl } from './queryBuilder'
import { request } from './request'
import type {
  GuardrailRule,
  GuardrailSummaryResponse,
  GuardrailsStatsInfo,
  GuardrailsTestResult,
  GuardrailViolationsResponse,
} from './types'

interface GuardrailRuleRaw {
  id: string
  name: string
  type: string
  enabled: boolean
  priority: number
  target_type?: string
  target_id?: number
}

interface GuardrailsListResponse {
  rules: GuardrailRuleRaw[]
  total: number
}

export async function getGuardrailRules(): Promise<GuardrailRule[]> {
  const go = await request<GuardrailsListResponse>('/guardrails')
  return (go.rules || []).map((rule) => ({
    id: rule.id,
    name: rule.id,
    type: rule.type,
    enabled: rule.enabled,
    priority: 0,
    target_type: rule.target_type,
    target_id: rule.target_id,
  }))
}

export async function toggleGuardrail(id: string, enabled: boolean): Promise<void> {
  await request(`/guardrails/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  })
}

export async function createGuardrailRule(data: {
  name: string
  description?: string
  patterns: string[]
  mode?: string
  severity?: string
  target_type?: string
  target_id?: number
}): Promise<{ status: string; rule_id: string }> {
  return request<{ status: string; rule_id: string }>('/guardrails/rules', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateGuardrailRule(
  id: string,
  data: {
    name?: string
    description?: string
    patterns?: string[]
    mode?: string
    severity?: string
    enabled?: boolean
    target_type?: string
    target_id?: number
  },
): Promise<void> {
  await request(`/guardrails/rules/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteGuardrailRule(id: string): Promise<void> {
  await request(`/guardrails/rules/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function testGuardrails(content: string): Promise<GuardrailsTestResult> {
  return request<GuardrailsTestResult>('/guardrails/test', {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}

export async function getGuardrailsStats(): Promise<GuardrailsStatsInfo> {
  return request<GuardrailsStatsInfo>('/guardrails/stats')
}

export async function getGuardrailViolations(params?: {
  type?: string
  action?: string
  page?: number
  limit?: number
}): Promise<GuardrailViolationsResponse> {
  return request<GuardrailViolationsResponse>(buildUrl('/guardrails/violations', params))
}

export async function getGuardrailSummary(period?: string): Promise<GuardrailSummaryResponse> {
  return request<GuardrailSummaryResponse>(buildUrl('/guardrails/summary', { period }))
}
