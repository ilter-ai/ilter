package dashboard

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/dashboard/access"
	"github.com/ilter-ai/ilter/internal/dashboard/auth"
	"github.com/ilter-ai/ilter/internal/dashboard/cache"
	dashconfig "github.com/ilter-ai/ilter/internal/dashboard/config"
	"github.com/ilter-ai/ilter/internal/dashboard/features"
	dashjobs "github.com/ilter-ai/ilter/internal/dashboard/jobs"
	dashmcp "github.com/ilter-ai/ilter/internal/dashboard/mcp"
	"github.com/ilter-ai/ilter/internal/dashboard/models"
	"github.com/ilter-ai/ilter/internal/dashboard/prompts"
	"github.com/ilter-ai/ilter/internal/dashboard/providers"
	"github.com/ilter-ai/ilter/internal/dashboard/ratelimit"
	dashsmartrouter "github.com/ilter-ai/ilter/internal/dashboard/smartrouter"
	"github.com/ilter-ai/ilter/internal/dashboard/stats"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/web"
)

type Server struct {
	cfg                  *config.Config
	configCache          *config.Cache
	store                *db.SQLiteStore
	lb                   *smartrouter.LoadBalancer
	reg                  *provider.Registry
	guardrailsMiddleware *middleware.GuardrailsMiddleware
	piiMasker            *middleware.PIIMaskerMiddleware
	adminHandler         *access.Handler
	mcpHandler           *dashmcp.MCPHandler
	accessHandler        *access.Handler
	openapiHandler       *OpenAPIHandler
	promptHandler        *prompts.PromptHandler
	guardrailsHandler    *GuardrailsHandler
	jobsHandler          *dashjobs.JobsHandler
	chatChain            http.Handler
	configAPIHandler     *dashconfig.ConfigAPIHandler
	srv                  *http.Server
	redis                *redis.Client
	cacheClient          *redis.Client
	semanticCache        *middleware.SemanticCacheMiddleware
	configAuditor        *audit.SQLiteConfigAuditor

	// Dashboard subpackage handlers
	providersHandler   *providers.Handler
	authHandler        *auth.Handler
	modelsHandler      *models.Handler
	piiHandler         *PIIHandler
	ratelimitHandler   *ratelimit.Handler
	requestsHandler    *RequestsHandler
	featuresHandler    *features.Handler
	statsHandler       *stats.Handler
	cacheHandler       *cache.Handler
	smartrouterHandler *dashsmartrouter.Handler
	chatHandler        *ChatHandler
}

type Option func(*Server)

func WithGuardrailsMiddleware(m *middleware.GuardrailsMiddleware) Option {
	return func(s *Server) { s.guardrailsMiddleware = m }
}

func WithPIIMasker(m *middleware.PIIMaskerMiddleware) Option {
	return func(s *Server) { s.piiMasker = m }
}

func WithChatChain(chain http.Handler) Option {
	return func(s *Server) { s.chatChain = chain }
}

func WithSemanticCache(sc *middleware.SemanticCacheMiddleware) Option {
	return func(s *Server) {
		s.semanticCache = sc
		if s.cacheHandler != nil {
			s.cacheHandler.SetSemanticCacheMiddleware(sc)
		}
	}
}

func WithCacheClient(client *redis.Client) Option {
	return func(s *Server) { s.cacheClient = client }
}

func WithConfigAPIHandler(h *dashconfig.ConfigAPIHandler) Option {
	return func(s *Server) { s.configAPIHandler = h }
}

func WithJobsHandler(h *dashjobs.JobsHandler) Option {
	return func(s *Server) { s.jobsHandler = h }
}

func WithAccessHandler(h *access.Handler) Option {
	return func(s *Server) { s.accessHandler = h }
}

func WithOpenAPIHandler(h *OpenAPIHandler) Option {
	return func(s *Server) { s.openapiHandler = h }
}

func WithAdminHandler(h *access.Handler) Option {
	return func(s *Server) { s.adminHandler = h }
}

