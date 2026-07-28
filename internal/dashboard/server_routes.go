package dashboard

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/web"
)

// ---------------------------------------------------------------------------
// API route registrations (each called within the /api group, authMw active)
// ---------------------------------------------------------------------------

func (s *Server) registerCoreAnalyticsRoutes(r chi.Router) {
	r.Get("/analytics/overview", s.delegateHandler(s.statsHandler.HandleAnalyticsOverview, "Stats"))
	r.Get("/stats", s.delegateHandler(s.statsHandler.HandleStats, "Stats"))
	r.Get("/requests/{id}", s.requestsHandler.HandleRequestDetail)
	r.Get("/requests", s.requestsHandler.HandleListRequests)
	r.Get("/costs", s.statsHandler.HandleCostsOverview)
	r.Get("/costs/by-provider", s.statsHandler.HandleCostsOverview)
	r.Get("/costs/by-key", s.statsHandler.HandleCostsByKey)
	r.Get("/models", s.delegateHandler(s.modelsHandler.HandleModels, "Models"))
	r.Post("/models/toggle", s.delegateHandler(s.modelsHandler.HandleToggleModel, "Models"))
	r.Post("/models/tier", s.delegateHandler(s.modelsHandler.HandleUpdateModelTier, "Models"))
	r.Patch("/models/{id}", s.modelsHandler.HandleUpdateModelByID)
	r.Get("/loops", s.delegateHandler(s.piiHandler.HandleLoops, "PII"))
	r.Get("/loop-export", s.delegateHandler(s.piiHandler.HandleLoopExport, "PII"))
	r.Get("/loop-settings", s.delegateHandler(s.featuresHandler.HandleLoopSettings, "Features"))
	r.Put("/loop-settings", s.delegateHandler(s.featuresHandler.HandleLoopSettings, "Features"))
	r.Get("/pii-events", s.delegateHandler(s.piiHandler.HandlePIIEvents, "PII"))
	r.Get("/pii-stats", s.delegateHandler(s.piiHandler.HandleStats, "PII"))
	r.Get("/pii-export", s.delegateHandler(s.piiHandler.HandlePIIExport, "PII"))
	r.Get("/pii/config", s.piiHandler.HandlePIIConfig)
	r.Post("/pii/config", s.piiHandler.HandlePIIConfig)
	r.Get("/pii/patterns", s.delegateHandler(s.piiHandler.HandleListPatterns, "PII"))
	r.Post("/pii/patterns", s.delegateHandler(s.piiHandler.HandleCreatePattern, "PII"))
	r.Put("/pii/patterns/{name}", s.delegateHandler(s.piiHandler.HandleUpdatePattern, "PII"))
	r.Delete("/pii/patterns/{name}", s.delegateHandler(s.piiHandler.HandleDeletePattern, "PII"))
	r.Post("/pii/patterns/reload", s.delegateHandler(s.piiHandler.HandleReloadPatterns, "PII"))
	r.Get("/providers", s.providersHandler.HandleProviders)
	r.Post("/providers", s.smartrouterHandler.HandleUpdateProvider)
	r.Post("/optimize", s.delegateHandler(s.smartrouterHandler.HandleOptimize, "Smart router"))
	r.Get("/insights/top-expensive", s.statsHandler.HandleTopExpensiveRequests)
	r.Get("/insights/cost-trend", s.statsHandler.HandleCostTrend)
	r.Get("/insights/cost-by-model", s.statsHandler.HandleCostByModel)
	r.Get("/insights/savings-opportunity", s.statsHandler.HandleSavingsOpportunity)
}

func (s *Server) registerSmartRouterRoutes(r chi.Router) {
	mountRoutes := func(prefix string) {
		r.Get(prefix+"/stats", s.delegateHandler(s.smartrouterHandler.HandleSmartRouterStats, "Smart router"))
		r.Get(prefix+"/history", s.delegateHandler(s.smartrouterHandler.HandleSmartRouterHistory, "Smart router"))
		r.Get(prefix+"/strategies", s.smartrouterHandler.HandleListStrategies)
		r.Get(prefix+"/strategies/{name}", s.smartrouterHandler.HandleGetStrategy)
		r.Put(prefix+"/strategies/{name}", s.smartrouterHandler.HandleSetStrategy)
		r.Delete(prefix+"/strategies/{name}", s.smartrouterHandler.HandleDeleteStrategy)
		r.Get(prefix+"/active", s.smartrouterHandler.HandleGetActiveStrategy)
		r.Put(prefix+"/active", s.smartrouterHandler.HandleSetActiveStrategy)
		r.Get(prefix, s.smartrouterHandler.HandleSmartRouterUnified)
	}
	mountRoutes("/smart-loadbalancer")
	mountRoutes("/smart-router")
}

