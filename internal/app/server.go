package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/dashboard"
	iltermiddleware "github.com/ilter-ai/ilter/internal/middleware"
)

// startDashboard launches the dashboard server in a goroutine.
func (a *App) startDashboard() *http.Server {
	if !a.cfg.Dashboard.Enabled {
		return nil
	}
	opts := []dashboard.Option{
		dashboard.WithAdminHandler(a.adminHandler),
		dashboard.WithMCPHandler(a.mcpHandler),
		dashboard.WithPromptHandler(a.promptHandler),
		dashboard.WithGuardrailsHandler(a.guardrailsHandler),
		dashboard.WithJobsHandler(a.jobsHandler),
		dashboard.WithOpenAPIHandler(a.openapiHandler),
	}
	if a.guardrailsMiddleware != nil {
		opts = append(opts, dashboard.WithGuardrailsMiddleware(a.guardrailsMiddleware))
	}
	if a.piiMaskerMiddleware != nil {
		opts = append(opts, dashboard.WithPIIMasker(a.piiMaskerMiddleware))
	}
	if a.proxyHandler != nil {
		if chain := a.proxyHandler.ChatChain(); chain != nil {
			opts = append(opts, dashboard.WithChatChain(chain))
		}
	}
	if a.semanticCacheMiddleware != nil {
		opts = append(opts, dashboard.WithSemanticCache(a.semanticCacheMiddleware))
	}
	if a.cacheGuard != nil {
		opts = append(opts, dashboard.WithCacheClient(a.cacheGuard.Client()))
	}
	if a.accessHandler != nil {
		opts = append(opts, dashboard.WithAccessHandler(a.accessHandler))
	}
	if a.cooldownStore != nil {
		opts = append(opts, dashboard.WithCooldownStore(a.cooldownStore))
	}
	if a.loopDetector != nil {
		opts = append(opts, dashboard.WithLoopDetector(a.loopDetector))
	}
	if a.auditor != nil {
		opts = append(opts, dashboard.WithConfigAuditor(a.auditor))
	}
	dashSrv := dashboard.NewServer(a.cfg, a.cfgCache, a.store, a.lb, a.reg, opts...)
	srv, err := dashSrv.BuildServer()
	if err != nil {
		slog.Error("Dashboard server build failed", "error", err)
		return nil
	}
	go func() {
		slog.Debug("starting dashboard server", "port", a.cfg.Dashboard.Port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("Dashboard server failed", "error", err)
		}
	}()
	return srv
}

// gracefulListen starts the proxy server and waits for SIGINT to shut down
// all servers cleanly with a shared timeout context.
func gracefulListen(servers []*http.Server, cfg *config.Config) error {
	if len(servers) == 0 {
		return fmt.Errorf("no servers to listen on")
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		slog.Info("shutting down")

		shutdownTimeout := cfg.Server.GracefulShutdown
		if shutdownTimeout == 0 {
			shutdownTimeout = 15 * time.Second
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		for i, s := range servers {
			if err := s.Shutdown(shutdownCtx); err != nil {
				slog.Error("Server Shutdown failed", "index", i, "addr", s.Addr, "error", err)
			}
		}
		close(idleConnsClosed)
	}()

	if err := servers[0].ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server ListenAndServe failed: %w", err)
	}

	<-idleConnsClosed
	slog.Info("server stopped")
	return nil
}

// startListen creates the HTTP server, starts the dashboard and metrics servers,
// registers job runner cleanup, and enters gracefulListen.
func (a *App) startListen(r *chi.Mux) error {
	cfg := a.cfg

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	servers := []*http.Server{srv}

	if dashSrv := a.startDashboard(); dashSrv != nil {
		servers = append(servers, dashSrv)
	}

	if cfg.Metrics.Enabled && iltermiddleware.Handler != nil {
		mmux := http.NewServeMux()
		mmux.Handle(cfg.Metrics.Path, iltermiddleware.Handler)
		mmux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		ln, err := net.Listen("tcp", cfg.Metrics.ListenAddr)
		if err != nil {
			return fmt.Errorf("metrics listen %s: %w", cfg.Metrics.ListenAddr, err)
		}
		metricsSrv := &http.Server{Handler: mmux}
		go func() {
			if err := metricsSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server stopped", "err", err)
			}
		}()
		slog.Info("metrics server started", "addr", cfg.Metrics.ListenAddr, "path", cfg.Metrics.Path)
		servers = append(servers, metricsSrv)
	}

	if a.jobRunner != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			a.jobRunner.StopAccepting()
			if code := a.jobRunner.Drain(ctx); code != 0 {
				slog.Error("Job runner drain timeout, some runs may be lost")
			}
		}()
	}

	return gracefulListen(servers, cfg)
}
