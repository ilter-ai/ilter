// Package circuitbreaker provides circuit-breaking wrappers for outbound HTTP
// and Redis calls. Both use gobreaker under the hood:
//   - HTTPBreaker wraps http.RoundTripper for provider API calls
//   - RedisBreaker wraps redis.Client for Redis calls with fail-open/fail-closed policies
//
// Merged from the former internal/circuit and internal/redisguard packages.
package circuitbreaker
