-- +goose Up

-- API keys: the sole key identity table.
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    hashed_key TEXT NOT NULL UNIQUE,
    salt TEXT NOT NULL DEFAULT 'sha256',
    key_prefix TEXT DEFAULT '',
    group_id INTEGER DEFAULT NULL,
    user_id INTEGER DEFAULT NULL,
    tags TEXT DEFAULT '{}',
    monthly_budget_usd REAL DEFAULT 0,
    monthly_budget_tokens INTEGER DEFAULT 0,
    rate_limit_rpm INTEGER DEFAULT 60,
    rate_limit_tpm INTEGER DEFAULT 100000,
    rate_limit_retry_after INTEGER DEFAULT 60,
    allowed_models TEXT DEFAULT '[]',
    allowed_providers TEXT DEFAULT '[]',
    enabled INTEGER NOT NULL DEFAULT 1,
    team_id TEXT DEFAULT NULL,
    org_id TEXT DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hashed_key ON api_keys(hashed_key);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_prefix ON api_keys(key_prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_group_id ON api_keys(group_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);

-- Per-key daily usage breakdown by model/provider.
CREATE TABLE IF NOT EXISTS key_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    date TEXT NOT NULL,
    tokens_in INTEGER DEFAULT 0,
    tokens_out INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0,
    request_count INTEGER DEFAULT 0,
    model TEXT DEFAULT '',
    provider TEXT DEFAULT '',
    UNIQUE(key_id, date, model, provider)
);

CREATE INDEX IF NOT EXISTS idx_key_usage_key_id ON key_usage(key_id);
CREATE INDEX IF NOT EXISTS idx_key_usage_date ON key_usage(date);

-- Proxy audit log — every chat completion request.
CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    key_id TEXT,
    model TEXT,
    provider TEXT,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    total_cost REAL,
    latency_ms INTEGER,
    status_code INTEGER,
    cache_hit BOOLEAN,
    prompt_preview TEXT,
    request_body TEXT,
    response_body TEXT,
    complexity_score REAL DEFAULT 0,
    guardrail_latency_ms REAL DEFAULT 0,
    llm_latency_ms REAL DEFAULT 0,
    queued_latency_ms REAL DEFAULT 0,
    client_ip TEXT,
    trace_id TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_key_id ON audit_log(key_id);

-- Normalized body storage for audit_log.
CREATE TABLE IF NOT EXISTS audit_body (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_log_id    INTEGER NOT NULL UNIQUE REFERENCES audit_log(id) ON DELETE CASCADE,
    request_body    TEXT,
    response_body   TEXT,
    prompt_preview  TEXT,
    compressed      INTEGER NOT NULL DEFAULT 0
);

-- Daily aggregated usage per key/model/provider.
CREATE TABLE IF NOT EXISTS usage_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_id TEXT,
    date TEXT,
    model TEXT DEFAULT '',
    provider TEXT DEFAULT '',
    tokens INTEGER DEFAULT 0,
    cost REAL DEFAULT 0,
    request_count INTEGER DEFAULT 0,
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    cache_hits INTEGER DEFAULT 0,
    UNIQUE(key_id, date, model, provider)
);

-- Loop detection events.
CREATE TABLE IF NOT EXISTS loop_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    key_id TEXT,
    client_ip TEXT,
    prompt_hash TEXT,
    repeat_count INTEGER,
    window_seconds INTEGER,
    action_taken TEXT,
    resolved_at DATETIME
);

-- PII masking events.
CREATE TABLE IF NOT EXISTS pii_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    key_id TEXT,
    request_id INTEGER,
    pii_type TEXT,
    action_taken TEXT,
    masked_prompt_preview TEXT,
    client_ip TEXT,
    pii_value TEXT
);

-- Unified guardrail violation events.
CREATE TABLE IF NOT EXISTS guardrail_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    key_id TEXT,
    guardrail_type TEXT NOT NULL,
    action_taken TEXT NOT NULL,
    model TEXT,
    provider TEXT,
    details TEXT,
    request_id INTEGER
);

