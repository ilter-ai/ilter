package dashboard

// This file re-exports types from subpackages for test compilation.
// Tests in this package reference types without qualifiers.

import (
	"github.com/ilter-ai/ilter/internal/dashboard/features"
	"github.com/ilter-ai/ilter/internal/dashboard/models"
	"github.com/ilter-ai/ilter/internal/dashboard/providers"
	"github.com/ilter-ai/ilter/internal/dashboard/smartrouter"
	"github.com/ilter-ai/ilter/internal/dashboard/stats"
)

// Stats
type (
	StatsResponse                 = stats.Response
	CircuitBreakerSummaryResponse = stats.CircuitBreakerSummaryResponse
)

// Features
type FeatureItem = features.FeatureItem

// Models
type (
	ModelResponseItem      = models.ModelResponseItem
	ToggleModelRequest     = models.ToggleModelRequest
	UpdateModelTierRequest = models.UpdateModelTierRequest
)

// Providers
type ProviderSummary = providers.ProviderSummary

// Smart router
type (
	OptimizeRequest            = smartrouter.OptimizeRequest
	OptimizeResponse           = smartrouter.OptimizeResponse
	SmartRouterStatsResponse   = smartrouter.StatsResponse
	SmartRouterHistoryResponse = smartrouter.HistoryResponse
)

// PII
type (
	PIIExportItem = ExportItem
	PIIEventItem  = EventItem
	PIIStats      = Stats
)

// Guardrails
type GuardrailViolationsResponse = Page[GuardrailEventItem]
