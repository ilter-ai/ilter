package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/features/pii"
	"github.com/ilter-ai/ilter/internal/features/semanticcache"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type SemanticCacheMiddleware struct {
	cache           *semanticcache.SemanticCache
	cfg             config.CacheConfig
	piiMw           *pii.Masker // optional, for response PII detection — prevents data leak
	runtimeDisabled atomic.Bool // runtime toggle, true = cache disabled regardless of config
	cfgCache        *config.Cache
}

// SetEnabled controls the cache at runtime. When false, the Handler skips caching
// even if config has caching enabled.
func (c *SemanticCacheMiddleware) SetEnabled(enabled bool) { c.runtimeDisabled.Store(!enabled) }

func NewSemanticCacheMiddleware(cfg config.CacheConfig, g *circuitbreaker.RedisBreaker, cfgCache *config.Cache) *SemanticCacheMiddleware {
	if g == nil {
		return &SemanticCacheMiddleware{cfg: cfg, cfgCache: cfgCache}
	}
	return &SemanticCacheMiddleware{
		cache:    semanticcache.New(cfg, g.Client(), cfg.OllamaURL),
		cfg:      cfg,
		cfgCache: cfgCache,
	}
}

// SetPIIMasker sets the optional PII masker. Responses with PII are not cached.
func (c *SemanticCacheMiddleware) SetPIIMasker(m *pii.Masker) { c.piiMw = m }

func (c *SemanticCacheMiddleware) Mode() string {
	enabled := c.cfg.Enabled
	if c.cfgCache != nil {
		enabled = IsEnabled(c.cfgCache, "semantic_cache")
	}
	if c.runtimeDisabled.Load() {
		enabled = false
	}
	if !enabled || c.cache == nil {
		return "disabled"
	}
	return string(c.cache.Mode())
}

func (c *SemanticCacheMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enabled := c.cfg.Enabled
		if c.cfgCache != nil {
			enabled = IsEnabled(c.cfgCache, "semantic_cache")
		}
		if !enabled || c.runtimeDisabled.Load() || c.cache == nil || r.Method != "POST" {
			next.ServeHTTP(w, r)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		var req model.ChatCompletionRequest
		if errUnmarshal := json.Unmarshal(bodyBytes, &req); errUnmarshal != nil {
			next.ServeHTTP(w, r)
			return
		}

		if req.Temperature != nil && *req.Temperature > 0.9 {
			next.ServeHTTP(w, r)
			return
		}

		if hasActiveTools := len(req.Tools) > 0 || hasToolRoleMessage(req.Messages); hasActiveTools {
			next.ServeHTTP(w, r)
			return
		}

		var b strings.Builder
		for _, m := range req.Messages {
			b.WriteString(string(m.Role))
			b.WriteString(": ")
			fmt.Fprintf(&b, "%v\n", m.Content)
		}
		fullPrompt := b.String()

		// Use only the last user message for semantic embedding, so long conversation
		// histories don't exceed embedding model context limits.
		var embedText string
		for _, v := range slices.Backward(req.Messages) {
			if v.Role == "user" {
				if s, ok := v.Content.(string); ok && s != "" {
					embedText = s
				}
				break
			}
		}
		if embedText == "" {
			embedText = fullPrompt
		}

		meta := reqmeta.GetRequestMetadata(r.Context())

		if cachedResp, score, hit := c.cache.GetFull(r.Context(), "search_query: "+embedText, embedText); hit {
			slog.Debug("semantic cache hit", "score", fmt.Sprintf("%.4f", score), "threshold", c.cfg.SimilarityThreshold)

			// Defense-in-depth: if the cached response contains tool_calls, fall
			// through to the real provider. Tool-call responses should never be
			// cached, but stale entries produce broken UI (no tool card markers).
			if !cachedResponseHasToolCalls(cachedResp) {
				if meta != nil {
					meta.SetCacheHit(true)
				}
				w.Header().Set("X-Cache-Hit", "true")
				w.Header().Set("X-Ilter-Cost", "0")
				w.Header().Set("X-Ilter-Model-Actual", "ilter/semantic_cache")

				if req.Stream {
					serveStreamingCacheHit(w, cachedResp)
					return
				}

				var cachedRespModel model.ChatCompletionResponse
				if err := json.Unmarshal([]byte(cachedResp), &cachedRespModel); err == nil {
					cachedRespModel.Model = "ilter/semantic_cache"
					if data, err := json.Marshal(cachedRespModel); err == nil {
						cachedResp = string(data)
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(cachedResp)); err != nil {
					slog.Debug("cache write error", "error", err)
				}
				return
			}
			slog.Warn("semantic cache: stale entry with tool_calls, falling through", "embed", embedText)
		}

		if meta != nil {
			meta.SetCacheHit(false)
		}
		w.Header().Set("X-Cache-Hit", "false")

		rec := NewResponseRecorder(w)
		next.ServeHTTP(rec, r)

		if rec.Status() == http.StatusOK {
			cachedBody := UnmaskResponse(r.Context(), rec.BodyString())

			if req.Stream {
				cachedBody = sseToResponse(cachedBody)
			}

			// Skip caching if the response contains tool_calls — caching a tool call
			// response breaks the MCP inject middleware's tool call loop: on follow-up
			// requests the cache returns the stale tool_calls response, causing the
			// inject loop to re-execute tools infinitely.
			var (
				piiDetected  bool
				hasToolCalls bool
			)
			var resp model.ChatCompletionResponse
			if err := json.Unmarshal([]byte(cachedBody), &resp); err == nil && len(resp.Choices) > 0 {
				hasToolCalls = len(resp.Choices[0].Message.ToolCalls) > 0 || strings.Contains(resp.Choices[0].Message.Content, "<tool_calls>")
				if c.piiMw != nil {
					piiDetected = len(c.piiMw.DetectPII(resp.Choices[0].Message.Content)) > 0
				}
			}
			if piiDetected {
				slog.Debug("semantic cache skipped: response contains PII")
			}
			if hasToolCalls {
				slog.Debug("semantic cache skipped: response contains tool_calls")
			}
			if !piiDetected && !hasToolCalls {
				// Use background context — request context may be canceled after handler returns
				// (e.g. streaming handlers), which would cause embedding to fail and VSS entry
				// to be silently skipped.
				if err := c.cache.SetFull(context.Background(), "search_document: "+embedText, embedText, cachedBody); err != nil {
					slog.Error("Failed to store semantic cache entry", "error", err)
				}
			}
		}
	})
}

