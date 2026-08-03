package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/pii"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	iltermiddleware "github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/logging"
	"github.com/ilter-ai/ilter/internal/provider"
)

type redisLogger struct{}

func (redisLogger) Printf(ctx context.Context, format string, v ...any) {
	slog.DebugContext(ctx, fmt.Sprintf(format, v...), "component", "redis")
}

func (a *App) initStore() error {
	cfg := a.cfg
	if cfg.Storage.Type == "sqlite" && cfg.Storage.SqlitePath != "" && !filepath.IsAbs(cfg.Storage.SqlitePath) {
		if abs, err := filepath.Abs(cfg.Storage.SqlitePath); err == nil {
			cfg.Storage.SqlitePath = abs
		}
	}
	if cfg.Storage.Type == "sqlite" && cfg.Storage.SqlitePath != ":memory:" {
		if _, err := os.Stat(cfg.Storage.SqlitePath); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to access database at %q: %w", cfg.Storage.SqlitePath, err)
			}
			// No DB yet. That's fine if the operator bootstrapped via env
			// (admin break-glass key + at least one provider key) — the
			// store below creates the file and runs migrations on its own.
			// Otherwise there's no way to authenticate or route requests,
			// so fail fast instead of serving a useless empty gateway.
			if !config.AdminKeyEnv.WasSet() || !config.AnyProviderKeyEnvSet() {
				return fmt.Errorf(
					"database not found at %q: run 'ilter init' first, or set %s and at least one provider key (e.g. %s) to boot from env",
					cfg.Storage.SqlitePath, "ILTER_ADMIN_API_KEY", config.ProviderKeyEnv("opencode_go"),
				)
			}
		}
	}
	store, err := db.NewSQLiteStore(cfg.Storage)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	db.InitConfigResolvers(store)

	cfg.Providers = loadProvidersFromDB(store)
	config.EnrichConfig(cfg)
	config.ResolveProviderKeys(cfg, slog.Default())

	if err := pii.LoadPatternsFromDB(store.DB); err != nil {
		slog.Warn("failed to load PII patterns from DB", "error", err)
	}

	a.store = store
	return nil
}

func loadProvidersFromDB(store *db.SQLiteStore) []config.ProviderConfig {
	entries, err := store.GetBySection("provider")
	if err != nil {
		slog.Warn("no providers in runtime_config (run 'ilter init' first)", "error", err)
		return nil
	}

	var providers []config.ProviderConfig
	for name, raw := range entries {
		var reg model.ProviderRegistration
		if err := json.Unmarshal([]byte(raw), &reg); err != nil {
			slog.Warn("failed to parse provider from runtime_config", "name", name, "error", err)
			continue
		}
		providers = append(providers, providerRegToConfig(reg))
	}
	return providers
}

func providerRegToConfig(reg model.ProviderRegistration) config.ProviderConfig {
	return config.ProviderConfig{
		Name:            reg.Name,
		Type:            reg.Provider,
		BaseURL:         reg.BaseURL,
		APIKey:          reg.APISecretKey,
		Timeout:         reg.Timeout,
		MaxRetries:      reg.MaxRetries,
		Headers:         reg.Headers,
		DiscoveryPublic: reg.DiscoveryPublic,
		CircuitBreaker: config.CircuitBreakerConfig{
			MaxFailures:         reg.CircuitBreaker.MaxFailures,
			Timeout:             reg.CircuitBreaker.Timeout,
			HalfOpenMaxRequests: reg.CircuitBreaker.HalfOpenMaxRequests,
		},
	}
}

func (a *App) setupLogging() {
	cfg := a.cfg
	level := slog.LevelInfo
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Logging.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = logging.NewPrettyHandler(os.Stdout, *opts)
	}
	slog.SetDefault(slog.New(handler))
	redis.SetLogger(redisLogger{})

	slog.Info("starting proxy", "addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port), "dashboard_port", cfg.Dashboard.Port)
}