CREATE INDEX IF NOT EXISTS idx_guardrail_events_timestamp ON guardrail_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_guardrail_events_type ON guardrail_events(guardrail_type);
CREATE INDEX IF NOT EXISTS idx_guardrail_events_key_id ON guardrail_events(key_id);

-- MCP (Model Context Protocol) gateway tables.
CREATE TABLE IF NOT EXISTS mcp_servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    transport TEXT NOT NULL DEFAULT 'sse',
    url TEXT DEFAULT '',
    command TEXT DEFAULT '',
    args TEXT DEFAULT '[]',
    env TEXT DEFAULT '{}',
    handler TEXT DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    timeout TEXT DEFAULT '30s',
    timeout_ms INTEGER DEFAULT 30000,
    max_retries INTEGER NOT NULL DEFAULT 3,
    auth_type TEXT DEFAULT '',
    auth_key_env TEXT DEFAULT '',
    config JSON DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mcp_tools (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    schema JSON DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(server_id, name)
);

-- Per-server access grants with allow/deny effect and global '*' sentinel.
CREATE TABLE IF NOT EXISTS mcp_grant (
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK(subject_type IN ('key','user','group')),
    subject_id TEXT NOT NULL,
    server_id TEXT NOT NULL DEFAULT '*',
    tools TEXT NOT NULL DEFAULT '*',
    effect TEXT NOT NULL DEFAULT 'allow' CHECK(effect IN ('allow','deny')),
    enabled INTEGER NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(subject_type, subject_id, server_id, tools, effect)
);

CREATE INDEX IF NOT EXISTS idx_mcp_grant_subject ON mcp_grant(subject_type, subject_id);

CREATE TABLE IF NOT EXISTS mcp_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_id TEXT,
    tool TEXT NOT NULL DEFAULT '',
    server_id TEXT DEFAULT '',
    method TEXT NOT NULL DEFAULT 'tools/call',
    params TEXT DEFAULT '{}',
    duration_ms REAL DEFAULT 0,
    status_code INTEGER DEFAULT 0,
    success INTEGER NOT NULL DEFAULT 1,
    error_msg TEXT DEFAULT '',
    client_ip TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_created_at ON mcp_audit_log(created_at);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_tool ON mcp_audit_log(tool);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_server_id ON mcp_audit_log(server_id);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_key_id ON mcp_audit_log(key_id);

-- Routing rules for smart router persistence.
CREATE TABLE IF NOT EXISTS routing_rules (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,
    condition     TEXT NOT NULL,
    target_model  TEXT NOT NULL,
    priority      INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_routing_rules_priority ON routing_rules(priority);

-- Provider configs (name = base_url/api_key overrides).
CREATE TABLE IF NOT EXISTS provider_configs (
    name TEXT PRIMARY KEY,
    base_url TEXT,
    api_key TEXT
);

-- Model registry status.
CREATE TABLE IF NOT EXISTS model_configs (
    name TEXT PRIMARY KEY,
    active INTEGER NOT NULL DEFAULT 1
);

-- Prompt template management.
CREATE TABLE IF NOT EXISTS prompts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT DEFAULT '',
    version TEXT NOT NULL DEFAULT '0.1.0',
    content TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1,
    labels TEXT DEFAULT '[]',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_prompts_name ON prompts(name);
CREATE INDEX IF NOT EXISTS idx_prompts_is_active ON prompts(is_active);

CREATE TABLE IF NOT EXISTS prompt_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    prompt_id INTEGER NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    content TEXT NOT NULL,
    change_summary TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_prompt_versions_prompt_id ON prompt_versions(prompt_id);

-- Prompt deployment traffic-split support.
CREATE TABLE IF NOT EXISTS prompt_deployments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    prompt_id INTEGER NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    label TEXT NOT NULL,
    weight INTEGER NOT NULL DEFAULT 100,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(prompt_id, label)
);

