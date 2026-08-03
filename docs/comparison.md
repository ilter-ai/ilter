# ILTER vs. Other AI Gateways

> We try to be honest here — if a competitor has something we don't, the table says so. Checked against each project's own repo/docs as of 2026-08; these projects ship fast, so re-verify before quoting externally.

---

## Who is each one actually for?

| Project | Built for |
|---|---|
| **ILTER** | Teams self-hosting production LLM traffic who want governance (budget kill-switch, PII masking, guardrails, loop protection) built into the gateway itself — a single binary, no Python/Node runtime to install |
| **[LiteLLM](https://github.com/BerriAI/litellm)** | Python-native teams/enterprises wanting the broadest provider + plugin ecosystem, fine running Python+Node+DB |
| **[Higress](https://github.com/higress-group/higress)** | Platform/SRE teams already on Kubernetes + Istio who want AI routing as one more capability of their existing cloud-native ingress gateway |
| **[New API](https://github.com/QuantumNous/new-api)** | People running or **reselling** AI API access — multi-tenant quota/billing, token shops, teams monetizing spare provider capacity |
| **[OmniRoute](https://github.com/diegosouzapw/OmniRoute)** | Individual devs chasing free-tier tokens across 290+ providers for coding agents (Claude Code, Cursor, etc.) — cost-minimization first |
| **[9Router](https://github.com/decolua/9router)** | Individual devs stretching subscription-based coding-tool quotas (Claude Code/Cursor) via auto-fallback + token compression |
| **[Apache APISIX](https://github.com/apache/apisix)** | Large enterprises with an existing general-purpose API gateway who want LLM proxying as one more upstream type, not a dedicated AI governance layer |

---

## Feature Comparison

| Feature | ILTER | LiteLLM | Higress | New API | OmniRoute | 9Router | APISIX |
|---|---|---|---|---|---|---|---|
| MCP Gateway (tool injection/interception) | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ✅ (one-way bridge only) |
| MCP Marketplace (browse + one-click install) | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |
| OpenAPI → MCP tool bridge | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Smart Router (complexity-scored tiering) | ✅ | ⚠️ routing exists, not complexity-scored | ❌ | ❌ weighted-random only | ✅ 12-factor scoring | ⚠️ simple subscription→cheap→free chain | ❌ |
| Provider fallback / circuit breaker | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ⚠️ generic, not LLM-aware |
| Hard budget kill-switch (per key, USD) | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ (rate-limit only, not $) |
| PII masking, in-process | ✅ names/email/phone/SSN/card | ⚠️ external Presidio/Lakera only | ❌ | ❌ | ⚠️ API-key/secret redaction only, not general PII | ❌ | ❌ |
| Semantic cache (vector similarity) | ✅ | ✅ | ❌ | ❌ | ⚠️ leans on provider prompt-cache, not its own vector cache | ❌ | ❌ |
| Prompt guardrails (injection/toxicity) | ✅ | ⚠️ external providers only | ❌ | ❌ | ✅ | ❌ | ❌ |
| Agent loop detector | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Cron / scheduled AI workflows | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Single binary, no Python/Node runtime required | ✅ | ❌ Python+Node | ❌ Envoy/Istio+K8s | ✅ Go binary — genuine tie | ❌ Node/npm | ❌ Node/npm | ❌ Nginx/OpenResty+etcd |

---

## Honestly, where we don't win

- **Provider count & ecosystem:** LiteLLM (100+ integrations, years of plugins) and OmniRoute (290+ providers) both beat ILTER's 9 on raw coverage.
- **Billing/reseller tooling:** New API's multi-tenant quota and reselling features are more built-out than anything ILTER offers — ILTER isn't built to run as a token shop.
- **Cloud-native/K8s fit:** Higress and APISIX are the better choice if you're already running Istio/Envoy or APISIX and just want to bolt on LLM routing rather than run a separate gateway.
- **Agent loop detection and cron workflows** are the two rows where we didn't find *any* equivalent elsewhere — genuinely unique to ILTER among this list, not just better-marketed.