func (a *App) initRedis() *circuitbreaker.RedisBreaker {
	cfg := a.cfg
	redisURL := ""
	if cfg.RateLimit.Enabled && cfg.RateLimit.RedisURL != "" {
		redisURL = cfg.RateLimit.RedisURL
	} else if cfg.Cache.Enabled && cfg.Cache.RedisURL != "" {
		redisURL = cfg.Cache.RedisURL
	} else if cfg.Budget.Enabled && cfg.RateLimit.RedisURL != "" {
		redisURL = cfg.RateLimit.RedisURL
	}

	if redisURL == "" {
		return nil
	}

	redisOpts, errParse := redis.ParseURL(redisURL)
	if errParse != nil {
		slog.Warn("Failed to parse Redis URL, features will degrade gracefully", "error", errParse)
		return nil
	}

	client := redis.NewClient(redisOpts)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if errPing := client.Ping(ctx).Err(); errPing != nil {
		slog.Warn("Failed to connect to Redis, features will degrade gracefully", "error", errPing)
		return nil
	}
	return circuitbreaker.NewRedisBreaker(client, 200*time.Millisecond, gobreaker.Settings{})
}

func (a *App) initMiddleware(rg, cacheGuard *circuitbreaker.RedisBreaker) {
	cfg := a.cfg
	store := a.store

	a.authMiddleware = iltermiddleware.NewAuthMiddleware(cfg.Auth, store).WithMCPEndpoint(cfg.MCP.Endpoint)

	rlMw, err := iltermiddleware.NewRateLimitMiddleware(&cfg.RateLimit, rg, a.cfgCache)
	if err != nil {
		slog.Error("Failed to initialize rate limiter middleware", "error", err)
	}
	a.rateLimitMiddleware = rlMw

	a.budgetMiddleware = iltermiddleware.NewBudgetMiddleware(cfg.Budget, rg, store, a.cfgCache)
	a.semanticCacheMiddleware = iltermiddleware.NewSemanticCacheMiddleware(cfg.Cache, cacheGuard, a.cfgCache)

	a.auditLoggerMiddleware = iltermiddleware.NewAuditLoggerMiddleware(store)
}

func initLoadBalancer(cfg *config.Config, reg *provider.Registry, store *db.SQLiteStore, cfgCache *config.Cache) (*smartrouter.LoadBalancer, *loopdetect.Detector, error) {
	lb, err := smartrouter.NewLoadBalancer(cfg, reg, cfgCache)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize load balancer: %w", err)
	}
	if errRoutes := lb.LoadRoutesFromDB(func(provider string) ([]smartrouter.ProviderModelEntry, error) {
		models, errQ := store.GetProviderModels(provider)
		if errQ != nil {
			return nil, errQ
		}
		entries := make([]smartrouter.ProviderModelEntry, len(models))
		for i, m := range models {
			entries[i] = smartrouter.ProviderModelEntry{
				Name:    m.Model,
				Active:  m.Active,
				Tier:    m.Tier,
				CostIn:  m.CostIn,
				CostOut: m.CostOut,
			}
		}
		return entries, nil
	}); errRoutes != nil {
		return nil, nil, fmt.Errorf("failed to load routes from DB: %w", errRoutes)
	}
	slog.Info("routes loaded")
	if inactiveModels, errDb := store.GetInactiveModels(); errDb == nil {
		lb.SetInactiveModels(inactiveModels)
	}
	loopSettings := config.LoopSettingsWithDefaults(cfg.CostGuard.LoopSettings)
	if overrides, errDb := store.GetBySection("loop_settings"); errDb == nil {
		loopSettings = config.ApplyLoopSettingsOverrides(loopSettings, overrides)
	}
	cfg.CostGuard.LoopSettings = loopSettings
	loopDetector := loopdetect.NewDetector(loopSettings)
	return lb, loopDetector, nil
}