func (s *Server) registerGuardrailRoutes(r chi.Router) {
	r.Get("/guardrails/violations", s.delegateHandler(s.guardrailsHandler.HandleGuardrailViolations, "Guardrails"))
	r.Get("/guardrails/summary", s.delegateHandler(s.guardrailsHandler.HandleGuardrailSummary, "Guardrails"))
	r.Get("/guardrails/export", s.guardrailsHandler.HandleGuardrailExport)
	r.Get("/guardrails", s.guardrailsHandler.HandleAdminGuardrails)
	r.Post("/guardrails/rules", s.guardrailsHandler.HandleCreateGuardrailRule)
	r.Put("/guardrails/rules/{id}", s.guardrailsHandler.HandleUpdateGuardrailRule)
	r.Delete("/guardrails/rules/{id}", s.guardrailsHandler.HandleDeleteGuardrailRule)
	r.Patch("/guardrails/{id}", s.guardrailsHandler.HandleToggleGuardrail)
	if s.guardrailsHandler != nil {
		r.Post("/guardrails/test", s.guardrailsHandler.TestGuardrail)
		r.Get("/guardrails/stats", s.guardrailsHandler.GuardrailStats)
	}
}

func (s *Server) registerPromptRoutes(r chi.Router) {
	if s.promptHandler == nil {
		return
	}
	r.Get("/prompts", s.promptHandler.ListTemplates)
	r.Post("/prompts", s.promptHandler.CreateTemplate)
	r.Put("/prompts/{id}", s.promptHandler.UpdateTemplate)
	r.Delete("/prompts/{id}", s.promptHandler.DeleteTemplate)
	r.Get("/prompts/by-name/{name}", s.promptHandler.GetTemplateByName)
	r.Get("/prompts/{id}/versions", s.promptHandler.GetVersions)
}

func (s *Server) registerMCProutes(r chi.Router) {
	if s.mcpHandler == nil {
		return
	}
	r.Get("/mcp-servers", s.mcpHandler.ListServers)
	r.Get("/mcp-servers/{id}", s.mcpHandler.GetServer)
	r.Post("/mcp-servers", s.mcpHandler.CreateServer)
	r.Put("/mcp-servers/{id}", s.mcpHandler.UpdateServer)
	r.Delete("/mcp-servers/{id}", s.mcpHandler.DeleteServer)
	r.Post("/mcp-servers/{id}/test", s.mcpHandler.TestServer)
	r.Patch("/mcp-servers/{id}/toggle", s.mcpHandler.ToggleServer)
	r.Get("/mcp-servers/{id}/tools", s.mcpHandler.ListServerTools)
	r.Post("/mcp-servers/{id}/sync", s.mcpHandler.SyncServerTools)
	r.Post("/mcp-servers/{id}/tools/call", s.mcpHandler.CallServerTool)
	r.Get("/mcp/stats", s.mcpHandler.GetStats)
	r.Get("/mcp/audit", s.mcpHandler.GetAuditLog)
	r.Get("/mcp/grants", s.mcpHandler.ListAllGrants)
	r.Get("/mcp-servers/{id}/grants", s.mcpHandler.ListGrants)
	r.Post("/mcp-servers/{id}/grants", s.mcpHandler.CreateGrant)
	r.Delete("/mcp-servers/{id}/grants/{grantId}", s.mcpHandler.DeleteGrant)
}

func (s *Server) registerOpenAPIRoutes(r chi.Router) {
	if s.openapiHandler == nil {
		return
	}
	r.Get("/openapi-specs", s.openapiHandler.ListSpecs)
	r.Post("/openapi-specs", s.openapiHandler.CreateSpec)
	r.Put("/openapi-specs/{id}", s.openapiHandler.UpdateSpec)
	r.Patch("/openapi-specs/{id}/toggle", s.openapiHandler.ToggleSpec)
	r.Delete("/openapi-specs/{id}", s.openapiHandler.DeleteSpec)
	r.Post("/openapi-specs/{id}/validate", s.openapiHandler.ValidateSpec)
}

