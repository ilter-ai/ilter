package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ilter-ai/ilter/internal/model"
)

// This file implements the legacy POST /v1/completions endpoint (OpenAI's
// pre-chat text-completion API). It's deprecated upstream but some older
// SDKs and self-hosted tooling still target it. Requests are translated into
// a single-user-message model.ChatCompletionRequest and run through the same
// chat-completions pipeline; responses are translated back into the legacy
// {choices: [{text, index, finish_reason}]} shape.

type legacyCompletionRequest struct {
	Model            string   `json:"model"`
	Prompt           any      `json:"prompt"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	N                *int     `json:"n,omitempty"`
	Stream           bool     `json:"stream,omitempty"`
	Stop             []string `json:"stop,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
}

// TranslateLegacyCompletionRequest decodes a legacy /v1/completions body and
// converts it into ilter's internal chat-completion request shape, wrapping
// the prompt as a single user message.
func TranslateLegacyCompletionRequest(bodyBytes []byte) (*model.ChatCompletionRequest, string, bool, error) {
	var legacyReq legacyCompletionRequest
	if err := json.Unmarshal(bodyBytes, &legacyReq); err != nil {
		return nil, "", false, fmt.Errorf("invalid json body: %w", err)
	}
	if legacyReq.Model == "" {
		return nil, "", false, fmt.Errorf("model is required")
	}

	prompt, err := flattenLegacyPrompt(legacyReq.Prompt)
	if err != nil {
		return nil, "", false, err
	}

	return &model.ChatCompletionRequest{
		Model:            legacyReq.Model,
		Messages:         []model.Message{{Role: "user", Content: prompt}},
		Temperature:      legacyReq.Temperature,
		TopP:             legacyReq.TopP,
		N:                legacyReq.N,
		Stream:           legacyReq.Stream,
		Stop:             legacyReq.Stop,
		MaxTokens:        legacyReq.MaxTokens,
		PresencePenalty:  legacyReq.PresencePenalty,
		FrequencyPenalty: legacyReq.FrequencyPenalty,
	}, legacyReq.Model, legacyReq.Stream, nil
}

func flattenLegacyPrompt(prompt any) (string, error) {
	switch v := prompt.(type) {
	case string:
		return v, nil
	case []any:
		if len(v) == 0 {
			return "", fmt.Errorf("prompt must not be empty")
		}
		if s, ok := v[0].(string); ok {
			return s, nil
		}
		return "", fmt.Errorf("only string prompts are supported")
	default:
		return "", fmt.Errorf("prompt is required")
	}
}

// legacyCompletionTranslator implements chatResponseTranslator, mapping
// ilter's internal /v1/chat/completions output (OpenAI chat-shaped JSON or
// SSE) into the legacy text-completion wire format. All the HTTP/SSE plumbing
// lives in translatingResponseWriter; this only does the format mapping.
type legacyCompletionTranslator struct {
	requestedModel string
}

func (t *legacyCompletionTranslator) streamChunk(w http.ResponseWriter, chunk *model.ChatCompletionChunk) {
	data, err := json.Marshal(translateChunkToLegacyCompletion(chunk, t.requestedModel))
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func (t *legacyCompletionTranslator) streamDone(w http.ResponseWriter) {
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func (t *legacyCompletionTranslator) finishSuccess(w http.ResponseWriter, body []byte) {
	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "failed to translate response")
		return
	}
	model.WriteJSON(w, http.StatusOK, translateChatResponseToLegacyCompletion(&chatResp, t.requestedModel))
}

func (t *legacyCompletionTranslator) finishError(w http.ResponseWriter, _ int, body []byte) {
	// Error JSON shape (model.ErrorResponse) is already OpenAI-compatible for
	// both chat and legacy completions — pass through unchanged.
	_, _ = w.Write(body)
}

type legacyCompletionChoice struct {
	Text         string `json:"text"`
	Index        int    `json:"index"`
	Logprobs     any    `json:"logprobs"`
	FinishReason string `json:"finish_reason"`
}

type legacyCompletionResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []legacyCompletionChoice `json:"choices"`
	Usage   *model.Usage             `json:"usage,omitempty"`
}

func translateChatResponseToLegacyCompletion(resp *model.ChatCompletionResponse, requestedModel string) legacyCompletionResponse {
	modelName := resp.Model
	if modelName == "" {
		modelName = requestedModel
	}
	out := legacyCompletionResponse{
		ID:      resp.ID,
		Object:  "text_completion",
		Created: resp.Created,
		Model:   modelName,
		Usage:   resp.Usage,
	}
	if out.ID == "" {
		out.ID = "cmpl-" + fmt.Sprint(time.Now().UnixNano())
	}
	for _, choice := range resp.Choices {
		out.Choices = append(out.Choices, legacyCompletionChoice{
			Text:         choice.Message.Content,
			Index:        choice.Index,
			FinishReason: choice.FinishReason,
		})
	}
	return out
}

// LegacyCompletions serves the deprecated POST /v1/completions API,
// translating prompt-based requests into a chat-completion request and back,
// reusing chatChain the same way AnthropicMessages does.
func (h *Handler) LegacyCompletions(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, "failed to read request body")
		return
	}

	chatReq, requestedModel, isStream, err := TranslateLegacyCompletionRequest(bodyBytes)
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

	rw := newTranslatingResponseWriter(w, isStream, &legacyCompletionTranslator{requestedModel: requestedModel})
	h.chatChain.ServeHTTP(rw, innerReq)
	rw.Finish()
}

func translateChunkToLegacyCompletion(chunk *model.ChatCompletionChunk, requestedModel string) legacyCompletionResponse {
	modelName := chunk.Model
	if modelName == "" {
		modelName = requestedModel
	}
	out := legacyCompletionResponse{
		ID:      chunk.ID,
		Object:  "text_completion",
		Created: chunk.Created,
		Model:   modelName,
		Usage:   chunk.Usage,
	}
	for _, choice := range chunk.Choices {
		finishReason := ""
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
		out.Choices = append(out.Choices, legacyCompletionChoice{
			Text:         choice.Delta.Content,
			Index:        choice.Index,
			FinishReason: finishReason,
		})
	}
	return out
}
