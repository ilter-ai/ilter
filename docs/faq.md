# Frequently Asked Questions — ILTER

This document is divided into two sections: **User FAQ**, covering daily operational questions, and **Internal FAQ**, detailing technical design decisions and architecture rationale.

---

## User FAQ

### Do I need to modify my existing application code?

No. ILTER speaks the exact OpenAI chat completions API (`/v1/chat/completions`). Any application currently using the OpenAI SDK or an OpenAI-compatible client will work seamlessly with ILTER simply by updating the `base_url` to `http://localhost:8181/v1`. Provider switching, PII protection, and cost tracking all happen at the proxy layer—not a single line of your application code needs to change.

---

### Which providers does ILTER support?

| Provider Config Type | Notes |
|----------------------|-------|
| `openai` | Native format |
| `anthropic` | System message extraction, content block conversion |
| `gemini` | Google Gemini OpenAI-compatible mode |
| `deepseek` | OpenAI-compatible |
| `openrouter` | `HTTP-Referer` + `X-Title` headers automatically injected |
| `ollama` | Local inference, OpenAI-compatible mode |
| `qwen` | Qwen / Alibaba DashScope OpenAI-compatible mode |
| `opencode_go`, `opencode_zen` | OpenCode SDK-configured endpoint mapping |
| `mock` | Built-in test mock provider |

---

### How does PII detection work? What data does it catch?

Three layers work together:

1. **Bloom Filter** — probabilistic name pre-filtering (<0.04ms)
2. **Aho-Corasick Trie** — dictionary-based deterministic multi-pattern matching
3. **Regex patterns** — structured data (TCKN, SSN, Credit Cards, Emails, Phones, IPs)

**Three operational modes:**

| Mode | Behavior |
|-----|---------|
| `mask` | Replaces with `[PII_EMAIL]`, `[PII_NAME]` — original data is obscured |
| `reversible` | Replaces with `PII:EMAIL:a8b9f1` — original value is restored token-by-token in response and SSE stream |
| `block` | Returns `422 pii_blocked` — request is blocked immediately |

---

### Are streaming requests supported?

Yes. SSE (Server-Sent Events) streaming is supported across all providers. The `reversible` PII mode works chunk-by-chunk during streaming. Token usage and audit logs are recorded upon stream completion.

---

### How are budget limits defined, and what happens when they are exceeded?

Daily and monthly limits (in USD) can be set for each API key via the dashboard or Admin API. Exceeding limits triggers an instant `429 budget_exceeded` response before contacting upstream providers.

---

### How does the Smart Router reduce costs?

The Smart Router evaluates prompt complexity in real-time (<0.03ms) and scores it between 0 and 100:

| Score | Tier | Target Model |
|------|------|--------------|
| 0–15 | Economy | Basic / low-cost models (e.g. gpt-4o-mini, deepseek-chat) |
| 16–50 | Standard | Mid-range models |
| ≥51 | Premium | Top-tier models (e.g. claude-3-7-sonnet, gpt-4o) |

Up to 40–70% of routine queries fall into the economy tier, significantly reducing monthly LLM spend.

---

### How does agent loop detection work?

Four parallel detection mechanisms run across a sliding window:

| Detector | Threshold | Action |
|----------|-----------|--------|
| Rate | >30 requests/second (same key) | Throttle |
| Fingerprint | Same prompt >5× (20 request window) | Block |
| Cost Accumulation | >$5 / 5 minutes (same key) | Block |
| Session Limit | >100 requests (single session) | Throttle |

---

### Do I need to change my client code to use the MCP Gateway?

No. The MCP Gateway operates entirely at the proxy layer. When you register an MCP server (stdio or SSE), ILTER injects tools as OpenAI `tool` definitions. When the model returns a `tool_call`, ILTER intercepts it, executes the tool via MCP, and feeds the result back to the model transparently.

Remote MCP clients (such as VS Code) can authenticate via built-in OAuth PKCE endpoints on port `8181`.

---

### Does the Cron engine require external infrastructure?

No. The Jobs engine runs embedded inside the binary. Cron schedules, webhook triggers, variable interpolation (`{{.prev}}`), and retries run without Redis or external job brokers.

---

### Do I need to install Node/npm separately for the dashboard?

No. The dashboard (Astro + React) is pre-compiled at build time and embedded into the Go binary using Go `//go:embed`. When you run `./ilter serve`, the dashboard is live on port `9191`.

---

### Is Redis mandatory?

No. Redis is optional and used only for vector-based semantic cache and distributed rate limiting across multiple instances. If Redis is absent, ILTER degrades gracefully (fails open) and operates as a standalone proxy.

---

## Architectural Decisions

### Why Go 1.26.3? Why not Node.js or Python?

- **Single static binary** — zero runtime dependencies, instant deployment.
- **Sub-millisecond overhead** — goroutine concurrency is ideal for high-throughput SSE streaming.
- **Minimal memory & image size** — idle footprint ~30-50MB, final container image `<20MB`.
- **CGo-free compilation** — pure Go runtime enables clean cross-compilation across Linux, macOS, and ARM64.

---

### Why SQLite instead of PostgreSQL?

ILTER targets zero-infrastructure overhead. `modernc.org/sqlite` is a pure Go CGo-free SQLite driver operating in WAL (Write-Ahead Logging) mode. It handles thousands of concurrent reads and hundreds of writes per second without requiring a database server or network overhead.

---

### Why a 3-Stage `scratch` Docker build?

- **Stage 1 (`oven/bun:1-alpine`)** compiles the Astro UI.
- **Stage 2 (`golang:1.26-alpine`)** compiles the Go binary and applies UPX compression (`upx --best --lzma`).
- **Stage 3 (`scratch`)** creates an empty base image with only the binary, CA certificates, and timezones.

This reduces the container attack surface (no shell or package manager inside the container) and shrinks image size to `<20MB`.

---

### Why standard Kubernetes manifests instead of a Helm Chart?

For a single-binary application, a standard Kubernetes `Deployment` and `Service` manifest provides clear, transparent, and version-controllable configuration without forcing users to manage Helm releases or tiller dependencies.

---

### Why is the middleware chain order fixed?

The order is strictly mandated by security requirements:

```
Auth → Rate Limit → Budget → Prompt Inject → PII Mask →
Guardrails → MCP Inject → Smart Router → Loop Detect →
Semantic Cache → Provider
```

- **Auth & Rate Limiting run first** to drop unauthorized or abusive requests instantly.
- **PII Masking runs before Semantic Cache** to prevent raw sensitive user data from entering vector embeddings or external cache stores.
- **Guardrails run before MCP Inject** to prevent prompt injections from triggering unauthorized tool calls.

---

### What is the security model for API keys?

- **Format**: `ilter-` prefix + 32 random bytes (hex-encoded).
- **Storage**: Argon2id salted hashes (`salt:hash`). Plaintext keys are shown only once upon creation.
- **Provider Secrets**: Encrypted at rest using AES-256-GCM with a key derived from `ILTER_ADMIN_API_KEY`.

---

### How to add a new upstream provider?

1. Create a new provider file in `internal/provider/` implementing the `Provider` interface.
2. Register the provider type in `internal/provider/factory.go`.
3. Add default model configs in `internal/registry/`.
4. Add unit tests in `internal/provider/<name>_test.go`.
5. Update documentation files (`docs/architecture.md`, `docs/faq.md`).