# Contributing to ILTER

First off — thank you for even considering contributing. ILTER is built in the open, for developers who are tired of black-box AI cost surprises and PII flying off to providers unchecked. Every PR that moves the needle on that mission matters.

This guide is **not** a list of rules to follow blindly. It's context — the *why* behind how ILTER is built. Read it once, and you'll understand why we make the decisions we make. Then you can make good calls on your own.

---

## Quick Start

```bash
git clone https://github.com/ilter-ai/ilter
cd ilter/

# Build everything (Go + web dashboard)
make build

# Run tests
make test

# Lint + type-check
make check

# Format code
make fix

# Hot reload for development
make dev
```

> **`make dev`** starts Air (Go hot-reload) + Vite (dashboard HMR) concurrently. Make changes, see them live.

---

## The Architecture in One Paragraph

ILTER is a single Go binary. It runs two HTTP servers: a proxy on `:8181` (handles `/v1/chat/completions`) and a dashboard on `:9191`. Requests flow through an 11-step middleware chain — auth → rate limit → budget → PII masking → guardrails → MCP injection → smart routing → loop detection → semantic cache → provider — before hitting the upstream LLM. State lives in embedded SQLite (always) and optional Redis (cache, rate limits). Everything is constructor-injected. No global state. No init() side effects.

See [`docs/architecture.md`](docs/architecture.md) for the full picture.

---

## Architecture Decisions You Need to Know

These are *settled* decisions. Before you send a PR that touches these areas, read the rationale. We won't merge changes that go against them without a strong new argument.

### ❌ No `panic` — always return `error`

If something fails, propagate it: `fmt.Errorf("context: %w", err)`. Panics in a gateway that handles production traffic are unacceptable. Early-return pattern everywhere — avoid `else` blocks after `if err != nil`.

### ❌ No CGo dependencies

ILTER must cross-compile cleanly (linux/amd64, linux/arm64, darwin). That means `modernc.org/sqlite` (pure Go) instead of `mattn/go-sqlite3`. Don't add CGo deps.

### ✅ Dependency injection via constructors

`NewHandler(db, cfg)` — always. No singletons, no package-level `var`. This makes testing straightforward: pass in a test DB, a mock client, done.

### ✅ `log/slog` for structured logging

Not `fmt.Println`, not `log.Printf`. Structured JSON logs with context propagation.

### ✅ Clean Architecture boundary: features vs. middleware

Every feature (`pii`, `budget`, `guardrails`, etc.) lives in `internal/features/<domain>/` and has **zero knowledge of HTTP**. The HTTP adapter lives in `internal/middleware/<feature>.go`. Features export transport-agnostic methods. Middleware handles HTTP parsing and calls the feature. This boundary is strict — don't blur it.

---

## Middleware Chain Order — Do Not Reorder

The chain in `internal/middleware/doc.go` is ordered for correctness, not convenience. PII *must* run before the semantic cache (preventing sensitive data reaching embedding models). Auth *must* run before everything else. If you think the order should change, open an issue first and explain why.

---

## What We're Looking For

### 🟢 High-value contributions
- Bug fixes with a reproduction test
- New provider adapters (implement the `provider.Provider` interface in `internal/provider/`)
- OTel metrics for currently unmeasured paths
- Dashboard improvements (we use Astro + React + shadcn/ui + Recharts)
- Failure mode tests (`tests/chaos/`)
- Documentation improvements

### 🟡 Needs discussion first
- New middleware layers
- Changes to the request flow order
- New external dependencies
- Database schema changes (new migration file in `internal/db/migrations/`)

### 🔴 Won't merge without strong justification
- Global state / init() side effects
- CGo dependencies
- Viper or any config-file-based system
- Backward compatibility shims (we're MVP — fix callers directly)

---

## Adding a New Provider

1. Create `internal/provider/<name>.go` implementing the `provider.Provider` interface
2. Add a `<name>_test.go` with at minimum: happy path, streaming, error handling
3. Register in `internal/provider/factory.go`
4. Add to `internal/registry/` model config entries
5. Document in `docs/architecture.md` under the provider table

Look at `internal/provider/openrouter.go` for a minimal example, or `internal/provider/anthropic.go` for a provider that requires format transformation.

---

## Testing

```bash
make test          # Unit + integration tests (-race -count=1)
make check-go      # Build + golangci-lint
make check-web     # Build dashboard + astro check + biome lint
```

**Rules:**
- Every new feature needs at least one test
- Use `httptest.NewRecorder()` + `chi.NewRouter()` for handler tests
- Use `roundTripFunc` mock transport for provider tests (see `internal/provider/mock.go`)
- LSP diagnostics must be clean on changed files

---

## Pull Request Checklist

Before opening a PR:

- [ ] `make check` passes (Go build + lint + web build + web lint)
- [ ] `make test` passes
- [ ] New code has tests
- [ ] If you changed middleware order, updated `internal/middleware/doc.go`
- [ ] If you added a dependency, justified it in the PR description
- [ ] If you changed the DB schema, added a migration file

---

## Good First Issues

Look for issues labeled [`good first issue`](https://github.com/ilter-ai/ilter/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) — each one includes context, the file to start from, and acceptance criteria. No need to ask permission, just comment that you're working on it.

---

## Questions?

Open an [issue](https://github.com/ilter-ai/ilter/issues) or start a [discussion](https://github.com/ilter-ai/ilter/discussions). We respond fast.
