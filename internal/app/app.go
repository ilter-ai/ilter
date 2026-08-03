package app

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/dashboard"
	"github.com/ilter-ai/ilter/internal/dashboard/access"
	dashjobs "github.com/ilter-ai/ilter/internal/dashboard/jobs"
	dashmcp "github.com/ilter-ai/ilter/internal/dashboard/mcp"
	"github.com/ilter-ai/ilter/internal/dashboard/prompts"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/jobs"
	"github.com/ilter-ai/ilter/internal/jobs/triggers"
	iltermiddleware "github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	mcptransport "github.com/ilter-ai/ilter/internal/platform/transport/mcp"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
)

type App struct {
	cfg           *config.Config
	cfgCache      *config.Cache
	store         *db.SQLiteStore
	cooldownStore cooldown.Store
	reg           *provider.Registry
	rg            *circuitbreaker.RedisBreaker
	cacheGuard    *circuitbreaker.RedisBreaker

	authMiddleware          *iltermiddleware.AuthMiddleware
	rateLimitMiddleware     *iltermiddleware.RateLimitMiddleware
	budgetMiddleware        *iltermiddleware.BudgetMiddleware
	semanticCacheMiddleware *iltermiddleware.SemanticCacheMiddleware
	auditLoggerMiddleware   *iltermiddleware.AuditLoggerMiddleware
	piiMaskerMiddleware     *iltermiddleware.PIIMaskerMiddleware
	guardrailsMiddleware    *iltermiddleware.GuardrailsMiddleware
	mcpInjectMiddleware     *iltermiddleware.MCPInjectMiddleware
	loopDetectMiddleware    *iltermiddleware.LoopDetectorMiddleware
	promptMiddleware        *iltermiddleware.PromptInjectionMiddleware
	smartRouterMiddleware   *iltermiddleware.SmartRouterMiddleware

	lb           *smartrouter.LoadBalancer
	loopDetector *loopdetect.Detector
	proxyHandler *proxy.Handler
	sr           *smartrouter.SmartRouter

	adminHandler      *access.Handler
	mcpHandler        *dashmcp.MCPHandler
	openapiHandler    *dashboard.OpenAPIHandler
	accessHandler     *access.Handler
	promptHandler     *prompts.PromptHandler
	guardrailsHandler *dashboard.GuardrailsHandler
	jobsHandler       *dashjobs.JobsHandler

	mcpAuditLogger    *mcp.AuditLogger
	mcpGatewayHandler *mcptransport.GatewayHandler
	mcpExecutor       *mcp.Executor
	mcpHubHandler     *mcptransport.HubHandler

	jobRunner   *jobs.JobRunner
	cronTrigger *triggers.CronTrigger

	oauthStore         *mcp.OAuthStore
	oauthCleanupCancel context.CancelFunc

	r *chi.Mux

	auditor *audit.SQLiteConfigAuditor
}

// New creates a new App with the given config.
func New(cfg *config.Config) (*App, error) {
	return &App{cfg: cfg}, nil
}
