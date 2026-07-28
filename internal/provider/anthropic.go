package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

type AnthropicProvider struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAnthropicProvider(cfg config.ProviderConfig) *AnthropicProvider {
	return &AnthropicProvider{
		config: cfg,
		client: NewResilientClient(cfg),
	}
}

func (p *AnthropicProvider) Name() string {
	return p.config.Name
}

func (p *AnthropicProvider) APIKeys() []string {
	return p.config.GetAPIKeys()
}

func (p *AnthropicProvider) Type() string {
	return "anthropic"
}

func (p *AnthropicProvider) TransformRequest(ctx context.Context, req *model.ChatCompletionRequest) (*http.Request, error) {
	anthropicReq := anthropicRequest{
		Model:       req.Model,
		Stop:        req.Stop,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if req.MaxTokens != nil {
		anthropicReq.MaxTokens = *req.MaxTokens
	} else {
		anthropicReq.MaxTokens = 4096
	}

	var systemParts []string
	var anthropicMsgs []anthropicMessage

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			if strContent, ok := msg.Content.(string); ok {
				systemParts = append(systemParts, strContent)
			}
		case "user":
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    "user",
				Content: msg.Content,
			})
		case "assistant":
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    "assistant",
				Content: buildAssistantContent(msg),
			})
		case "tool":
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    "user",
				Content: []any{buildToolResult(msg)},
			})
		}
	}

	anthropicReq.System = strings.Join(systemParts, "\n\n")
	anthropicReq.Messages = anthropicMsgs

	if len(req.Tools) > 0 {
		var aTools []anthropicTool
		for _, t := range req.Tools {
			if t.Type == "function" {
				aTools = append(aTools, anthropicTool{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					InputSchema: t.Function.Parameters,
				})
			}
		}
		anthropicReq.Tools = aTools
	}

	if req.ToolChoice != nil {
		switch v := req.ToolChoice.(type) {
		case string:
			if v == "auto" {
				anthropicReq.ToolChoice = map[string]string{"type": "auto"}
			} else if v == "required" {
				anthropicReq.ToolChoice = map[string]string{"type": "any"}
			}
		case map[string]any:
			if typeVal, ok := v["type"].(string); ok && typeVal == "function" {
				if fn, ok := v["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						anthropicReq.ToolChoice = map[string]string{
							"type": "tool",
							"name": name,
						}
					}
				}
			}
		}
	}

	bodyBytes, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	url := fmt.Sprintf("%s/messages", p.config.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	apiKey := SelectedAPIKeyFromContext(ctx)
	if apiKey == "" {
		keys := p.config.GetAPIKeys()
		if len(keys) > 0 {
			apiKey = keys[0]
		}
	}
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	for k, v := range p.config.Headers {
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}

func (p *AnthropicProvider) TransformResponse(_ context.Context, resp *http.Response) (*model.ChatCompletionResponse, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read anthropic response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic returned status %d: %s", resp.StatusCode, strings.ReplaceAll(strings.ReplaceAll(string(bodyBytes), "\n", " "), "\r", ""))
	}

	var anthResp anthropicResponse
	if err := json.Unmarshal(bodyBytes, &anthResp); err != nil {
		return nil, fmt.Errorf("failed to decode anthropic response: %w", err)
	}

	finishReason := "stop"
	if anthResp.StopReason == "max_tokens" {
		finishReason = "length"
	}

	var fullText string
	var toolCalls []model.ToolCall
	for _, block := range anthResp.Content {
		if block.Type == "text" {
			fullText += block.Text
		} else if block.Type == "tool_use" {
			argsBytes, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, model.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: model.ToolCallFunctionData{
					Name:      block.Name,
					Arguments: string(argsBytes),
				},
			})
		}
	}

	chatResp := &model.ChatCompletionResponse{
		ID:      anthResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   anthResp.Model,
		Choices: []model.Choice{
			{
				Index: 0,
				Message: model.ChoiceMessage{
					Role:      anthResp.Role,
					Content:   fullText,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: &model.Usage{
			PromptTokens:     anthResp.Usage.InputTokens,
			CompletionTokens: anthResp.Usage.OutputTokens,
			TotalTokens:      anthResp.Usage.InputTokens + anthResp.Usage.OutputTokens,
		},
	}

	return chatResp, nil
}

func (p *AnthropicProvider) Client() *http.Client {
	return p.client
}

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	// Anthropic doesn't have a /models endpoint — POST a minimal empty body to /v1/messages.
	// A 400 means auth is valid but the request body was empty (expected for health check).
	// A 401 means the endpoint is reachable but the API key is wrong.
	url := fmt.Sprintf("%s/messages", p.config.BaseURL)
	body := bytes.NewReader([]byte("{}"))
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("health check failed: unauthorized api key")
	}
	return nil
}

func (p *AnthropicProvider) UpdateConfig(baseURL string, apiKey string) {
	if baseURL != "" {
		p.config.BaseURL = baseURL
	}
	p.config.APIKey = apiKey
	if apiKey != "" {
		p.config.APIKeys = []string{apiKey}
	} else {
		p.config.APIKeys = nil
	}
}

func buildAssistantContent(msg model.Message) any {
	if len(msg.ToolCalls) == 0 {
		return msg.Content
	}
	var blocks []any
	if strContent, ok := msg.Content.(string); ok && strContent != "" {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": strContent,
		})
	}
	for _, tc := range msg.ToolCalls {
		var inputMap map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &inputMap); err != nil {
			slog.Warn("failed to unmarshal tool call arguments", "error", err)
		}
		if inputMap == nil {
			inputMap = make(map[string]any)
		}
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": inputMap,
		})
	}
	return blocks
}

func buildToolResult(msg model.Message) map[string]any {
	toolResult := map[string]any{
		"type":        "tool_result",
		"tool_use_id": msg.ToolCallID,
	}
	if strContent, ok := msg.Content.(string); ok {
		toolResult["content"] = strContent
	} else {
		toolResult["content"] = msg.Content
	}
	return toolResult
}

func (p *AnthropicProvider) UpdateKeys(baseURL string, apiKey string, apiKeys []string) {
	if baseURL != "" {
		p.config.BaseURL = baseURL
	}
	p.config.APIKey = apiKey
	p.config.APIKeys = apiKeys
}
