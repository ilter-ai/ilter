package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/model"
)

// This file implements a POST /v1/messages passthrough: clients that speak
// Anthropic's native Messages API wire format (e.g. Claude Code pointed at a
// custom base URL) can talk to ilter directly, without every request having
// to go through an OpenAI-format client first. The request is translated to
// ilter's internal model.ChatCompletionRequest, run through the *exact same*
// middleware chain and provider routing as /v1/chat/completions (so it can
// still land on a non-Anthropic upstream), and the response is translated
// back into Anthropic's wire format.

type anthropicWireMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicWireTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicWireThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anthropicMessagesRequest struct {
	Model         string                 `json:"model"`
	Messages      []anthropicWireMessage `json:"messages"`
	System        any                    `json:"system,omitempty"` // string or []content-block
	MaxTokens     int                    `json:"max_tokens"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Tools         []anthropicWireTool    `json:"tools,omitempty"`
	ToolChoice    any                    `json:"tool_choice,omitempty"`
	Thinking      *anthropicWireThinking `json:"thinking,omitempty"`
}

// TranslateAnthropicMessagesRequest decodes an Anthropic /v1/messages request
// body and converts it into ilter's internal chat-completion request shape.
func TranslateAnthropicMessagesRequest(bodyBytes []byte) (*model.ChatCompletionRequest, string, bool, error) {
	var wireReq anthropicMessagesRequest
	if err := json.Unmarshal(bodyBytes, &wireReq); err != nil {
		return nil, "", false, fmt.Errorf("invalid json body: %w", err)
	}
	if wireReq.Model == "" {
		return nil, "", false, fmt.Errorf("model is required")
	}

	chatReq := &model.ChatCompletionRequest{
		Model:       wireReq.Model,
		Messages:    anthropicWireToInternalMessages(wireReq.System, wireReq.Messages),
		Temperature: wireReq.Temperature,
		TopP:        wireReq.TopP,
		Stream:      wireReq.Stream,
		Stop:        wireReq.StopSequences,
		Tools:       anthropicWireToolsToInternal(wireReq.Tools),
		ToolChoice:  anthropicWireToolChoiceToInternal(wireReq.ToolChoice),
	}
	if wireReq.MaxTokens > 0 {
		chatReq.MaxTokens = &wireReq.MaxTokens
	}
	if wireReq.Thinking != nil {
		chatReq.Thinking = &model.ThinkingConfig{
			Type:         wireReq.Thinking.Type,
			BudgetTokens: wireReq.Thinking.BudgetTokens,
		}
	}

	return chatReq, wireReq.Model, wireReq.Stream, nil
}

func anthropicWireToInternalMessages(system any, msgs []anthropicWireMessage) []model.Message {
	var out []model.Message
	if sysText := flattenAnthropicSystem(system); sysText != "" {
		out = append(out, model.Message{Role: "system", Content: sysText})
	}
	for _, m := range msgs {
		out = append(out, anthropicWireMessageToInternal(m)...)
	}
	return out
}

func flattenAnthropicSystem(system any) string {
	switch v := system.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if bm, ok := item.(map[string]any); ok {
				if t, _ := bm["text"].(string); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

func anthropicWireMessageToInternal(m anthropicWireMessage) []model.Message {
	switch content := m.Content.(type) {
	case string:
		return []model.Message{{Role: m.Role, Content: content}}
	case []any:
		var textParts []string
		var toolCalls []model.ToolCall
		var toolResults []model.Message
		// kept preserves every non-tool_use, non-tool_result block (text,
		// image, and any future block type) in its original raw shape, so
		// nothing is silently dropped when a message mixes text with images
		// — only collapsed to a plain string below when it's text-only.
		var kept []any
		onlyText := true
		for _, blk := range content {
			bm, ok := blk.(map[string]any)
			if !ok {
				kept = append(kept, blk)
				onlyText = false
				continue
			}
			switch bm["type"] {
			case "text":
				if t, ok := bm["text"].(string); ok {
					textParts = append(textParts, t)
				}
				kept = append(kept, blk)
			case "tool_use":
				id, _ := bm["id"].(string)
				name, _ := bm["name"].(string)
				inputBytes, _ := json.Marshal(bm["input"])
				toolCalls = append(toolCalls, model.ToolCall{
					ID:   id,
					Type: "function",
					Function: model.ToolCallFunctionData{
						Name:      name,
						Arguments: string(inputBytes),
					},
				})
				onlyText = false
			case "tool_result":
				toolUseID, _ := bm["tool_use_id"].(string)
				toolResults = append(toolResults, model.Message{
					Role:       "tool",
					ToolCallID: toolUseID,
					Content:    flattenAnthropicToolResultContent(bm["content"]),
				})
				onlyText = false
			default:
				// image or any other/unrecognized block type: preserve raw
				// rather than silently dropping it.
				kept = append(kept, blk)
				onlyText = false
			}
		}
		var out []model.Message
		if len(kept) > 0 || len(toolCalls) > 0 {
			msg := model.Message{Role: m.Role, ToolCalls: toolCalls}
			switch {
			case onlyText:
				msg.Content = strings.Join(textParts, "")
			case len(kept) > 0:
				msg.Content = kept
			}
			out = append(out, msg)
		}
		out = append(out, toolResults...)
		return out
	default:
		return nil
	}
}

func flattenAnthropicToolResultContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if bm, ok := item.(map[string]any); ok {
				if t, _ := bm["text"].(string); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "")
	default:
		b, _ := json.Marshal(content)
		return string(b)
	}
}

func anthropicWireToolsToInternal(tools []anthropicWireTool) []model.Tool {
	var out []model.Tool
	for _, t := range tools {
		params, _ := t.InputSchema.(map[string]any)
		out = append(out, model.Tool{
			Type: "function",
			Function: model.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func anthropicWireToolChoiceToInternal(tc any) any {
	m, ok := tc.(map[string]any)
	if !ok {
		return nil
	}
	switch m["type"] {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		if name, ok := m["name"].(string); ok {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
	}
	return nil
}

// --- response translation: internal chat-completion -> Anthropic wire ---

// anthropicTranslator implements chatResponseTranslator, mapping ilter's
// internal /v1/chat/completions output (OpenAI-shaped JSON or SSE) into
// Anthropic's Messages API wire format. All the HTTP/SSE plumbing lives in
// translatingResponseWriter; this only does the format mapping.
type anthropicTranslator struct {
	requestedModel string
	stream         *anthropicStreamTranslator
}

func newAnthropicTranslator(requestedModel string) *anthropicTranslator {
	return &anthropicTranslator{
		requestedModel: requestedModel,
		stream:         newAnthropicStreamTranslator(requestedModel),
	}
}

func (t *anthropicTranslator) streamChunk(w http.ResponseWriter, chunk *model.ChatCompletionChunk) {
	t.stream.feed(w, chunk)
}

func (t *anthropicTranslator) streamDone(w http.ResponseWriter) {
	t.stream.finish(w)
}

func (t *anthropicTranslator) finishSuccess(w http.ResponseWriter, body []byte) {
	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "failed to translate response")
		return
	}
	model.WriteJSON(w, http.StatusOK, translateChatResponseToAnthropic(&chatResp, t.requestedModel))
}

func (t *anthropicTranslator) finishError(w http.ResponseWriter, status int, body []byte) {
	var errResp model.ErrorResponse
	_ = json.Unmarshal(body, &errResp)
	msg := errResp.Error.Message
	if msg == "" {
		msg = string(body)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    anthropicErrorType(status),
			"message": msg,
		},
	})
}

// anthropicErrorType maps an HTTP status to Anthropic's error.type taxonomy
// (invalid_request_error, rate_limit_error, ...) so native Anthropic clients
// that branch on error.type behave the same talking to ilter as to Anthropic
// directly.
func anthropicErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

func translateChatResponseToAnthropic(resp *model.ChatCompletionResponse, requestedModel string) map[string]any {
	content := []map[string]any{}
	stopReason := "end_turn"
	role := "assistant"

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if choice.Message.Role != "" {
			role = choice.Message.Role
		}
		if choice.Message.ReasoningContent != "" {
			content = append(content, map[string]any{"type": "thinking", "thinking": choice.Message.ReasoningContent})
		}
		if choice.Message.Content != "" {
			content = append(content, map[string]any{"type": "text", "text": choice.Message.Content})
		}
		for _, tc := range choice.Message.ToolCalls {
			var input map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": input,
			})
		}
		switch choice.FinishReason {
		case "length":
			stopReason = "max_tokens"
		case "tool_calls":
			stopReason = "tool_use"
		}
	}

	id := resp.ID
	if id == "" {
		id = "msg_" + uuid.New().String()
	}
	modelName := resp.Model
	if modelName == "" {
		modelName = requestedModel
	}
	usage := map[string]int{"input_tokens": 0, "output_tokens": 0}
	if resp.Usage != nil {
		usage["input_tokens"] = resp.Usage.PromptTokens
		usage["output_tokens"] = resp.Usage.CompletionTokens
	}

	return map[string]any{
		"id":            id,
		"type":          "message",
		"role":          role,
		"model":         modelName,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         usage,
	}
}

// --- streaming translation: internal SSE chunks -> Anthropic SSE events ---

type anthropicStreamTranslator struct {
	requestedModel string
	messageID      string
	startSent      bool
	blockOpen      bool
	blockIndex     int
	blockType      string
	nextIndex      int
	toolBlockByIdx map[int]int
	inputTokens    int
	outputTokens   int
	stopReason     string
}

func newAnthropicStreamTranslator(requestedModel string) *anthropicStreamTranslator {
	return &anthropicStreamTranslator{
		requestedModel: requestedModel,
		toolBlockByIdx: make(map[int]int),
		stopReason:     "end_turn",
	}
}

func (t *anthropicStreamTranslator) writeEvent(w io.Writer, eventType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
}

func (t *anthropicStreamTranslator) ensureStarted(w io.Writer, chunk *model.ChatCompletionChunk) {
	if t.startSent {
		return
	}
	t.startSent = true
	t.messageID = chunk.ID
	if t.messageID == "" {
		t.messageID = "msg_" + uuid.New().String()
	}
	modelName := chunk.Model
	if modelName == "" {
		modelName = t.requestedModel
	}
	t.writeEvent(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            t.messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         modelName,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	})
}

func (t *anthropicStreamTranslator) closeBlock(w io.Writer) {
	if !t.blockOpen {
		return
	}
	t.writeEvent(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": t.blockIndex,
	})
	t.blockOpen = false
	t.blockType = ""
}

func (t *anthropicStreamTranslator) openBlock(w io.Writer, blockType string, contentBlock map[string]any) int {
	t.closeBlock(w)
	idx := t.nextIndex
	t.nextIndex++
	t.blockOpen = true
	t.blockType = blockType
	t.blockIndex = idx
	t.writeEvent(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": contentBlock,
	})
	return idx
}

func (t *anthropicStreamTranslator) feed(w io.Writer, chunk *model.ChatCompletionChunk) {
	t.ensureStarted(w, chunk)

	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			t.inputTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			t.outputTokens = chunk.Usage.CompletionTokens
		}
	}

	for _, choice := range chunk.Choices {
		delta := choice.Delta

		if delta.ReasoningContent != "" {
			if t.blockType != "thinking" {
				t.openBlock(w, "thinking", map[string]any{"type": "thinking", "thinking": ""})
			}
			t.writeEvent(w, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": t.blockIndex,
				"delta": map[string]any{"type": "thinking_delta", "thinking": delta.ReasoningContent},
			})
		}

		if delta.Content != "" {
			if t.blockType != "text" {
				t.openBlock(w, "text", map[string]any{"type": "text", "text": ""})
			}
			t.writeEvent(w, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": t.blockIndex,
				"delta": map[string]any{"type": "text_delta", "text": delta.Content},
			})
		}

		for _, tc := range delta.ToolCalls {
			if tc.ID != "" {
				blockIdx := t.openBlock(w, "tool_use", map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": map[string]any{},
				})
				t.toolBlockByIdx[tc.Index] = blockIdx
			}
			if tc.Function.Arguments != "" {
				blockIdx, ok := t.toolBlockByIdx[tc.Index]
				if !ok {
					blockIdx = t.blockIndex
				}
				t.writeEvent(w, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIdx,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": tc.Function.Arguments},
				})
			}
		}

		if choice.FinishReason != nil {
			switch *choice.FinishReason {
			case "length":
				t.stopReason = "max_tokens"
			case "tool_calls":
				t.stopReason = "tool_use"
			default:
				t.stopReason = "end_turn"
			}
		}
	}
}

func (t *anthropicStreamTranslator) finish(w io.Writer) {
	if !t.startSent {
		return
	}
	t.closeBlock(w)
	t.writeEvent(w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": t.stopReason, "stop_sequence": nil},
		"usage": map[string]int{"output_tokens": t.outputTokens},
	})
	t.writeEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

// AnthropicMessages serves POST /v1/messages: Anthropic's native Messages API
// wire format, translated into ilter's internal chat-completion request and
// run through the exact same middleware chain (auth, budget, PII,
// guardrails, MCP tool injection, smart routing, semantic cache) as
// /v1/chat/completions, so Anthropic-native clients (Claude Code pointed at
// a custom base URL, for example) can use ilter directly.
func (h *Handler) AnthropicMessages(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, "failed to read request body")
		return
	}

	chatReq, requestedModel, isStream, err := TranslateAnthropicMessagesRequest(bodyBytes)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, err.Error())
		return
	}

	chatBodyBytes, err := json.Marshal(chatReq)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "failed to translate request")
		return
	}

	innerReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "/v1/chat/completions", bytes.NewReader(chatBodyBytes))
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "failed to build internal request")
		return
	}
	innerReq.Header = r.Header.Clone()
	innerReq.Header.Set("Content-Type", "application/json")
	innerReq.Header.Set("Content-Length", strconv.Itoa(len(chatBodyBytes)))
	innerReq.ContentLength = int64(len(chatBodyBytes))

	rw := newTranslatingResponseWriter(w, isStream, newAnthropicTranslator(requestedModel))
	h.chatChain.ServeHTTP(rw, innerReq)
	rw.Finish()
}