func serveStreamingCacheHit(w http.ResponseWriter, cachedResp string) {
	var resp model.ChatCompletionResponse
	if err := json.Unmarshal([]byte(cachedResp), &resp); err != nil {
		slog.Warn("semantic cache: failed to unmarshal cached response for streaming", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cachedResp))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Warn("semantic cache: ResponseWriter does not support Flusher, serving as JSON")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cachedResp))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Ilter-Model-Actual", "ilter/semantic_cache")
	w.WriteHeader(http.StatusOK)

	content := ""
	reasoning := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		reasoning = resp.Choices[0].Message.ReasoningContent
	}
	finish := "stop"

	chunk := model.ChatCompletionChunk{
		ID:      resp.ID,
		Object:  "chat.completion.chunk",
		Created: resp.Created,
		Model:   "ilter/semantic_cache",
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Role: "assistant", ReasoningContent: reasoning}}},
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	chunk.Choices[0].Delta = model.Delta{Content: content, ReasoningContent: reasoning}
	chunk.Choices[0].FinishReason = &finish
	data, _ = json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// hasToolRoleMessage reports whether the conversation shows evidence that a
// tool was actually called — not merely that tools are available. The MCP
// tool-injection system prompt ("You have access to the following tools.")
// is present on nearly every request for keys with any authorized tool, so
// matching on it here would disable semantic caching almost unconditionally
// regardless of whether the turn ever used a tool.
func hasToolRoleMessage(msgs []model.Message) bool {
	for i := range msgs {
		if msgs[i].Role == "tool" {
			return true
		}
		// The injected system prompt for emulation-mode models contains
		// illustrative <tool_calls>/<invoke> example syntax as instructions —
		// matching against system messages here would false-positive on every
		// such request regardless of whether a tool was ever actually called.
		if msgs[i].Role == "system" {
			continue
		}
		if s, ok := msgs[i].Content.(string); ok {
			if strings.Contains(s, "<tool_calls>") || strings.Contains(s, "<tool_result") {
				return true
			}
		}
	}
	return false
}

func cachedResponseHasToolCalls(cachedJSON string) bool {
	var resp model.ChatCompletionResponse
	if err := json.Unmarshal([]byte(cachedJSON), &resp); err != nil {
		return false
	}
	for _, ch := range resp.Choices {
		if len(ch.Message.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func sseToResponse(sseData string) string {
	var id, modelName string
	var created int64
	var content, reasoning strings.Builder

	for line := range strings.SplitSeq(sseData, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk model.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if id == "" {
			id = chunk.ID
			modelName = chunk.Model
			created = chunk.Created
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
			reasoning.WriteString(choice.Delta.ReasoningContent)
		}
	}

	resp := model.ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   modelName,
		Choices: []model.Choice{{
			Index:        0,
			Message:      model.ChoiceMessage{Role: "assistant", Content: content.String(), ReasoningContent: reasoning.String()},
			FinishReason: "stop",
		}},
	}
	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		slog.Warn("sseToResponse marshal failed", "err", err)
		return sseData
	}
	return string(jsonBytes)
}
