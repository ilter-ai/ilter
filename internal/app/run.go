package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/dashboard"
	"github.com/ilter-ai/ilter/internal/dashboard/access"
	dashmcp "github.com/ilter-ai/ilter/internal/dashboard/mcp"
	"github.com/ilter-ai/ilter/internal/dashboard/prompts"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/features/fallback"
	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	iltermiddleware "github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	mcptransport "github.com/ilter-ai/ilter/internal/platform/transport/mcp"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
)

// RunServe is the public method that orchestrates component creation and startup.
func (a *App) RunServe() error {
	cfg := a.cfg

	a.setupLogging()

	boot := config.ToBootConfig(cfg)
	a.cfgCache = config.NewConfigCache(&boot)

	if err := a.initStore(); err != nil {
		return err
	}
	defer a.store.Close()

	if err := a.cfgCache.Refresh(context.Background(), &config.RuntimeStores{RuntimeConfig: a.store}); err != nil {
		slog.Warn("failed to load runtime_config state", "error", err)
	}

	if snap := a.cfgCache.Get(); snap != nil {
		if snap.PII.Mode != "" {
			cfg.PII.Mode = snap.PII.Mode
		}
		if snap.Dashboard.Port != 0 {
			cfg.Dashboard.Port = snap.Dashboard.Port
		}
		if snap.Metrics.ListenAddr != config.DefaultMetricsListenAddr {
			cfg.Metrics.ListenAddr = snap.Metrics.ListenAddr
		}
	}

	a.reg = provider.NewRegistry()
	if errInit := a.reg.InitFromConfig(cfg); errInit != nil {
		return fmt.Errorf("failed to initialize providers: %w", errInit)
	}

	a.rg = a.initRedis()

	var cacheGuard *circuitbreaker.RedisBreaker
	if cfg.Cache.Enabled && cfg.Cache.RedisURL != "" {
		if redisOpts, parseErr := redis.ParseURL(cfg.Cache.RedisURL); parseErr == nil {
			client := redis.NewClient(redisOpts)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if pingErr := client.Ping(ctx).Err(); pingErr == nil {
				cacheGuard = circuitbreaker.NewRedisBreaker(client, 200*time.Millisecond, gobreaker.Settings{})
			} else {
				slog.Warn("semantic cache: Redis ping failed, cache disabled", "addr", redisOpts.Addr, "error", pingErr)
			}
			cancel()
		} else {
			slog.Warn("semantic cache: invalid ILTER_REDIS_URL, cache disabled", "url", cfg.Cache.RedisURL, "error", parseErr)
		}
	}
	a.cacheGuard = cacheGuard

	a.initMiddleware(a.rg, cacheGuard)
	defer a.auditLoggerMiddleware.Close()

	a.discoverModelsAtStartup()
	a.syncModelsToDB()

	lb, loopDetector, err := initLoadBalancer(cfg, a.reg, a.store, a.cfgCache)
	if err != nil {
		return err
	}
	a.lb = lb
	a.loopDetector = loopDetector

	proxyHandler := proxy.NewHandler(lb, a.auditLoggerMiddleware, a.budgetMiddleware.Enforcer(), loopDetector)
	proxyHandler.SetStore(a.store)
	proxyHandler.SetConfig(cfg)
	proxyHandler.SetConfigCache(a.cfgCache)

	var redisClient *redis.Client
	if a.rg != nil {
		redisClient = a.rg.Client()
	}
	var cooldownStore cooldown.Store = cooldown.NewRedisStore(redisClient, a.rg)
	a.cooldownStore = cooldownStore

	fe := fallback.NewFallbackExecutor(cfg.Fallback, cooldownStore, a.reg)
	proxyHandler.SetFallbackExecutor(fe, cooldownStore)

	a.proxyHandler = proxyHandler
	a.loopDetectMiddleware = iltermiddleware.NewLoopDetectorMiddleware(loopDetector, a.store, a.cfgCache)

	sr := smartrouter.NewSmartRouterFromCache(a.cfgCache, lb)
	a.sr = sr
	a.smartRouterMiddleware = iltermiddleware.NewSmartRouterMiddleware(a.cfgCache, sr)

	var adminRedisClient *redis.Client
	if cacheGuard != nil {
		adminRedisClient = cacheGuard.Client()
	} else if a.rg != nil {
		adminRedisClient = a.rg.Client()
	}
	a.auditor = audit.NewSQLiteConfigAuditor(a.store.DB)
	adminHandler := access.NewHandler(a.store, adminRedisClient, a.auditor)
	a.adminHandler = adminHandler

	mcpAuditLogger := mcp.NewAuditLogger(a.store)
	defer mcpAuditLogger.Close()
	a.mcpAuditLogger = mcpAuditLogger
	a.mcpHandler = dashmcp.NewMCPHandler(a.store, mcpAuditLogger, a.auditor)
	a.accessHandler = adminHandler
	a.openapiHandler = dashboard.NewOpenAPIHandler(a.store, a.auditor)

	var piiRedisGuard *circuitbreaker.RedisBreaker
	if cfg.PII.RedisURL != "" {
		redisOpts, redisParseErr := redis.ParseURL(cfg.PII.RedisURL)
		if redisParseErr == nil {
			client := redis.NewClient(redisOpts)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if pingErr := client.Ping(ctx).Err(); pingErr == nil {
				piiRedisGuard = circuitbreaker.NewRedisBreaker(client, 200*time.Millisecond, gobreaker.Settings{})
			}
			cancel()
		}
		if piiRedisGuard == nil {
			slog.Warn("PII Redis not available, cross-request unmask degraded", "url", cfg.PII.RedisURL)
		}
	}

	if cfg.PII.Enabled {
		a.piiMaskerMiddleware = iltermiddleware.NewPIIMaskerMiddleware(a.store, cfg.PII, piiRedisGuard, a.cfgCache)
		if a.semanticCacheMiddleware != nil {
			a.semanticCacheMiddleware.SetPIIMasker(a.piiMaskerMiddleware.Masker())
		}
	}

	// Always construct the guardrails middleware — like rate-limit/PII/budget,
	// its actual enforcement is gated at request time by the live config
	// snapshot (m.enabled, refreshed on every cfgCache change), so the
	// "Guardrails" feature toggle in the dashboard can turn it on/off at
	// runtime without a restart. Gating construction on the boot-time
	// cfg.Guardrails.Enabled value (which has no env var and defaults to
	// false) made that toggle a no-op.
	a.guardrailsMiddleware, err = iltermiddleware.NewGuardrailsMiddleware(a.cfgCache, slog.Default())
	if err != nil {
		slog.Error("Failed to initialize guardrails middleware", "error", err)
	} else {
		a.guardrailsMiddleware.SetStore(a.store)
	}

	a.initMCP()
	a.initJobs()
	a.initOTel()

	// Auto-sync MCP servers and OpenAPI specs in background on startup
	go func() {
		syncCtx, syncCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer syncCancel()

		if a.mcpHandler != nil {
			a.mcpHandler.SyncAllEnabledServers(syncCtx)
		}
		if a.openapiHandler != nil {
			a.openapiHandler.SyncOperationsFromProvider(syncCtx)
		}
	}()

	a.promptHandler = prompts.NewPromptHandler(a.store, a.auditor)
	a.promptMiddleware = iltermiddleware.NewPromptInjectionMiddleware(a.store)
	a.guardrailsHandler = dashboard.NewGuardrailsHandler(a.guardrailsMiddleware, a.store, a.cfg, a.cfgCache)

	oauthStore := mcp.NewOAuthStore(a.store)
	oauthCleanupCtx, oauthCleanupCancel := context.WithCancel(context.Background())
	defer oauthCleanupCancel()
	go oauthStore.StartCleanup(oauthCleanupCtx)
	a.oauthStore = oauthStore
	a.oauthCleanupCancel = oauthCleanupCancel

	oauthEndpoints := mcptransport.NewOAuthEndpoints(
		fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port),
		fmt.Sprintf("http://localhost:%d", cfg.Dashboard.Port),
		oauthStore,
		a.store,
		&cfg.MCP.OAuth,
	)

	r, chatChain := a.setupRouter()
	a.r = r
	a.proxyHandler.SetChatChain(chatChain)

	// Anthropic-native passthrough. Runs its own translation + re-enters
	// chatChain, which already carries auth/budget/PII/MCP/etc., so the full
	// chatMiddlewares stack must not be mounted here again (that would
	// double-charge budget and double-count rate limits). Auth alone is
	// cheap and idempotent (a key lookup, no counters), so it's applied here
	// too, to reject unauthenticated requests before the handler spends work
	// reading and translating the body.
	r.With(a.authMiddleware.Handler).Post("/v1/messages", a.proxyHandler.AnthropicMessages)
	r.With(a.authMiddleware.Handler).Post("/v1/completions", a.proxyHandler.LegacyCompletions)

	// OAuth PKCE endpoints (unprotected so VSCode can discover them).
	r.Get("/.well-known/oauth-protected-resource", oauthEndpoints.ProtectedResourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", oauthEndpoints.AuthorizationServerMetadata)
	r.Get("/authorize", oauthEndpoints.Authorize)
	r.Post("/authorize", oauthEndpoints.Authorize)
	r.Post("/token", oauthEndpoints.Token)
	r.Get("/register", oauthEndpoints.Register)
	r.Post("/register", oauthEndpoints.Register)

	if a.mcpGatewayHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(a.authMiddleware.Handler)
			r.Get(cfg.MCP.Endpoint, a.mcpGatewayHandler.ServeHTTP)
			r.Post(cfg.MCP.Endpoint, a.mcpGatewayHandler.ServeHTTP)
		})
	}

	if a.mcpHubHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(a.authMiddleware.Handler)
			r.Get(cfg.MCP.HubEndpoint, a.mcpHubHandler.ServeHTTP)
			r.Post(cfg.MCP.HubEndpoint, a.mcpHubHandler.ServeHTTP)
		})
	}

	return a.startListen(r)
}

func (a *App) initOTel() {
	cfg := a.cfg
	if err := iltermiddleware.InitMetrics(); err != nil {
		slog.Error("Failed to initialize metrics instruments", "error", err)
	}
	if err := mcp.InitMCPMetrics(); err != nil {
		slog.Error("Failed to initialize metrics instruments", "error", err)
	}
	if err := iltermiddleware.InitGuardrailsMetrics(); err != nil {
		slog.Error("Failed to initialize guardrails metrics", "error", err)
	}

	if cfg.Telemetry.Enabled && cfg.Telemetry.OTLPEndpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		tp, errTr := iltermiddleware.InitTracer(ctx, cfg.Telemetry.OTLPEndpoint, cfg.Telemetry.TraceSampling)
		cancel()
		if errTr != nil {
			slog.Error("Failed to initialize OTel Tracer", "error", errTr)
		} else if tp != nil {
			go func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()
				if errShut := tp.Shutdown(shutdownCtx); errShut != nil {
					slog.Error("OTel Tracer Shutdown failed", "error", errShut)
				}
			}()
		}
	}
}
