# ILTER — Agent Setup Guide

> This document is written for an AI coding agent (Claude Code, Cursor, etc.) acting on behalf of a human. Follow the steps in order. Only stop to ask the human when a step needs information only they have (which provider to use, a real API key, a port conflict).

## Goal

Get ILTER running locally or in the human's target environment, put at least one AI provider behind it, and point the human's application at it — with the fewest possible manual steps for them.

## 0. Ask what the human actually wants first

Don't install blind. Before touching anything, figure out — from the conversation, the project's existing code, or by asking directly — which of these applies:

- **Just swap the endpoint** (drop-in OpenAI-compatible proxy, no extra features touched) → steps 1–6 only, skip everything provider/feature-specific.
- **Cost/budget control** (spending caps, per-key limits) → after step 6, configure via `/api/keys` and `/api/budget` — see `api/openapi.yaml`.
- **PII masking / guardrails** for compliance → configure via `/api/pii` and `/api/guardrails/rules` before routing real traffic.
- **MCP tools / OpenAPI-to-MCP bridge** for agentic workflows → needs `/api/mcp/servers` or `/api/openapi/specs` set up, and the human's MCP client pointed at `/mcp`.
- **Just evaluating / demoing** → `./ilter init --demo` instead of real provider keys, skip step 3's secret-hunting.

The answer changes which env vars, endpoints, and dashboard sections matter — don't configure PII rules or MCP servers nobody asked for, and don't skip budget/guardrail setup if compliance was the stated reason for adopting ILTER.

## 1. Pick an install method

| Situation | Method |
|---|---|
| Human just wants to try it locally, no container runtime constraints | **Binary** |
| Project already uses Docker / docker-compose | **Docker** |
| Human wants to modify ILTER source, or no release matches their OS/arch | **Build from source** |
| Deploying to a cluster | **Docker + Kubernetes manifest**, see [`deployment.md`](deployment.md#kubernetes-deployment) |

Default to **Docker** if a container runtime is available and the project has no strong preference — it needs the least setup (no Go toolchain, no PATH changes).

## 2. Get ILTER

**Docker:**
```bash
docker pull ykocaman/ilter:latest
```

**Binary** — detect OS/arch, download the matching asset from the latest release:
```bash
curl -s https://api.github.com/repos/ilter-ai/ilter/releases/latest \
  | grep browser_download_url | grep "$(uname -s | tr '[:upper:]' '[:lower:]')" | grep "$(uname -m)"
```
Download, `chmod +x`, and place it wherever the human keeps local binaries (or the project root).

**Build from source:**
```bash
git clone https://github.com/ilter-ai/ilter.git
cd ilter
make build   # produces ./ilter
```

## 3. Collect required secrets

Two things are required before `serve` will start: `ILTER_ADMIN_API_KEY` and at least one provider key.

- **Admin key** — generate one if the human hasn't provided one:
  ```bash
  openssl rand -hex 32
  ```
- **Provider key** — check the project first (`.env`, `.env.local`, existing env vars) for `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `DEEPSEEK_API_KEY`, `OPENROUTER_API_KEY`, etc. Reuse whatever is already there instead of asking. If nothing is found, ask the human which provider they want and for the key — never invent or guess one.

Supported provider env var names: `ILTER_PROVIDER_OPENAI_API_KEY`, `ILTER_PROVIDER_ANTHROPIC_API_KEY`, `ILTER_PROVIDER_GEMINI_API_KEY`, `ILTER_PROVIDER_DEEPSEEK_API_KEY`, `ILTER_PROVIDER_OPENROUTER_API_KEY`, `ILTER_PROVIDER_OLLAMA_URL`.

## 4. Start it

**Docker:**
```bash
docker run -d \
  --name ilter \
  -p 8181:8181 -p 9191:9191 -p 9192:9192 \
  -v $(pwd)/data:/app/data \
  -e ILTER_ADMIN_API_KEY=<admin-key> \
  -e ILTER_PROVIDER_OPENAI_API_KEY=<provider-key> \
  ykocaman/ilter:latest
```

**Binary:**
```bash
ILTER_ADMIN_API_KEY=<admin-key> \
ILTER_PROVIDER_OPENAI_API_KEY=<provider-key> \
./ilter serve
```

If the default ports (8181 proxy, 9191 dashboard, 9192 metrics) are already taken in the project, override with `ILTER_SERVER_PORT`, `ILTER_DASHBOARD_PORT`, `ILTER_METRICS_LISTEN_ADDR` — check for conflicts first (`lsof -i :8181` or equivalent).

## 5. Verify it's up

```bash
curl -s http://localhost:8181/admin/health

curl -s -X POST http://localhost:8181/v1/chat/completions \
  -H "Authorization: Bearer <admin-key>" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Hello!"}]}'
```
A `200` with a real completion means the provider key works and the gateway is routing correctly.

## 6. Point the human's application at ILTER

No SDK migration needed — ILTER speaks the OpenAI-compatible API. In the human's codebase, find wherever the OpenAI/Anthropic/etc. client is constructed and:

- Change the `base_url` (or `OPENAI_BASE_URL` / equivalent env var) to `http://localhost:8181/v1`
- Replace the API key used by that client with the ILTER admin key or a per-key credential created via the dashboard/API (see step 7)
- Leave request/response handling untouched — the wire format is unchanged

## 7. Optional: proper per-key setup instead of the admin key

The admin key is a break-glass credential, not meant for routing day-to-day app traffic. For anything beyond a quick local test, create a scoped key:

```bash
./ilter init
```
This runs an interactive wizard to configure providers, routing, and issue scoped API keys. Or use the dashboard at `http://localhost:9191`, or the `/api/*` admin endpoints directly.

## Reference

- **Full API surface** (every endpoint, request/response schema — providers, keys, budget, PII, guardrails, MCP, routing, jobs): [`api/openapi.yaml`](../api/openapi.yaml). Read this before implementing anything beyond the basic chat-completions swap — don't guess endpoint shapes.
- Full environment variable list: [`configuration.md`](configuration.md)
- Docker Compose / Kubernetes / production topologies: [`deployment.md`](deployment.md)
- How requests flow through the gateway: [`architecture.md`](architecture.md)
- Design rationale / FAQ: [`faq.md`](faq.md)
