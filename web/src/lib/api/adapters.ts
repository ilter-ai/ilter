import type {
  CostSummary,
  DashboardStats,
  GuardrailRule,
  KeyInfo,
  LoopEvent,
  ModelProvider,
  PaginatedResponse,
  PIIEvent,
  PromptTemplate,
} from './types'

// Go Backend Response Shapes

interface GoStatsResponse {
  total_requests: number
  total_cost: number
  estimated_savings: number
  success_count: number
  error_count: number
  cache_hits: number
  avg_latency_ms: number
  total_tokens: number
  active_keys_used: number
  active_keys: number
  total_keys: number
  blocked_requests_24h: number
  daily_stats: {
    date: string
    requests: number
    tokens_in: number
    tokens_out: number
    cost: number
  }[]
  provider_breakdown: {
    provider: string
    requests: number
    tokens: number
    cost: number
    pct: number
  }[]
  system_health: {
    name: string
    status: string
    value: string
    metric: string
  }[]
}

interface GoProviderCostBreakdown {
  provider: string
  cost: number
  count: number
  pct: number
}

interface GoModelCostBreakdown {
  model: string
  cost: number
  count: number
  pct: number
}

interface GoTimeCostItem {
  period: string
  cost: number
  count: number
}

interface GoCostAttributionResponse {
  total_cost: number
  total_requests: number
  avg_cost_per_request: number
  period: string
  by_provider: GoProviderCostBreakdown[]
  by_model: GoModelCostBreakdown[]
  time_series: GoTimeCostItem[]
  savings_summary: unknown
}

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

interface GoGuardrailEventItem {
  id: number
  timestamp: string
  api_key_id: number
  key?: KeyInfo
  guardrail_type: string
  action_taken: string
  model?: string
  provider?: string
  details?: string
}

interface GoPIIEventItem {
  id: number
  timestamp: string
  api_key_id: number
  key?: KeyInfo
  pii_type: string
  action_taken: string
  client_ip: string
  request_id?: number | null
  masked_prompt_preview?: string | null
  pii_value?: string | null
  model?: string | null
  provider?: string | null
  latency_ms?: number | null
  total_cost?: number | null
  cache_hit?: boolean | null
}

interface GoPIIEventResponse {
  items: GoPIIEventItem[]
  total: number
  page: number
  limit: number
  total_pages: number
}

interface GoLoopEventItem {
  id: number
  detected_at: string
  api_key_id: number
  key?: KeyInfo
  client_ip: string
  prompt_hash: string
  repeat_count: number
  window_seconds: number
  action_taken: string
  resolved_at: string | null
}

interface GoPromptTemplate {
  id: number
  name: string
  content: string
  description: string
  version: string
  is_active: boolean
  labels: string[]
  created_at: string
  updated_at: string
}

// Go→TS Adapter Functions

export function adaptDashboardStats(go: GoStatsResponse): DashboardStats {
  return {
    total_requests_24h: go.total_requests,
    total_cost_24h: go.total_cost,
    active_keys: go.active_keys ?? 0,
    avg_latency_ms: go.avg_latency_ms,
    error_rate_pct: go.total_requests > 0 ? (go.error_count / go.total_requests) * 100 : 0,
    blocked_requests_24h: go.blocked_requests_24h ?? 0,
  }
}

export function adaptCostSummary(go: GoCostAttributionResponse): CostSummary {
  return {
    total_cost: go.total_cost,
    total_requests: go.total_requests ?? 0,
    avg_cost_per_request: go.avg_cost_per_request ?? 0,
    daily_costs: (go.time_series || []).map((ts) => ({
      date: ts.period,
      cost: ts.cost,
    })),
    model_breakdown: (go.by_model || []).map((m) => ({
      model: m.model,
      cost: m.cost,
      calls: m.count,
    })),
    provider_breakdown: (go.by_provider || []).map((p) => ({
      provider: p.provider,
      cost: p.cost,
      calls: p.count,
    })),
    savings_summary: (go.savings_summary as CostSummary['savings_summary']) || undefined,
  }
}

function formatModelId(id: string): string {
  return id
    .toLowerCase()
    .replace(/[-/]?\d{6,8}$/, '')
    .replace(/(\d)-(\d)/g, '$1.$2')
    .replace(/[-/]+/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

export function adaptModelProvider(item: GoModelResponseItem): ModelProvider {
  return {
    id: `${item.provider}/${item.name}`,
    name: item.display_name?.includes(' ') ? item.display_name : formatModelId(item.name),
    provider: item.provider,
    model: item.name,
    is_active: item.active,
    tier: item.tier,
    cost_per_1k_in: item.cost_per_input_token ? item.cost_per_input_token * 1000 : 0,
    cost_per_1k_out: item.cost_per_output_token ? item.cost_per_output_token * 1000 : 0,
  }
}

export function adaptPIIEvents(go: GoPIIEventResponse, page?: number, per_page?: number): PaginatedResponse<PIIEvent> {
  const items = (go.items || []).map(
    (item): PIIEvent => ({
      id: String(item.id),
      timestamp: item.timestamp,
      api_key_id: item.api_key_id,
      key: item.key,
      mode: item.action_taken === 'blocked' ? 'block' : item.action_taken === 'masked' ? 'mask' : item.action_taken,
      entity_type: item.pii_type,
      matched_value_preview: item.masked_prompt_preview ?? '',
      value: item.pii_value ?? undefined,
      blocked: item.action_taken === 'blocked',
    }),
  )
  const p = go.page ?? page ?? 1
  const pp = go.limit ?? per_page ?? (items.length || 50)
  return {
    data: items,
    total: go.total,
    page: p,
    per_page: pp,
    total_pages: go.total_pages ?? Math.max(1, Math.ceil(go.total / pp)),
  }
}

export function adaptLoopEvents(
  items: GoLoopEventItem[],
  page?: number,
  per_page?: number,
): PaginatedResponse<LoopEvent> {
  const data = (items || []).map(
    (item): LoopEvent => ({
      id: String(item.id),
      timestamp: item.detected_at,
      api_key_id: item.api_key_id,
      key: item.key,
      fingerprint: item.prompt_hash,
      action_taken: item.action_taken,
      repeat_count: item.repeat_count,
      window_seconds: item.window_seconds,
    }),
  )
  const pp = per_page ?? (data.length || 1)
  return {
    data,
    total: data.length,
    page: page ?? 1,
    per_page: pp,
    total_pages: 1,
  }
}

export function adaptGuardrailEvent(item: GoGuardrailEventItem): GuardrailRule {
  return {
    id: String(item.id),
    name: item.guardrail_type,
    type: item.action_taken,
    enabled: true,
    priority: 0,
    key: item.key,
  }
}

function extractTemplateVariables(content: string): string[] {
  const matches = content.match(/\{\{\.?(\w+)\}\}/g)
  if (!matches) return []
  return [...new Set(matches.map((m) => m.replace(/[{}.]/g, '')))]
}

export function adaptPromptTemplate(item: GoPromptTemplate): PromptTemplate {
  return {
    id: String(item.id),
    name: item.name,
    content: item.content,
    variables: extractTemplateVariables(item.content),
    labels: item.labels || [],
    description: item.description || '',
    version: item.version || '',
    created_at: item.created_at,
    updated_at: item.updated_at,
  }
}
