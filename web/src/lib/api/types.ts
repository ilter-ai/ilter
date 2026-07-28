export interface KeyInfo {
  id: number
  key_prefix?: string
  key_name?: string
  owner_type?: string
  owner_id?: number
  owner_name?: string
}

export interface BudgetKeyItem {
  id: number
  key_prefix: string
  key_name: string
  monthly_budget_usd: number
  monthly_budget_tokens: number
  monthly_spent: number
  daily_spent: number
  daily_limit: number
  status: 'ok' | 'warning' | 'critical' | 'depleted'
  key?: KeyInfo
}

export interface BudgetChartPoint {
  day: string
  spend: number
  daily_limit: number
}

export interface BudgetSummary {
  enabled: boolean
  default_monthly_limit: number
  default_daily_limit: number
  alert_threshold: number
  total_budget: number
  total_spent: number
  keys: BudgetKeyItem[]
  chart: BudgetChartPoint[]
}

export interface UserBudgetItem {
  user_id: number
  user_name: string
  monthly_budget: number
  monthly_spent: number
  daily_limit: number
  daily_spent: number
  status: 'ok' | 'warning' | 'critical' | 'depleted'
}

export interface GroupBudgetItem {
  group_id: number
  group_name: string
  monthly_budget: number
  monthly_spent: number
  daily_limit: number
  daily_spent: number
  status: 'ok' | 'warning' | 'critical' | 'depleted'
}

export interface ApiError {
  status: number
  message: string
  details?: unknown
}

export interface PaginatedResponse<T> {
  data: T[]
  total: number
  page: number
  per_page: number
  total_pages: number
}

export interface CostSummary {
  total_cost: number
  total_requests: number
  avg_cost_per_request: number
  daily_costs: { date: string; cost: number }[]
  model_breakdown: { model: string; cost: number; calls: number }[]
  provider_breakdown: { provider: string; cost: number; calls: number }[]
  savings_summary?: { routing_savings: number; cache_savings: number; total_savings: number }
}

