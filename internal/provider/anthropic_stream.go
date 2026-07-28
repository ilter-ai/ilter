package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

func (p *AnthropicProvider) TransformStreamChunk(data []byte) (*model.ChatCompletionChunk, bool, error) {
	var event anthropicSSEEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, false, nil
	}

	switch event.Type {
	case "message_start":
		chunk := &model.ChatCompletionChunk{
			ID:      event.Message.ID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   event.Message.Model,
			Choices: []model.ChunkChoice{
				{
					Index: 0,
					Delta: model.Delta{Role: "assistant"},
				},
			},
		}
		if event.Message.Usage != nil && event.Message.Usage.InputTokens > 0 {
			chunk.Usage = &model.Usage{
				PromptTokens: event.Message.Usage.InputTokens,
				TotalTokens:  event.Message.Usage.InputTokens,
			}
		}
		return chunk, false, nil

	case "content_block_start":
		// Annotations input is empty or partial at block start (Anthropic sends input: {}).
		// Actual arguments arrive incrementally via input_json_delta using accumulated
		// partial JSON. Setting Arguments to "" lets mergeStreamToolCalls handle it correctly.

		if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
			return &model.ChatCompletionChunk{
				Choices: []model.ChunkChoice{
					{
						Index: 0,
						Delta: model.Delta{
							ToolCalls: []model.ChunkToolCall{
								{
									Index: event.Index,
									ID:    event.ContentBlock.ID,
									Type:  "function",
									Function: model.ChunkToolCallFunction{
										Name:      event.ContentBlock.Name,
										Arguments: "",
									},
								},
							},
						},
					},
				},
			}, false, nil
		}

	case "content_block_delta":
		if event.Delta != nil && event.Delta.Type == "text_delta" {
			return &model.ChatCompletionChunk{
				Choices: []model.ChunkChoice{
					{
						Index: 0,
						Delta: model.Delta{Content: event.Delta.Text},
					},
				},
			}, false, nil
		} else if event.Delta != nil && event.Delta.Type == "input_json_delta" {
			return &model.ChatCompletionChunk{
				Choices: []model.ChunkChoice{
					{
						Index: 0,
						Delta: model.Delta{
							ToolCalls: []model.ChunkToolCall{
								{
									Index: event.Index,
									Function: model.ChunkToolCallFunction{
										Arguments: event.Delta.PartialJSON,
									},
								},
							},
						},
					},
				},
			}, false, nil
		}

	case "message_delta":
		finishReason := "stop"
		if event.Delta != nil && event.Delta.StopReason == "max_tokens" {
			finishReason = "length"
		} else if event.Delta != nil && event.Delta.StopReason == "tool_use" {
			finishReason = "tool_calls"
		}
		chunk := &model.ChatCompletionChunk{
			Choices: []model.ChunkChoice{
				{
					Index:        0,
					Delta:        model.Delta{},
					FinishReason: &finishReason,
				},
			},
		}
		if event.Usage != nil && event.Usage.OutputTokens > 0 {
			chunk.Usage = &model.Usage{
				CompletionTokens: event.Usage.OutputTokens,
				TotalTokens:      event.Usage.OutputTokens,
			}
		}
		return chunk, false, nil

	case "message_stop":
		return nil, true, nil

	case "error":
		return nil, false, fmt.Errorf("provider error: %s", string(data))
	}

	return nil, false, nil
}

func (p *AnthropicProvider) DiscoverModels(ctx context.Context) ([]catalog.ModelInfo, error) {
	if len(p.config.GetAPIKeys()) == 0 {
		slog.Debug("skipping model discovery, no credentials configured",
			"provider", p.config.Name, "type", "anthropic")
		return nil, nil
	}

	url := fmt.Sprintf("%s/models", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return p.discoverModelsFallback()
	}
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return p.discoverModelsFallback()
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return p.discoverModelsFallback()
	}

	if resp.StatusCode != http.StatusOK {
		return p.discoverModelsFallback()
	}

	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &modelsResp); err != nil {
		return p.discoverModelsFallback()
	}

	var models []catalog.ModelInfo
	for _, entry := range modelsResp.Data {
		if entry.ID == "" {
			continue
		}

		catalog.ModelsMu.RLock()
		entries, ok := catalog.Models[entry.ID]
		catalog.ModelsMu.RUnlock()

		if ok && len(entries) > 0 {
			regInfo := entries[0]
			regInfo.DefaultBaseURL = p.config.BaseURL
			models = append(models, regInfo)
			continue
		}

		tier := "standard"
		costIn := 0.000003
		costOut := 0.000015
		maxCtx := 200000
		maxOut := 8192
		caps := []string{"function_calling", "json_mode"}

		idLower := strings.ToLower(entry.ID)
		if strings.Contains(idLower, "opus") {
			costIn = 0.000015
			costOut = 0.000075
			tier = "premium"
			caps = append(caps, "vision")
		} else if strings.Contains(idLower, "sonnet") {
			costIn = 0.000003
			costOut = 0.000015
			tier = "standard"
			caps = append(caps, "vision")
		} else if strings.Contains(idLower, "haiku") {
			costIn = 0.0000008
			costOut = 0.000004
			tier = "economy"
		}

		models = append(models, catalog.ModelInfo{
			ID:                 entry.ID,
			Provider:           "anthropic",
			DisplayName:        entry.ID,
			MaxContextTokens:   maxCtx,
			MaxOutputTokens:    maxOut,
			CostPerInputToken:  costIn,
			CostPerOutputToken: costOut,
			Tier:               tier,
			Capabilities:       caps,
			DefaultBaseURL:     p.config.BaseURL,
		})
	}

	if len(models) == 0 {
		return p.discoverModelsFallback()
	}

	return models, nil
}

func (p *AnthropicProvider) discoverModelsFallback() ([]catalog.ModelInfo, error) {
	catalog.ModelsMu.RLock()
	defer catalog.ModelsMu.RUnlock()

	var models []catalog.ModelInfo
	for _, infos := range catalog.Models {
		for _, mInfo := range infos {
			if mInfo.Provider == "anthropic" {
				models = append(models, mInfo)
			}
		}
	}
	return models, nil
}
