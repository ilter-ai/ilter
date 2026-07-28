// Package middleware provides HTTP middleware for the ILTER-AI proxy.
//
// Middleware Chain Order (for /v1/chat/completions requests):
//
//  1. ObservabilityHandler (optional, OTel metrics)
//  2. AuthMiddleware         - validates API key, sets key_id in context
//  3. RateLimitMiddleware    - enforces RPM/TPM limits per API key
//  4. BudgetMiddleware       - enforces monthly budget per API key
//  5. PromptInjection        - injects configured system prompts
//  6. PIIMaskerMiddleware    - masks credit cards, TC Kimlik, SSN, email, phone, IPs
//  7. GuardrailsMiddleware   - prompt-injection, toxicity, topic-block checks
//  8. MCPInjectMiddleware    - injects authorized MCP tools into requests,
//     intercepts tool_calls in responses, executes tools via MCP,
//     and returns the final result (transparent tool-call loop)
//  9. SmartRouterMiddleware  - reads active strategy, scores, matches rules,
//     selects model & provider preference, stores in context
//
// 10. LoopDetectorMiddleware - detects and breaks request loops
//
// 11. SemanticCacheMiddleware - checks cache via embedding
//
// 12. ChatCompletions handler- routes to provider, handles streaming, records audit+budget
//
// This ordering ensures that auth and rate-limiting happen before any PII processing,
// PII and guardrails run before MCP injection, and the semantic cache sees the
// fully-enriched request (with injected tools).
package middleware
