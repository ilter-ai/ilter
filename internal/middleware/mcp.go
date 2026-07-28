package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/guardrails"
	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

var mcpLog = slog.With("component", "mcp")

// MCPInjectMiddleware intercepts /v1/chat/completions requests, injects
// authorized MCP tools, and handles the tool_calls -> execution -> follow-up
// round-trip transparently.
type MCPInjectMiddleware struct {
	injectFn          func(keyID string, groupIDs []int) []model.Tool
	executeFn         func(ctx context.Context, keyID string, keyPrefix string, toolCalls []model.ToolCall) ([]model.Message, []bool)
	piiMasker         *PIIMaskerMiddleware
	guardrailsChecker *guardrails.Checker
	toolEventWriter   func(w io.Writer, eventType string, data json.RawMessage)
	supportsToolsFn   func(modelID string) bool
	cfg               config.MCPInjectionConfig
	cfgMu             sync.RWMutex
}

func NewMCPInjectMiddleware(
	injectFn func(keyID string, groupIDs []int) []model.Tool,
	executeFn func(ctx context.Context, keyID string, keyPrefix string, toolCalls []model.ToolCall) ([]model.Message, []bool),
	cfg config.MCPInjectionConfig,
	opts ...*PIIMaskerMiddleware,
) *MCPInjectMiddleware {
	m := &MCPInjectMiddleware{
		injectFn:  injectFn,
		executeFn: executeFn,
		cfg:       cfg,
	}
	if len(opts) > 0 {
		m.piiMasker = opts[0]
	}
	return m
}

// NewMCPMiddleware creates an MCPInjectMiddleware with ConfigCache-based config.
func NewMCPMiddleware(
	cache *config.Cache,
	injectFn func(keyID string, groupIDs []int) []model.Tool,
	executeFn func(ctx context.Context, keyID string, keyPrefix string, toolCalls []model.ToolCall) ([]model.Message, []bool),
	opts ...*PIIMaskerMiddleware,
) *MCPInjectMiddleware {
	cfg := config.MCPInjectionConfig{
		Enabled:                IsEnabled(cache, "mcp") || IsEnabled(cache, "openapi"),
		DefaultToolChoice:      "auto",
		StripToolsFromResponse: true,
	}

	m := NewMCPInjectMiddleware(injectFn, executeFn, cfg, opts...)

	cache.OnChange(func(_ *config.Snapshot) {
		m.setEnabled(IsEnabled(cache, "mcp") || IsEnabled(cache, "openapi"))
	})

	return m
}

// SetGuardrailsChecker sets the optional guardrails checker.
func (m *MCPInjectMiddleware) SetGuardrailsChecker(c *guardrails.Checker) {
	m.guardrailsChecker = c
}

// SetToolEventWriter sets the optional event writer for Chat UI visibility.
func (m *MCPInjectMiddleware) SetToolEventWriter(w func(w io.Writer, eventType string, data json.RawMessage)) {
	m.toolEventWriter = w
}

// SetSupportsToolsFn sets the optional function that reports whether a model supports native tool calling.
func (m *MCPInjectMiddleware) SetSupportsToolsFn(fn func(modelID string) bool) {
	m.supportsToolsFn = fn
}

func (m *MCPInjectMiddleware) setEnabled(v bool) {
	m.cfgMu.Lock()
	m.cfg.Enabled = v
	m.cfgMu.Unlock()
}

// Handler returns the Chi-compatible HTTP middleware.
func (m *MCPInjectMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.cfgMu.RLock()
		cfgEnabled := m.cfg.Enabled
		m.cfgMu.RUnlock()
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") || !cfgEnabled {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		var req model.ChatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			r.Body = io.NopCloser(bytes.NewBuffer(body))
			next.ServeHTTP(w, r)
			return
		}

		modified := m.maybeInjectTools(r.Context(), &req)
		if modified {
			newBody, _ := json.Marshal(req)
			r.Body = io.NopCloser(bytes.NewBuffer(newBody))
			r.ContentLength = int64(len(newBody))
		} else {
			r.Body = io.NopCloser(bytes.NewBuffer(body))
		}

		if !modified && len(req.Tools) == 0 && !mcp.HasToolSentinel(req.Messages) && !mcp.HasToolResult(req.Messages) {
			next.ServeHTTP(w, r)
			return
		}

		m.toolCallLoop(w, r, &req, next)
	})
}

func (m *MCPInjectMiddleware) maybeInjectTools(rctx context.Context, req *model.ChatCompletionRequest) bool {
	if len(req.Tools) > 0 {
		return false
	}
	keyID := reqmeta.GetKeyID(rctx)
	groupIDs := reqmeta.GetGroupIDs(rctx)

	tools := m.injectFn(keyID, groupIDs)
	if len(tools) == 0 {
		mcpLog.Debug("no authorized tools for key", "key_id", keyID)
		return false
	}
	mcpLog.Debug("injected tools", "key_id", keyID, "count", len(tools))

	req.Tools = tools
	m.cfgMu.RLock()
	defaultToolChoice := m.cfg.DefaultToolChoice
	m.cfgMu.RUnlock()
	if defaultToolChoice != "" && req.ToolChoice == nil {
		req.ToolChoice = defaultToolChoice
	}
	return true
}