func WithMCPHandler(h *dashmcp.MCPHandler) Option {
	return func(s *Server) { s.mcpHandler = h }
}

func WithPromptHandler(h *prompts.PromptHandler) Option {
	return func(s *Server) { s.promptHandler = h }
}

func WithGuardrailsHandler(h *GuardrailsHandler) Option {
	return func(s *Server) { s.guardrailsHandler = h }
}

func WithCooldownStore(cs cooldown.Store) Option {
	return func(s *Server) {
		if s.featuresHandler != nil {
			s.featuresHandler.SetCooldownStore(cs)
		}
	}
}

func WithLoopDetector(d *loopdetect.Detector) Option {
	return func(s *Server) {
		if s.featuresHandler != nil {
			s.featuresHandler.SetLoopDetector(d)
		}
	}
}

func WithConfigAuditor(a *audit.SQLiteConfigAuditor) Option {
	return func(s *Server) { s.configAuditor = a }
}

func NewServer(cfg *config.Config, configCache *config.Cache, store *db.SQLiteStore, lb *smartrouter.LoadBalancer, reg *provider.Registry, opts ...Option) *Server {
	s := &Server{
		cfg:         cfg,
		configCache: configCache,
		store:       store,
		lb:          lb,
		reg:         reg,
	}
	if cfg.RateLimit.Enabled && cfg.RateLimit.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RateLimit.RedisURL)
		if err == nil {
			s.redis = redis.NewClient(opt)
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := s.redis.Ping(pingCtx).Err(); err != nil {
				slog.Warn("Dashboard Redis connection failed, degradation enabled", "error", err)
			}
			pingCancel()
		}
	}

	for _, opt := range opts {
		opt(s)
	}

	s.authHandler = auth.NewAuthHandler(store, cfg)
	s.providersHandler = providers.NewHandler(store, cfg, lb)
	s.modelsHandler = models.NewModelsHandler(store, cfg, lb)
	s.piiHandler = NewPIIHandler(store, configCache)
	s.ratelimitHandler = ratelimit.NewRateLimitHandler(store, cfg, configCache, s.redis)
	s.requestsHandler = NewRequestsHandler(store)
	s.featuresHandler = features.NewFeaturesHandler(store, cfg, configCache, s.configAuditor, s.redis, s.cacheClient)
	s.statsHandler = stats.NewStatsHandler(store, cfg, configCache, reg, lb, s.configAuditor)
	s.cacheHandler = cache.NewCacheHandler(store, cfg, configCache, s.redis, s.cacheClient)
	s.smartrouterHandler = dashsmartrouter.NewHandler(store, cfg, reg, configCache)
	if s.guardrailsHandler == nil {
		s.guardrailsHandler = NewGuardrailsHandler(nil, store, cfg, configCache)
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		model.WriteJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is accepted")
		return
	}
	if s.chatChain == nil {
		model.WriteJSONError(w, http.StatusServiceUnavailable, "chat_unavailable", "Chat completions endpoint is not configured")
		return
	}

	r.Header.Set("Authorization", "Bearer "+s.cfg.Auth.AdminKey)
	r.URL.Path = "/v1/chat/completions"
	s.chatChain.ServeHTTP(w, r)
}