CREATE INDEX IF NOT EXISTS idx_prompt_deployments_prompt_id ON prompt_deployments(prompt_id);
CREATE INDEX IF NOT EXISTS idx_prompt_deployments_active ON prompt_deployments(prompt_id, is_active);

-- OpenAPI specs for external tools/services.
CREATE TABLE IF NOT EXISTS openapi_specs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    spec_url    TEXT NOT NULL,
    operations  TEXT NOT NULL DEFAULT '[]',
    auth_type   TEXT NOT NULL DEFAULT 'none',
    auth_value  TEXT NOT NULL DEFAULT '',
    auth_key    TEXT NOT NULL DEFAULT '',
    timeout_ms  INTEGER NOT NULL DEFAULT 30000,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);


CREATE INDEX IF NOT EXISTS idx_openapi_specs_enabled ON openapi_specs(enabled);

-- User-defined guardrail rules with targeting.
CREATE TABLE IF NOT EXISTS guardrail_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    patterns TEXT NOT NULL DEFAULT '[]',
    mode TEXT NOT NULL DEFAULT 'block' CHECK(mode IN ('block','warn','mask')),
    severity TEXT NOT NULL DEFAULT 'medium' CHECK(severity IN ('low','medium','high','critical')),
    enabled INTEGER NOT NULL DEFAULT 1,
    type TEXT NOT NULL DEFAULT 'custom',
    target_type TEXT DEFAULT 'global',
    target_id INTEGER DEFAULT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_guardrail_rules_enabled ON guardrail_rules(enabled);
CREATE INDEX IF NOT EXISTS idx_guardrail_rules_target ON guardrail_rules(target_type, target_id);

-- Dynamically discovered provider models with metadata.
CREATE TABLE IF NOT EXISTS provider_models (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    tier TEXT NOT NULL DEFAULT 'standard',
    cost_in REAL NOT NULL DEFAULT 0,
    cost_out REAL NOT NULL DEFAULT 0,
    display_name TEXT DEFAULT '',
    max_context_tokens INTEGER DEFAULT 0,
    max_output_tokens INTEGER DEFAULT 0,
    capabilities TEXT DEFAULT '[]',
    default_base_url TEXT DEFAULT '',
    discovered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider, model)
);

CREATE INDEX IF NOT EXISTS idx_provider_models_provider ON provider_models(provider);
CREATE INDEX IF NOT EXISTS idx_provider_models_active ON provider_models(active);
CREATE INDEX IF NOT EXISTS idx_provider_models_model ON provider_models(model);

-- Users and groups for targeting and budgets.
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active',
    password_hash TEXT DEFAULT '',
    metadata TEXT DEFAULT '{}',
    budget REAL DEFAULT 0,
    daily_limit REAL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);

CREATE TABLE IF NOT EXISTS groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT DEFAULT '',
    metadata TEXT DEFAULT '{}',
    budget REAL DEFAULT 0,
    daily_limit REAL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_groups_name ON groups(name);

