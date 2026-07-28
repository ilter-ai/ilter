package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ilter-ai/ilter/internal/config/openapi"
	dashopenapi "github.com/ilter-ai/ilter/internal/dashboard"
	"github.com/ilter-ai/ilter/internal/features/mcp"
	iltermiddleware "github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	mcptransport "github.com/ilter-ai/ilter/internal/platform/transport/mcp"
)

// initMCP initializes the MCP gateway, OpenAPI tool provider, injection middleware, and hub.
func (a *App) initMCP() {
	cfg := a.cfg

	mcpRegistry, err := mcp.NewRegistryFromCache(a.cfgCache.Get().MCPServers(), a.store)
	if err != nil {
		slog.Error("Failed to initialize MCP registry", "error", err)
		return
	}
	a.mcpHandler.SetRegistry(mcpRegistry)
	mcpAuthorizer := mcp.NewAuthorizer(a.store, nil, cfg.MCP.DefaultPolicy)
	mcpClients := mcp.NewClientManager(mcpRegistry)
	a.mcpExecutor = mcp.NewExecutor(mcpRegistry, mcpClients, mcpAuthorizer, a.mcpAuditLogger, a.store.DB)
	mcpGateway := mcp.NewGateway(mcpRegistry, mcpAuthorizer, a.mcpAuditLogger, a.store, &cfg.MCP, a.mcpExecutor)
	mcpGateway.SetConfigCache(a.cfgCache)

	a.mcpGatewayHandler = mcptransport.NewGatewayHandler(mcpGateway)
	a.mcpGatewayHandler.SetConfigCache(a.cfgCache)

	mcpInjector := mcp.NewInjector(mcpRegistry, mcpAuthorizer, a.store)
	mcpToolCallExecutor := mcp.NewToolCallExecutor(a.mcpExecutor)

	slog.Info(
		"MCP Gateway initialized",
		"endpoint", cfg.MCP.Endpoint,
		"servers", len(mcpRegistry.ListServers()),
		"injection", cfg.MCP.Injection.Enabled,
	)

	if cfg.MCP.HubEndpoint != "" {
		a.mcpHubHandler = mcptransport.NewHubHandler(
			mcp.NewHub(mcpRegistry, mcpAuthorizer, a.mcpExecutor, a.store, &cfg.MCP),
			mcp.NewSessionManager(),
		)
		slog.Info("MCP Hub enabled", "hub_endpoint", cfg.MCP.HubEndpoint)
	}

	// Initialize OpenAPI ToolProvider from database
	var openapiProvider *openapi.ToolProvider
	openAPISpecs, specErr := dashopenapi.LoadEnabledOpenAPISpecs(a.store)
	if specErr != nil {
		slog.Warn("Failed to load OpenAPI specs for tool injection", "error", specErr)
	} else {
		provider, initErr := openapi.NewToolProvider(openAPISpecs)
		if initErr != nil {
			slog.Error("Failed to initialize OpenAPI ToolProvider", "error", initErr)
		} else {
			provider.AdminKey = a.cfg.Auth.AdminKey

			openapiProvider = provider
			mcpGateway.SetOpenAPIProvider(openapiProvider)
			if a.openapiHandler != nil {
				a.openapiHandler.SetProvider(openapiProvider)
			}
			slog.Info("OpenAPI ToolProvider initialized", "enabled_specs", len(openAPISpecs))
		}

	}

	// Combine MCP tools and OpenAPI tools into ProviderSet
	// MCP: match tools that can be resolved by the registry (namespaced or bare without conflicts)
	mcpProv := mcp.Provider{
		Name:   "mcp",
		Prefix: "",
		Match: func(name string) bool {
			// If registry can resolve it, it's an MCP tool
			_, _, resolveErr := mcpRegistry.ResolveTool(name)
			return resolveErr == nil
		},
		Tools: func(keyID string, groupIDs []int) []model.Tool {
			if !iltermiddleware.IsEnabled(a.cfgCache, "mcp") || mcpInjector == nil {
				return nil
			}
			return mcpInjector.GetAuthorizedOpenAITools(keyID, groupIDs)
		},
		Execute: func(ctx context.Context, keyID, keyPrefix string, calls []model.ToolCall) ([]model.Message, []bool) {
			if !iltermiddleware.IsEnabled(a.cfgCache, "mcp") || mcpToolCallExecutor == nil {
				return nil, nil
			}
			return mcpToolCallExecutor.ExecuteToolCalls(ctx, keyID, keyPrefix, calls)
		},
	}

	openapiProv := mcp.Provider{
		Name:   "openapi",
		Prefix: "openapi_",
		Match: func(name string) bool {
			return strings.HasPrefix(name, "openapi_")
		},
		Tools: func(keyID string, groupIDs []int) []model.Tool {
			if !iltermiddleware.IsEnabled(a.cfgCache, "openapi") || openapiProvider == nil {
				return nil
			}
			return openapiProvider.GetAuthorizedTools(keyID, groupIDs)
		},
		Execute: func(ctx context.Context, keyID, keyPrefix string, calls []model.ToolCall) ([]model.Message, []bool) {
			if !iltermiddleware.IsEnabled(a.cfgCache, "openapi") || openapiProvider == nil {
				return nil, nil
			}
			return openapiProvider.Execute(ctx, keyID, keyPrefix, calls)
		},
	}

	providerSet, err := mcp.New(mcpProv, openapiProv)
	if err != nil {
		slog.Error("Failed to create MCP ProviderSet", "error", err)
		return
	}

	a.mcpInjectMiddleware = iltermiddleware.NewMCPMiddleware(
		a.cfgCache,
		providerSet.Inject,
		providerSet.Execute,
		a.piiMaskerMiddleware,
	)

	if a.guardrailsMiddleware != nil {
		a.mcpInjectMiddleware.SetGuardrailsChecker(a.guardrailsMiddleware.Checker())
	}

	toolEventWriter := func(w io.Writer, eventType string, data json.RawMessage) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(data))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	a.mcpInjectMiddleware.SetToolEventWriter(toolEventWriter)
	a.mcpInjectMiddleware.SetSupportsToolsFn(catalog.ModelSupportsTools)
}
