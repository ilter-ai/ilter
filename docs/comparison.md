# ILTER vs. Other AI Gateways

> Quick reference for developers evaluating AI gateway options. We try to be honest — if a competitor does something better, we say so.

---

## Technical Comparison Matrix

| Feature / Aspect | ILTER | LiteLLM | Portkey | HelixGateway |
|------------------|-------|---------|---------|-------------|
| **Language & Runtime** | Go 1.26.3 (single static binary) | Python | SaaS + self-host | Go |
| **Container Base Image** | 3-stage empty `scratch` (<20MB) | Python Alpine (~400MB) | Cloud / Docker | Docker |
| **CVE Attack Surface** | Virtually zero (no OS packages/shell) | Moderate (Python env + OS deps) | Closed SaaS / Docker | Low |
| **Zero-config start** | ✅ `./ilter serve` | Requires config YAML | Requires API key | Requires config |
| **Built-in PII masking** | ✅ Bloom Filter + Trie (<0.04ms) | ❌ | ❌ | ❌ |
| **Agentic Loop Detection** | ✅ 4 parallel detectors | ❌ | ❌ | ❌ |
| **MCP Gateway + OAuth** | ✅ Full (stdio/SSE + OAuth PKCE) | ❌ | ❌ | ❌ |
| **Embedded Dashboard** | ✅ Astro + React (port 9191, zero node dep) | ✅ | ✅ (cloud) | ❌ |
| **Prometheus Metrics** | ✅ Dedicated OTel endpoint (:9192) | ✅ | ✅ | Partial |
| **Semantic Cache** | ✅ Redis VSS + SHA256 fallback | ✅ | ✅ | ❌ |
| **Smart Routing** | ✅ Heuristic scorer + Rule DSL | ✅ | ✅ | Partial |
| **Budget Enforcement** | ✅ Hard kill switch (USD) | ✅ | ✅ | Partial |
| **Air-gap / On-prem** | ✅ Statically linked binary | Partial | ❌ (SaaS) | ✅ |
| **Memory Footprint** | ~30 idle | ~200-400MB | N/A | ~50MB |

---

## ILTER vs. LiteLLM

**LiteLLM** is a popular Python-based AI proxy. It features broad provider integration and a large ecosystem.

**Choose LiteLLM if:**
- Your backend is strictly Python-native and you rely on Python SDK extensions
- You require support for legacy or niche providers (100+ integrations)

**Choose ILTER if:**
- You want a single binary with **zero Python or Node.js runtime dependencies**
- You need high-performance **PII masking** (<0.04ms) before data leaves your network
- You operate under strict image size (<20MB) or **zero-CVE OS attack surface** requirements (`scratch` base image)
- You require **agentic loop protection** against runaway LLM loops
- You use **MCP tools** and need seamless tool injection or OAuth PKCE authentication

---

## ILTER vs. Portkey

**Portkey** is a cloud-first SaaS gateway with rich observability features.

**Choose Portkey if:**
- You prefer a fully-managed SaaS platform
- Cloud metadata processing is compliant with your security policies

**Choose ILTER if:**
- You need 100% on-premise or air-gapped data sovereignty
- Strict compliance requires keeping prompts and PII entirely within your network
- You want zero recurring proxy platform subscription costs

---

## Supported Upstream Providers

ILTER supports 9 provider integration types out of the box (`internal/provider/factory.go`):

1. **OpenAI** (native format)
2. **Anthropic** (content block & system message conversion)
3. **Google Gemini** (OpenAI-compatible)
4. **DeepSeek** (OpenAI-compatible)
5. **OpenRouter** (automatic referrer/title header injection)
6. **Ollama** (local inference models)
7. **Qwen** (Alibaba DashScope OpenAI-compatible)
8. **OpenCode** (`opencode_go` and `opencode_zen` SDK endpoints)
9. **Mock** (built-in testing mock provider)

---

## Honest Limitations

Things ILTER does not do (yet):

- **100+ exotic provider adapters** — ILTER focuses on major production LLM providers.
- **Hosted SaaS multi-tenancy** — ILTER is designed as a self-hosted single-binary gateway.
- **Audio / Video binary transformations** — Currently optimized for Chat Completions (`/v1/chat/completions`) and tool execution.