-- Many-to-many user-group membership.
CREATE TABLE IF NOT EXISTS user_group_memberships (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_group_id ON user_group_memberships(group_id);
CREATE INDEX IF NOT EXISTS idx_memberships_user_id ON user_group_memberships(user_id);

-- PII regex patterns table.
CREATE TABLE IF NOT EXISTS pii_patterns (
    name       TEXT PRIMARY KEY,
    regex      TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    action     TEXT    NOT NULL DEFAULT 'mask',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Config audit log — tracks mutations on configuration entities.
CREATE TABLE IF NOT EXISTS config_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('create','update','delete')),
    old_values TEXT,
    new_values TEXT,
    performed_by TEXT,
    performed_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_config_audit_entity
    ON config_audit_log(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_config_audit_performed_at
    ON config_audit_log(performed_at);
CREATE INDEX IF NOT EXISTS idx_config_audit_action
    ON config_audit_log(action);

-- Triggers table for multi-trigger job system (cron + webhook).
CREATE TABLE IF NOT EXISTS triggers (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('cron', 'webhook')),
    enabled INTEGER NOT NULL DEFAULT 1,
    config TEXT NOT NULL DEFAULT '{}',
    token TEXT,
    secret_hash TEXT,
    last_used_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_triggers_token ON triggers(token);
CREATE INDEX IF NOT EXISTS idx_triggers_job_id ON triggers(job_id);

-- Jobs table — replaces cron_jobs (V2).
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    steps TEXT NOT NULL DEFAULT '',
    variables_config TEXT DEFAULT '{}',
    delivery_config TEXT DEFAULT '{}',
    timeout_ms INTEGER DEFAULT 120000,
    enabled INTEGER NOT NULL DEFAULT 1,
    api_key_id TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Job runs — replaces cron_job_executions (V2).
-- Includes attempts (V15), next_retry_at and last_error (V16).
CREATE TABLE IF NOT EXISTS job_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    trigger_id TEXT DEFAULT '',
    idempotency_key TEXT DEFAULT '',
    execution_key TEXT DEFAULT '',
    llm_result TEXT DEFAULT '',
    llm_error TEXT DEFAULT '',
    delivery_result TEXT DEFAULT '',
    delivery_error TEXT DEFAULT '',
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0,
    request_body TEXT DEFAULT '',
    steps TEXT DEFAULT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at DATETIME,
    last_error TEXT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    duration_ms INTEGER DEFAULT 0,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_job_runs_job_id ON job_runs(job_id);
CREATE INDEX IF NOT EXISTS idx_job_runs_status ON job_runs(status);
CREATE INDEX IF NOT EXISTS idx_job_runs_started_at ON job_runs(started_at);

-- Job activations — deduplicates trigger firings.
CREATE TABLE IF NOT EXISTS job_activations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trigger_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    idem_key TEXT NOT NULL,
    payload BLOB,
    status TEXT NOT NULL DEFAULT 'pending',
    run_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(trigger_id, idem_key)
);

CREATE INDEX IF NOT EXISTS idx_job_activations_created_at
    ON job_activations(created_at);

-- Runtime config key-value store — used by the runtime configuration system
-- (internal/store) for feature flags, routing strategies, provider configs,
-- guardrail rules, MCP servers, and OpenAPI tools.
CREATE TABLE IF NOT EXISTS runtime_config (
  section TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_by TEXT,
  PRIMARY KEY (section, key)
);

-- Runtime migration tracking — independent checksum-verified migration system.
CREATE TABLE IF NOT EXISTS runtime_schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT (datetime('now')),
  checksum TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS orgs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_mcp_grant_server_delete
AFTER DELETE ON mcp_servers
BEGIN
    DELETE FROM mcp_grant WHERE server_id = OLD.id;
END;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS config_settings (
    scope TEXT NOT NULL,           -- 'key' | 'team' | 'org' | 'global'
    scope_id TEXT NOT NULL,        -- team_id, org_id, key_id, etc.
    field TEXT NOT NULL,
    value TEXT NOT NULL,           -- JSON value
    PRIMARY KEY (scope, scope_id, field)
);

CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT 'New Chat',
    key_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    model TEXT,
    token_count INTEGER DEFAULT 0,
    cost REAL DEFAULT 0,
    reasoning_content TEXT,
    tool_calls TEXT,
    usage_cost REAL DEFAULT 0,
    billing_key TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON messages(conversation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);

CREATE TABLE IF NOT EXISTS oauth_requests (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    code_challenge TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_oauth_requests_created_at ON oauth_requests(created_at);

CREATE TABLE IF NOT EXISTS oauth_codes (
    id TEXT PRIMARY KEY,
    api_key TEXT NOT NULL,
    redirect_uri TEXT NOT NULL DEFAULT '',
    code_challenge TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT '',
    expires_at DATETIME NOT NULL,
    used INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires_at ON oauth_codes(expires_at);
