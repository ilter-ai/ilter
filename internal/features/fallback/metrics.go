package fallback

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	fallbackMetricsOnce sync.Once
	retryAfterCapped    metric.Int64Counter
	cooldownCapped      metric.Int64Counter
)

func ensureFallbackMetrics() {
	fallbackMetricsOnce.Do(func() {
		m := otel.Meter("ilter-proxy") // same namespace as middleware metrics
		retryAfterCapped, _ = m.Int64Counter(
			"ilter_retry_after_capped_total",
			metric.WithDescription("Total number of times ParseRetryAfter capped a cooldown duration"),
		)
		cooldownCapped, _ = m.Int64Counter(
			"ilter_cooldown_capped_total",
			metric.WithDescription("Total number of times executor safety cap limited cooldown"),
		)
	})
}

// errors silently dropped — counter creation basically never fails with OTel;
// if we ever need observability into init failures, bubble up from ensureFallbackMetrics.

func incRetryAfterCapped() {
	ensureFallbackMetrics()
	retryAfterCapped.Add(context.Background(), 1)
}

func incCooldownCapped() {
	ensureFallbackMetrics()
	cooldownCapped.Add(context.Background(), 1)
}
