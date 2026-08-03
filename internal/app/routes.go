package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	iltermiddleware "github.com/ilter-ai/ilter/internal/middleware"
)

func (a *App) setupRouter() (*chi.Mux, http.Handler) {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(iltermiddleware.RequestLogger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "x-api-key", "anthropic-version"},
		ExposedHeaders:   []string{"Link", "X-Ilter-Model-Actual"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	var chatMiddlewares []func(http.Handler) http.Handler
	if a.cfg.Metrics.Enabled {
		chatMiddlewares = append(chatMiddlewares, iltermiddleware.ObservabilityHandler)
	}
	chatMiddlewares = append(chatMiddlewares, a.authMiddleware.Handler)
	if a.rateLimitMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.rateLimitMiddleware.Handler)
	}
	if a.budgetMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.budgetMiddleware.Handler)
	}
	chatMiddlewares = append(chatMiddlewares, a.promptMiddleware.Handler)
	if a.cfg.PII.Enabled && a.piiMaskerMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.piiMaskerMiddleware.Handler)
	}
	if a.guardrailsMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.guardrailsMiddleware.Handler)
	}
	if a.mcpInjectMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.mcpInjectMiddleware.Handler)
	}
	if a.smartRouterMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.smartRouterMiddleware.Handler)
	}
	if a.loopDetectMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.loopDetectMiddleware.Handler)
	}
	if a.semanticCacheMiddleware != nil {
		chatMiddlewares = append(chatMiddlewares, a.semanticCacheMiddleware.Handler)
	}

	chatChain := chi.Chain(chatMiddlewares...).Handler(http.HandlerFunc(a.proxyHandler.ChatCompletions))

	r.Group(func(r chi.Router) {
		for _, mw := range chatMiddlewares {
			r.Use(mw)
		}
		r.Post("/v1/chat/completions", a.proxyHandler.ChatCompletions)
		r.Get("/v1/models", a.proxyHandler.Models)
	})

	// Embeddings and rerank aren't conversational: PII masking, prompt
	// injection, guardrails, MCP tool injection, semantic cache, and loop
	// detection don't apply. Auth/rate-limit/budget still do.
	var dataMiddlewares []func(http.Handler) http.Handler
	if a.cfg.Metrics.Enabled {
		dataMiddlewares = append(dataMiddlewares, iltermiddleware.ObservabilityHandler)
	}
	dataMiddlewares = append(dataMiddlewares, a.authMiddleware.Handler)
	if a.rateLimitMiddleware != nil {
		dataMiddlewares = append(dataMiddlewares, a.rateLimitMiddleware.Handler)
	}
	if a.budgetMiddleware != nil {
		dataMiddlewares = append(dataMiddlewares, a.budgetMiddleware.Handler)
	}

	r.Group(func(r chi.Router) {
		for _, mw := range dataMiddlewares {
			r.Use(mw)
		}
		r.Post("/v1/embeddings", a.proxyHandler.Embeddings)
		r.Post("/v1/rerank", a.proxyHandler.Rerank)
	})

	return r, chatChain
}