export interface APIKey {
  id: string
  name: string
  tags: Record<string, string>
  group_id?: number
  user_id?: number
  rate_limit_rpm: number
  rate_limit_tpm: number
  allowed_models: string[]
  allowed_providers: string[]
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface APIKeyUsage {
  date: string
  model: string
  provider: string
  tokens_in: number
  tokens_out: number
  cost_usd: number
  request_count: number
}

export interface APIKeySummary {
  total_keys: number
  enabled_keys: number
  total_requests: number
  total_cost_usd: number
  total_tokens_in: number
  total_tokens_out: number
}

export interface PIIEvent {
  id: string
  timestamp: string
  api_key_id?: number
  key?: KeyInfo
  mode: string
  entity_type: string
  matched_value_preview: string
  value?: string
  blocked: boolean
}

export interface LoopSettingsConfig {
  rate_threshold: number
  fingerprint_window: number
  fingerprint_duplicates: number
  cost_window: string // duration like "5m"
  cost_threshold: number
  session_max_requests: number
  output_loop_mode: string // "off" | "observe" | "enforce"
  output_loop_threshold: number
  output_min_sentence_len: number
}

export interface LoopEvent {
  id: string
  timestamp: string
  api_key_id?: number
  key?: KeyInfo
  fingerprint: string
  action_taken: string
  repeat_count: number
  window_seconds: number
}

export interface GuardrailRule {
  id: string
  name: string
  type: string
  enabled: boolean
  priority: number
  target_type?: string
  target_id?: number
  key?: KeyInfo
}

export interface ModelProvider {
  id: string
  name: string
  provider: string
  model: string
  is_active: boolean
  tier?: string
  cost_per_1k_in: number
  cost_per_1k_out: number
}

export interface ProviderInfo {
  name: string
  type: string
  base_url: string
  models: { name: string; active: boolean; tier?: string }[]
  active_models: number
  total_models: number
  status: string
  circuit_breaker_state: string
  api_key_set: boolean
  api_key_source: string
}

export interface PromptTemplate {
  id: string
  name: string
  content: string
  variables: string[]
  labels: string[]
  description: string
  version: string
  created_at: string
  updated_at: string
}

export interface MCPServer {
  id: string
  name: string
  description?: string
  transport?: string
  url: string
  command?: string
  args?: string
  env?: string
  handler?: string
  enabled?: boolean
  status: 'online' | 'offline' | 'error'
  timeout_ms?: number
  max_retries?: number
  auth_type?: string
  auth_key_env?: string
  tools_count: number
  last_health_check: string
  created_at?: string
  updated_at?: string
}

export interface OpenAPISpec {
  id: string
  name: string
  description?: string
  spec_url: string
  operations: string[]
  auth_type: string
  auth_value: string
  auth_key: string
  timeout_ms: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface DashboardStats {
  total_requests_24h: number
  total_cost_24h: number
  active_keys: number
  avg_latency_ms: number
  error_rate_pct: number
  blocked_requests_24h: number
}

export interface RateLimitKeyItem {
  id: string
  key_prefix: string
  key_name: string
  rpm_limit: number
  retry_after: number
  current_rpm: number
  blocked_24h: number
  status: 'ok' | 'warning' | 'critical'
  key?: KeyInfo
}

export interface RateLimitChartPoint {
  time: string
  requests: number
  limit: number
}

export interface RateLimitSummary {
  enabled: boolean
  default_rpm: number
  redis_ready: boolean
  total_requests_24h: number
  rate_limited_24h: number
  active_keys: number
  avg_rpm: number
  limit_rpm: number
  keys: RateLimitKeyItem[]
  chart: RateLimitChartPoint[]
}

export interface UserRateLimit {
  user_id: number
  user_name: string
  rpm_limit: number
  retry_after: number
  current_rpm: number
  blocked_24h: number
  status: 'ok' | 'warning' | 'critical'
}

export interface GroupRateLimit {
  group_id: number
  group_name: string
  rpm_limit: number
  retry_after: number
  current_rpm: number
  blocked_24h: number
  status: 'ok' | 'warning' | 'critical'
}

export interface CircuitBreakerSummary {
  summary: {
    total_circuits: number
    closed_count: number
    open_count: number
    half_open_count: number
    total_failures_24h: number
    enabled: boolean
  }
  circuits: {
    provider: string
    state: string
    requests: number
    successes: number
    failures: number
    consecutive_successes: number
    consecutive_failures: number
  }[]
}

export interface TopCachedQuery {
  query_preview: string
  model: string
  hit_count: number
  last_accessed: string
  avg_latency: number
}

export interface CacheHourlyPoint {
  time: string
  hits: number
  misses: number
}

export interface FeatureFlag {
  feature_key: string
  enabled: boolean
  warning?: string
}

export interface SemanticCacheSummary {
  cache_hits_24h: number
  cache_misses_24h: number
  hit_rate_pct: number
  cache_size_entries: number
  cache_size_mb: number
  avg_latency_saved_ms: number
  redis_connected: boolean
  redis_error?: string
  mode: string
  similarity_threshold: number
  ttl_seconds: number
  top_queries: TopCachedQuery[]
  hourly_data: CacheHourlyPoint[]
}

export interface UsageRecord {
  api_key_id: number
  prompt_tokens: number
  completion_tokens: number
  total_cost: number
}

export interface McpStats {
  servers: {
    total: number
    enabled: number
  }
  tools: number
  access_rules: number
  usage: {
    total_calls: number
    error_count: number
    avg_duration_ms: number
    calls_by_tool_24h: { tool: string; count: number }[]
  }
}

export interface McpGrant {
  id: string
  subject_type: 'key' | 'user' | 'group'
  subject_id: string
  server_id: string
  server_name?: string
  tools: string
  effect: 'allow' | 'deny'
  enabled: boolean
  priority: number
  created_at: string
}

export interface CreateGrantInput {
  subject_type: 'key' | 'user' | 'group'
  subject_id: string
  server_id: string
  tools?: string
  effect?: 'allow' | 'deny'
  enabled?: boolean
  priority?: number
}

export interface UpdateGrantInput {
  subject_type?: 'key' | 'user' | 'group'
  subject_id?: string
  tools?: string
  effect?: 'allow' | 'deny'
  enabled?: boolean
  priority?: number
}

export interface ConfigAuditEntry {
  id: number
  entity_type: string
  entity_id: string
  action: 'create' | 'update' | 'delete'
  old_values?: string
  new_values?: string
  performed_by?: string
  performed_at: string
}

export interface BatchGrantOp {
  id: string
  action: 'enable' | 'disable' | 'delete'
}

export interface MCPToolDefinition {
  id: string
  name: string
  description: string
  input_schema: string // JSON string
  created_at: string
}

export interface MCPToolContent {
  type: string
  text?: string
  data?: string
  mimeType?: string
  uri?: string
}

export interface MCPToolCallResult {
  isError: boolean
  content: MCPToolContent[]
}

export interface McpAuditEntry {
  id: string
  api_key_id?: number
  key?: KeyInfo
  server_id: string
  tool: string
  method: string
  params?: string
  duration_ms: number
  success: boolean
  status_code: number
  error_msg?: string
  created_at: string
}

export interface GuardrailsTestResult {
  blocked: boolean
  warned: boolean
  rule_id: string
  rule_set: string
  severity: string
  matched: string
  action: string
}

export interface GuardrailsStatsInfo {
  enabled: boolean
  rule_count: number
  rule_sets: string[]
}

export interface GuardrailEventItem {
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

export interface GuardrailViolationsResponse {
  items: GuardrailEventItem[]
  total: number
  page: number
  limit: number
}

export interface GuardrailSummaryItem {
  guardrail_type: string
  count: number
  pct: number
}

export interface GuardrailTrendItem {
  date: string
  count: number
}

export interface GuardrailSummaryResponse {
  total_events: number
  period: string
  by_type: GuardrailSummaryItem[]
  recent_trend?: GuardrailTrendItem[]
}

// ── Groups ──
export interface Group {
  id: number
  name: string
  description: string
  metadata?: string
  created_at: string
  updated_at: string
}

export interface CreateGroupRequest {
  name: string
  description?: string
}

export interface UpdateGroupRequest {
  name?: string
  description?: string
}

// ── Group Members ──
// The backend returns User objects from the members endpoint
export interface GroupMember {
  id: number
  user_id: number
  name: string
  email: string
  created_at: string
  status?: string
}

export interface User {
  id: number
  name: string
  email: string
  status: string
  metadata?: string
  budget?: number
  daily_limit?: number
  created_at: string
  updated_at: string
}

export interface CreateUserRequest {
  name: string
  email: string
  password: string
  status?: string
  budget?: number
}

export interface PIIPattern {
  name: string
  regex: string
  enabled: boolean
  action: string
}

export interface PIIStats {
  total_events: number
  blocked_count: number
  masked_count: number
  blocked_rate: number
  type_breakdown: { pii_type: string; count: number }[]
  top_keys: { api_key_id: number; api_key_name: string; count: number }[]
  recent_trend: { date: string; blocked: number; masked: number }[]
}

// ── Cost Attribution ──

export interface CostByKeyItem {
  key_id: string
  api_key_name: string
  cost: number
  count: number
  pct: number
}

export interface CostByKeyResponse {
  period: string
  by_key: CostByKeyItem[]
}

// ── Insights ──

export interface TopExpensiveItem {
  id: number
  model: string
  provider: string
  prompt_tokens: number
  completion_tokens: number
  total_cost: number
  complexity_score: number
  timestamp: string
}

export interface CostTrendItem {
  date: string
  total_cost: number
  prompt_tokens: number
  completion_tokens: number
  request_count: number
}

export interface ModelCostItem {
  model: string
  total_cost: number
  prompt_tokens: number
  completion_tokens: number
  request_count: number
}

export interface SavingsItem {
  model: string
  actual_cost: number
  cheapest_alternative_cost: number
  savings: number
  request_count: number
  recommended_model: string
}

export interface SavingsResponse {
  total_savings_potential: number
  actual_total_cost: number
  savings_rate: number
  request_count: number
  opportunities: SavingsItem[]
}

export interface UpdateUserRequest {
  name?: string
  email?: string
  password?: string
  status?: string
  budget?: number
  daily_limit?: number
}

// ── Jobs ──

export interface StepProgress {
  index: number
  type: 'llm' | 'mcp'
  model?: string
  tool?: string
  status: 'pending' | 'running' | 'done' | 'failed'
  output?: string
  error?: string
  tokens_in?: number
  tokens_out?: number
  cost?: number
}

export interface Job {
  id: string
  name: string
  description?: string
  cron_expr?: string
  variables_config: Record<string, unknown> | null
  delivery_config: string
  steps: string
  timeout_ms: number
  enabled: boolean
  api_key_id?: string
  triggers?: Trigger[]
  created_at: string
  updated_at: string
  last_exec_status?: string
  last_exec_started_at?: string
  next_run?: string
}

export interface JobStep {
  type: 'mcp' | 'llm'
  tool?: string
  arguments?: string
  prompt_id?: number
  model?: string
}

export interface ExecutionRequest {
  rendered_prompt: string
  model: string
  variables?: Record<string, unknown>
}

export interface JobRun {
  id: string
  job_id: string
  trigger_id: string
  run_id: string
  status: string
  llm_result?: string
  llm_error?: string
  delivery_result?: string
  delivery_error?: string
  prompt_tokens: number
  completion_tokens: number
  cost_usd: number
  started_at: string
  completed_at?: string
  duration_ms: number
  request_body?: ExecutionRequest | null
  attempts?: number
  next_retry_at?: string
  last_error?: string
  steps?: StepProgress[] | null
}

export interface JobStats {
  jobs: { total: number; enabled: number }
  executions: { total: number; errors: number }
  recent: JobRun[]
}

export interface Trigger {
  id: string
  job_id: string
  kind: 'cron' | 'webhook' | 'http' | 'event'
  enabled: boolean
  config: string // JSON — {expr, timezone} for cron or {provider} for webhook
  token?: string // only for webhook kind, only present right after creation
  secret?: string // HMAC signing key, only present right after creation
  created_at: string
  updated_at: string
}

export interface TriggerInput {
  id?: string // present to keep/update an existing trigger; omitted to create a new one
  kind: 'cron' | 'webhook'
  config: string // JSON string
}

// ── Routing Strategies ──

export interface RoutingStrategy {
  name: string
  description: string
  enabled: boolean
  provider_preference: 'cheapest' | 'round-robin'
  load_balancer_strategy: 'weighted-random' | 'round-robin' | 'cost-optimized' | 'latency-optimized' | 'priority-based'
  scorer: ScorerConfig
  complexity_thresholds: ComplexityThresholds
  rules: RoutingRule[]
}

export interface RoutingRule {
  name: string
  condition: string
  target_model: string
  priority: number
  enabled: boolean
}

export interface ComplexityThresholds {
  economy: number
  standard: number
}

export interface ScorerConfig {
  type: 'heuristic' | 'llm' | 'embedding' | 'trainable'
  llm?: LLMScorerConfig | null
  embedding?: EmbeddingScorerConfig | null
  trainable?: TrainableScorerConfig | null
}

export interface LLMScorerConfig {
  model: string
  provider: string
  cache_ttl: string
  cache_max_entries: number
  timeout: string
}

export interface EmbeddingScorerConfig {
  model: string
  dimensions: number
  reference_count: number
  similarity_threshold: number
}

export interface TrainableScorerConfig {
  model_path: string
  feature_version: number
  fallback_on_error: boolean
}

export interface StrategyListResponse {
  data: RoutingStrategy[]
  active: string
}

export interface ActiveStrategyResponse {
  active: string
}

export interface SetActiveRequest {
  name: string
}

export interface ChatThread {
  id: string
  title: string
  last_message_preview?: string
  message_count?: number
  created_at: string
  updated_at: string
}

export interface ChatMessage {
  id: number
  conversation_id: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  model?: string
  token_count?: number
  cost?: number
  reasoning_content?: string
  tool_calls?: string
  usage_cost?: number
  billing_key?: string
  created_at: string
}