func (s *Server) registerAdminRoutes(r chi.Router) {
	if s.adminHandler == nil {
		return
	}
	r.Get("/users", s.adminHandler.ListUsers)
	r.Post("/users", s.adminHandler.CreateUser)
	r.Get("/users/{id}", s.adminHandler.GetUser)
	r.Put("/users/{id}", s.adminHandler.UpdateUser)
	r.Delete("/users/{id}", s.adminHandler.DeleteUser)
	r.Get("/groups", s.adminHandler.ListGroups)
	r.Post("/groups", s.adminHandler.CreateGroup)
	r.Get("/groups/{id}", s.adminHandler.GetGroup)
	r.Put("/groups/{id}", s.adminHandler.UpdateGroup)
	r.Delete("/groups/{id}", s.adminHandler.DeleteGroup)
	r.Post("/groups/{groupId}/members", s.adminHandler.AddMember)
	r.Delete("/groups/{groupId}/members/{userId}", s.adminHandler.RemoveMember)
	r.Get("/groups/{groupId}/members", s.adminHandler.ListMembers)
	r.Get("/users/{id}/groups", s.adminHandler.ListUserGroups)
	r.Post("/cache/flush", s.cacheHandler.CacheFlush)
	r.Post("/cache/mode", s.cacheHandler.HandleCacheModeToggle)
	r.Get("/api-keys", s.adminHandler.ListAPIKeys)
	r.Post("/api-keys", s.adminHandler.CreateAPIKey)
	r.Get("/api-keys/summary", s.adminHandler.GetAPIKeysSummary)
	r.Get("/api-keys/{id}", s.adminHandler.GetAPIKey)
	r.Put("/api-keys/{id}", s.adminHandler.UpdateAPIKey)
	r.Delete("/api-keys/{id}", s.adminHandler.DeleteAPIKey)
	r.Get("/api-keys/{id}/usage", s.adminHandler.GetAPIKeyUsage)
}

func (s *Server) registerRateLimitRoutes(r chi.Router) {
	r.Get("/rate-limits", s.ratelimitHandler.HandleRateLimits)
	r.Get("/rate-limits/user/{id}", s.ratelimitHandler.HandleGetUserRateLimit)
	r.Put("/rate-limits/user/{id}", s.ratelimitHandler.HandleSetUserRateLimit)
	r.Get("/rate-limits/group/{id}", s.ratelimitHandler.HandleGetGroupRateLimit)
	r.Put("/rate-limits/group/{id}", s.ratelimitHandler.HandleSetGroupRateLimit)
	r.Get("/rate-limits/key/{id}", s.ratelimitHandler.HandleGetKeyRateLimit)
	r.Put("/rate-limits/key/{id}", s.ratelimitHandler.HandleSetKeyRateLimit)
}

func (s *Server) registerBudgetRoutes(r chi.Router) {
	r.Get("/budget", s.handleBudgetSummary)
	r.Get("/budget/key/{id}", s.handleKeyBudget)
	r.Post("/budget/key/{id}", s.handleSetKeyBudget)
	r.Get("/budget/user/{id}", s.handleUserBudget)
	r.Post("/budget/user/{id}", s.handleSetUserBudget)
	r.Get("/budget/group/{id}", s.handleGroupBudget)
	r.Post("/budget/group/{id}", s.handleSetGroupBudget)
}

func (s *Server) registerCircuitBreakerRoutes(r chi.Router) {
	r.Get("/circuit-breaker/summary", s.statsHandler.HandleCircuitBreakerSummary)
	r.Post("/circuit-breaker/toggle", s.statsHandler.HandleCircuitBreakerToggle)
	r.Post("/circuit-breaker/reset", s.statsHandler.HandleCircuitBreakerReset)
	r.Post("/circuit-breaker/force-open", s.statsHandler.HandleCircuitBreakerForceOpen)
}

func (s *Server) registerSemanticCacheRoutes(r chi.Router) {
	r.Get("/semantic-cache/summary", s.cacheHandler.HandleSemanticCacheSummary)
}

func (s *Server) registerFeatureRoutes(r chi.Router) {
	r.Get("/features", s.featuresHandler.HandleFeatures)
	r.Post("/features/toggle", s.featuresHandler.HandleToggleFeature)
	r.Get("/fallback", s.featuresHandler.HandleGetFallback)
	r.Post("/fallback", s.featuresHandler.HandleUpdateFallback)
	r.Post("/fallback/toggle", s.featuresHandler.HandleToggleFallback)
	r.Post("/fallback/cooldown/clear", s.featuresHandler.HandleClearCooldown)
}

func (s *Server) registerConfigAPIRoutes(r chi.Router) {
	if s.configAPIHandler == nil {
		return
	}
	r.Get("/config/{section}", s.configAPIHandler.ListSection)
	r.Get("/config/{section}/{key}", s.configAPIHandler.GetItem)
	r.Post("/config/{section}", s.configAPIHandler.Create)
	r.Put("/config/{section}/{key}", s.configAPIHandler.Update)
	r.Delete("/config/{section}/{key}", s.configAPIHandler.Delete)
}

