# ILTER Configuration Guide

> ILTER uses a **zero-configuration** approach. Compiled-in defaults work out of the box. Override any setting via `ILTER_*` environment variables. No YAML file is required.

---

## Quick Start

```bash
# Works immediately with compiled-in defaults:
./ilter serve

# Interactive setup wizard (providers, routing, feature flags):
./ilter init

# Deterministic demo data for development:
./ilter init --demo
```

---

## Configuration Architecture

ILTER separates operational boot configuration from dynamic gateway business logic using two distinct layers:

| Layer | Source | Scope & Lifecycle | Examples |
|-------|--------|------------------|----------|
| **Boot Config** | Environment variables (`ILTER_*`) + compiled-in defaults | Loaded once at startup. Requires process restart to change. | Server ports, SQLite file path, log level, metrics listen address, Redis URL |
| **Runtime Config** | SQLite `runtime_config` table | Dynamic state. Updates take effect **immediately** without process restart. | Upstream provider credentials, MCP servers, OpenAPI specs, guardrails, routing strategies, feature flags |

---

## Environment Variables (Boot Config)

All environment variables use the `ILTER_` prefix:

| Variable | Default | Description |
|----------|---------|-------------|
| `ILTER_SERVER_PORT` | `8181` | Main Proxy HTTP listen port |
| `ILTER_DASHBOARD_PORT` | `9191` | Embedded Web Dashboard UI port |
| `ILTER_METRICS_LISTEN_ADDR` | `:9192` | OpenTelemetry Prometheus `/metrics` listen address |
| `ILTER_STORAGE_PATH` | `"./data/ilter.db"` | SQLite database file path |
| `ILTER_ADMIN_API_KEY` | (auto-generated) | Admin API break-glass key. Also derives AES-256-GCM key for provider secret encryption at rest |
| `ILTER_LOG_LEVEL` | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `ILTER_REDIS_URL` | `""` (none) | Redis connection URL shared by rate limiter and semantic cache |

### Provider API Key Overrides (Boot Time)

Provider keys can be set via environment variables. When supplied, they populate initial provider settings:

```bash
ILTER_PROVIDER_OPENAI_API_KEY=sk-... \
ILTER_PROVIDER_ANTHROPIC_API_KEY=sk-ant-... \
ILTER_PROVIDER_DEEPSEEK_API_KEY=sk-... \
ILTER_PROVIDER_GEMINI_API_KEY=... \
ILTER_PROVIDER_OPENROUTER_API_KEY=sk-... \
ILTER_PROVIDER_OLLAMA_URL=http://localhost:11434 \
  ./ilter serve
```

---

## Dynamic Runtime Configuration

Runtime settings are managed in the SQLite database and updated hot in memory via an internal cache event bus (`config.Cache`). Changes take effect across all worker goroutines **without restarting the service**.

### Runtime Config Management Options

1. **Interactive CLI Setup Wizard**: `./ilter init`
2. **Web Dashboard**: Interactive UI on port `9191` (`http://localhost:9191`)
3. **Admin & REST API**: Endpoints under `/admin/*` and `/api/*` on ports `8181` and `9191`

### Catalog of Runtime Config Sections

| Section | Description | API Endpoint Path |
|---------|-------------|-------------------|
| **Providers** | Manage upstream API keys, base URLs, active models | `/api/providers` |
| **MCP Servers** | Register stdio/SSE MCP servers, tool access grants (`mcp_grant`) | `/api/mcp/servers` |
| **OpenAPI Specs** | Register REST APIs via OpenAPI specs for automatic MCP conversion | `/api/openapi/specs` |
| **Guardrail Rules** | Configure prompt injection, toxicity, and topic block filters | `/api/guardrails/rules` |
| **Routing Strategy** | Define heuristic thresholds, model tiers, and rule DSL | `/api/smart-loadbalancer/strategies` |
| **Feature Flags** | Hot toggle PII masking, semantic cache, rate limit, budget, and loop detection | `/api/features` |
| **Prompt Templates** | Versioned system prompts with traffic deployment splits | `/api/prompts` |
| **Jobs Engine** | Define scheduled LLM + MCP tasks with cron and webhook triggers | `/api/jobs` |

---

## Configuration Precedence & Order

When evaluating settings, ILTER applies configuration in the following order (highest precedence wins):

1. **Request-level Headers & Context** (e.g., explicit model in request payload)
2. **Runtime Configuration** (SQLite `runtime_config` table state)
3. **Environment Variables** (`ILTER_*` env vars)
4. **Compiled-in Defaults**