func (s *Server) BuildServer() (*http.Server, error) {
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)

	authMw := s.makeAuthMiddleware()

	_ = mime.AddExtensionType(".css", "text/css")
	_ = mime.AddExtensionType(".js", "application/javascript")

	subFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("dashboard static files: %w", err)
	}
	fileServer := http.FileServer(http.FS(subFS))

	r.Post("/api/auth/login", s.authHandler.HandleLogin)
	r.Post("/api/auth/user-login", s.authHandler.HandleUserLogin)

	if s.jobsHandler != nil {
		r.Post("/api/webhooks/{token}", s.jobsHandler.WebhookHandler)
	}

	r.Route("/api", func(r chi.Router) {
		r.Use(authMw)
		s.registerCoreAnalyticsRoutes(r)
		s.registerSmartRouterRoutes(r)
		s.registerGuardrailRoutes(r)
		s.registerPromptRoutes(r)
		s.registerMCProutes(r)
		s.registerOpenAPIRoutes(r)
		s.registerAdminRoutes(r)
		s.registerRateLimitRoutes(r)
		s.registerBudgetRoutes(r)
		s.registerCircuitBreakerRoutes(r)
		s.registerSemanticCacheRoutes(r)
		s.registerFeatureRoutes(r)
		s.registerConfigAPIRoutes(r)
		s.registerAccessRoutes(r)
		s.registerJobsRoutes(r)
		s.chatHandler = NewChatHandler(s.store)
		s.registerChatRoutes(r)
	})

	s.registerSPARoutes(r, fileServer, s.makePageAuthMiddleware())

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.Dashboard.Port),
		Handler: r,
	}

	return s.srv, nil
}

// requestToken extracts the auth token from the Authorization header,
// admin-key headers, or the token/user_token cookies, in that order.
func requestToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	token := ""
	if rest, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		token = rest
	} else if authHeader != "" {
		token = authHeader
	}
	if token == "" {
		token = r.Header.Get("X-Admin-Token")
	}
	if token == "" {
		token = r.Header.Get("X-Admin-Key")
	}
	if token == "" {
		token = r.Header.Get("x-api-key")
	}
	if token == "" {
		if cookie, err := r.Cookie("token"); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		if cookie, err := r.Cookie("user_token"); err == nil {
			token = cookie.Value
		}
	}
	return token
}

// isAuthorized reports whether token is a valid dashboard/admin credential:
// the configured dashboard auth token, the admin key, an "admin"-group API
// key, or a valid user JWT.
func (s *Server) isAuthorized(token string) bool {
	if token == "" {
		return false
	}

	dashToken := s.cfg.Dashboard.AuthToken
	adminKey := s.cfg.Auth.AdminKey
	if (dashToken != "" && token == dashToken) ||
		(adminKey != "" && token == adminKey) {
		return true
	}

	if s.store != nil && s.store.IsAdminAPIKey(token) {
		return true
	}

	jwtSecret := s.cfg.Dashboard.UserAuthJWTSecret
	if jwtSecret == "" {
		jwtSecret = "user-auth-dev-secret"
	}
	parsedToken, err := jwt.Parse(token, func(_ *jwt.Token) (any, error) {
		return []byte(jwtSecret), nil
	})
	return err == nil && parsedToken.Valid
}

// makeAuthMiddleware protects JSON API routes: unauthenticated or invalid
// requests get a 401 JSON error, as expected by API/XHR clients.
func (s *Server) makeAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := requestToken(r)
			if token == "" {
				model.WriteJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication required. Please provide a valid token via Authorization header, X-Admin-Token header, X-Admin-Key header, x-api-key header, or token cookie")
				return
			}
			if s.isAuthorized(token) {
				next.ServeHTTP(w, r)
				return
			}
			model.WriteJSONError(w, http.StatusUnauthorized, "invalid_token", "The provided token is invalid or expired. Please check your credentials and try again")
		})
	}
}

// makePageAuthMiddleware protects SPA page routes: unauthenticated or
// invalid requests are redirected to the login page (a browser navigating
// to a protected page can't do anything useful with a raw JSON error).
func (s *Server) makePageAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.isAuthorized(requestToken(r)) {
				next.ServeHTTP(w, r)
				return
			}
			redirectTo := "/login?redirect=" + url.QueryEscape(r.URL.Path)
			http.Redirect(w, r, redirectTo, http.StatusFound)
		})
	}
}

// delegateHandler provides nil-safe handler delegation with a standard 503 error message.
func (s *Server) delegateHandler(handler http.HandlerFunc, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if handler != nil {
			handler(w, r)
		} else {
			model.WriteJSONError(w, http.StatusServiceUnavailable, "service_unavailable", name+" handler not configured")
		}
	}
}