func (s *Server) registerAccessRoutes(r chi.Router) {
	if s.accessHandler == nil {
		return
	}
	r.Get("/access/mcp", s.accessHandler.ListAllGrants)
	r.Post("/access/mcp", s.accessHandler.CreateGrant)
	r.Post("/access/mcp/test-rule", s.accessHandler.TestRule)
	r.Get("/access/mcp/default-policy", s.accessHandler.GetDefaultPolicy)
	r.Put("/access/mcp/default-policy", s.accessHandler.SetDefaultPolicy)
	r.Get("/access/mcp/server/{serverId}", s.accessHandler.ListGrantsByServer)
	r.Get("/access/mcp/{id}", s.accessHandler.GetGrant)
	r.Put("/access/mcp/{id}", s.accessHandler.UpdateGrant)
	r.Patch("/access/mcp/{id}/toggle", s.accessHandler.ToggleGrant)
	r.Delete("/access/mcp/{id}", s.accessHandler.DeleteGrant)
	r.Get("/access/audit", s.accessHandler.ListConfigAuditLog)
}

func (s *Server) registerJobsRoutes(r chi.Router) {
	if s.jobsHandler == nil {
		return
	}
	r.Get("/jobs", s.jobsHandler.ListJobs)
	r.Post("/jobs", s.jobsHandler.CreateJob)
	r.Get("/jobs/{id}", s.jobsHandler.GetJob)
	r.Put("/jobs/{id}", s.jobsHandler.UpdateJob)
	r.Delete("/jobs/{id}", s.jobsHandler.DeleteJob)
	r.Post("/jobs/{id}/trigger", s.jobsHandler.TriggerJob)
	r.Get("/jobs/{id}/runs", s.jobsHandler.ListRuns)
	r.Get("/jobs/{id}/runs/{runId}", s.jobsHandler.GetRun)
	r.Get("/jobs/stats", s.jobsHandler.GetStats)
	r.Get("/jobs/{id}/triggers", s.jobsHandler.ListTriggers)
	r.Post("/jobs/{id}/triggers", s.jobsHandler.CreateTrigger)
	r.Get("/jobs/{id}/triggers/{triggerId}/reveal", s.jobsHandler.RevealTrigger)
	r.Delete("/triggers/{id}", s.jobsHandler.DeleteTrigger)
}

func (s *Server) registerChatRoutes(r chi.Router) {
	if s.chatChain == nil {
		return
	}
	r.Post("/chat/completions", s.handleChatCompletions)
	if s.chatHandler != nil {
		r.Get("/chat/threads", s.chatHandler.ListThreads)
		r.Post("/chat/threads", s.chatHandler.CreateThread)
		r.Get("/chat/threads/{id}", s.chatHandler.GetThread)
		r.Put("/chat/threads/{id}", s.chatHandler.UpdateThread)
		r.Delete("/chat/threads/{id}", s.chatHandler.DeleteThread)
		r.Post("/chat/threads/{id}/messages", s.chatHandler.AddMessage)
		r.Get("/chat/threads/{id}/messages", s.chatHandler.ListMessages)
	}
}

// registerSPARoutes registers SPA file serving routes with optional auth.
func (s *Server) registerSPARoutes(r chi.Router, fileServer http.Handler, authMw func(http.Handler) http.Handler) {
	subFS, _ := fs.Sub(web.DistFS, "dist")

	serveIndex := func(relativePath string) http.HandlerFunc {
		data, err := fs.ReadFile(subFS, relativePath)
		if err != nil {
			return func(w http.ResponseWriter, r *http.Request) {
				fileServer.ServeHTTP(w, r)
			}
		}
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		}
	}

	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		serveIndex("login/index.html")(w, r)
	})
	r.Get("/_astro/*", func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)
	})
	r.Get("/favicon.*", func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)
	})
	r.Get("/jobs", authMw(serveIndex("jobs/index.html")).ServeHTTP)
	r.Get("/jobs/*", authMw(serveIndex("jobs/index.html")).ServeHTTP)
	r.Get("/mcp/servers/*", authMw(serveIndex("mcp/servers/index.html")).ServeHTTP)
	r.Get("/openapi/specs/*", authMw(serveIndex("openapi/specs/index.html")).ServeHTTP)
	r.Get("/access/*", authMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(r.URL.Path, "/access/")
		cleanPath = strings.TrimSuffix(cleanPath, "/")
		if cleanPath == "" {
			cleanPath = "keys"
		}
		serveIndex("access/"+cleanPath+"/index.html")(w, r)
	})).ServeHTTP)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/overview/", http.StatusMovedPermanently)
	})
	notFoundHandler := authMw(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		cleanPath := strings.TrimPrefix(req.URL.Path, "/")
		cleanPath = strings.TrimSuffix(cleanPath, "/")
		if cleanPath == "" {
			cleanPath = "overview"
		}
		indexPath := cleanPath + "/index.html"
		if data, err := fs.ReadFile(subFS, indexPath); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		fileServer.ServeHTTP(w, req)
	}))
	r.NotFound(notFoundHandler.ServeHTTP)
}
