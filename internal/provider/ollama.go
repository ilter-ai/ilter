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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

type OllamaProvider struct {
	openAI     *OpenAIProvider
	config     config.ProviderConfig
	detectOnce sync.Once
	useNative  bool
	detectErr  error
}

func NewOllamaProvider(cfg config.ProviderConfig) *OllamaProvider {
	baseURL := cfg.BaseURL
	if baseURL == "http://localhost:11434" {
		baseURL = "http://localhost:11434/v1"
	}

	openAICfg := cfg
	openAICfg.BaseURL = baseURL
	openAICfg.DiscoveryPublic = true // Ollama is local — no auth needed for /v1/models

	return &OllamaProvider{
		openAI: NewOpenAIProvider(openAICfg),
		config: cfg,
	}
}

// detectMode probes once whether Ollama supports the OpenAI-compatible (/v1/models)
// or native (/api/tags) endpoint. After detection, useNative is read-only and safe
// to access without synchronization.
func (p *OllamaProvider) detectMode(ctx context.Context) error {
	p.detectOnce.Do(func() {
		v1URL := fmt.Sprintf("%s/v1/models", p.config.BaseURL)
		if p.config.BaseURL == "http://localhost:11434" {
			v1URL = "http://localhost:11434/v1/models"
		}

		req, err := http.NewRequestWithContext(ctx, "GET", v1URL, nil)
		if err != nil {
			p.detectErr = fmt.Errorf("failed to create v1 endpoint request: %w", err)
			return
		}
		resp, err := p.openAI.client.Do(req)
		if err != nil {
			if resp != nil {
				resp.Body.Close()
			}
		} else {
			// resp.Body must be closed before the sync.Once closure returns.
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				p.useNative = false
				return
			}
		}

		nativeURL := fmt.Sprintf("%s/api/tags", p.config.BaseURL)
		req2, err := http.NewRequestWithContext(ctx, "GET", nativeURL, nil)
		if err != nil {
			p.detectErr = fmt.Errorf("failed to create native endpoint request: %w", err)
			return
		}
		resp2, err2 := p.openAI.client.Do(req2)
		if err2 != nil {
			if resp2 != nil {
				resp2.Body.Close()
			}
			p.detectErr = err2
			return
		}
		defer resp2.Body.Close()

		if resp2.StatusCode == http.StatusOK {
			p.useNative = true
			return
		}

		p.detectErr = fmt.Errorf("both ollama endpoints failed")
	})
	return p.detectErr
}

func (p *OllamaProvider) Name() string {
	return p.config.Name
}

func (p *OllamaProvider) Type() string {
	return "ollama"
}

type ollamaNativeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaNativeOptions struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	NumPredict       *int     `json:"num_predict,omitempty"`
	Stop             []string `json:"stop,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
}

type ollamaNativeRequest struct {
	Model    string                `json:"model"`
	Messages []ollamaNativeMessage `json:"messages"`
	Stream   bool                  `json:"stream"`
	Options  *ollamaNativeOptions  `json:"options,omitempty"`
}

type ollamaNativeResponse struct {
	Model           string              `json:"model"`
	CreatedAt       string              `json:"created_at"`
	Message         ollamaNativeMessage `json:"message"`
	Done            bool                `json:"done"`
	PromptEvalCount int                 `json:"prompt_eval_count"`
	EvalCount       int                 `json:"eval_count"`
}

func (p *OllamaProvider) TransformRequest(ctx context.Context, req *model.ChatCompletionRequest) (*http.Request, error) {
	if err := p.detectMode(ctx); err != nil {
		return nil, fmt.Errorf("failed to detect ollama mode: %w", err)
	}
	if !p.useNative {
		httpReq, err := p.openAI.TransformRequest(ctx, req)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Del("Authorization")
		return httpReq, nil
	}

	nativeReq := ollamaNativeRequest{
		Model:  req.Model,
		Stream: req.Stream,
	}

	for _, m := range req.Messages {
		if contentStr, ok := m.Content.(string); ok {
			nativeReq.Messages = append(nativeReq.Messages, ollamaNativeMessage{
				Role:    m.Role,
				Content: contentStr,
			})
		}
	}

	options := &ollamaNativeOptions{
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		NumPredict:       req.MaxTokens,
		Stop:             req.Stop,
		PresencePenalty:  req.PresencePenalty,
		FrequencyPenalty: req.FrequencyPenalty,
	}
	nativeReq.Options = options

	bodyBytes, err := json.Marshal(nativeReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/chat", p.config.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (p *OllamaProvider) TransformResponse(ctx context.Context, resp *http.Response) (*model.ChatCompletionResponse, error) {
	if err := p.detectMode(ctx); err != nil {
		return nil, fmt.Errorf("failed to detect ollama mode: %w", err)
	}
	if !p.useNative {
		return p.openAI.TransformResponse(ctx, resp)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama native returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var nativeResp ollamaNativeResponse
	if err := json.Unmarshal(bodyBytes, &nativeResp); err != nil {
		return nil, err
	}

	createdTime, _ := time.Parse(time.RFC3339, nativeResp.CreatedAt)

	finishReason := "stop"

	chatResp := &model.ChatCompletionResponse{
		ID:      "ollama-" + uuid.New().String(),
		Object:  "chat.completion",
		Created: createdTime.Unix(),
		Model:   nativeResp.Model,
		Choices: []model.Choice{
			{
				Index: 0,
				Message: model.ChoiceMessage{
					Role:    nativeResp.Message.Role,
					Content: nativeResp.Message.Content,
				},
				FinishReason: finishReason,
			},
		},
		Usage: &model.Usage{
			PromptTokens:     nativeResp.PromptEvalCount,
			CompletionTokens: nativeResp.EvalCount,
			TotalTokens:      nativeResp.PromptEvalCount + nativeResp.EvalCount,
		},
	}

	return chatResp, nil
}

func (p *OllamaProvider) Client() *http.Client {
	return p.openAI.Client()
}

func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	return p.detectMode(ctx)
}

func (p *OllamaProvider) TransformStreamChunk(data []byte) (*model.ChatCompletionChunk, bool, error) {
	if err := p.detectMode(context.Background()); err != nil {
		slog.Error("ollama detectMode failed in TransformStreamChunk", "error", err)
	}
	if !p.useNative {
		return p.openAI.TransformStreamChunk(data)
	}

	var nativeChunk ollamaNativeResponse

	cleanData := data
	if strings.HasPrefix(string(data), "data: ") {
		cleanData = data[6:]
	}

	if err := json.Unmarshal(cleanData, &nativeChunk); err != nil {
		return nil, false, nil
	}

	createdTime, _ := time.Parse(time.RFC3339, nativeChunk.CreatedAt)

	chunk := &model.ChatCompletionChunk{
		ID:      "ollama-" + uuid.New().String(),
		Object:  "chat.completion.chunk",
		Created: createdTime.Unix(),
		Model:   nativeChunk.Model,
		Choices: []model.ChunkChoice{
			{
				Index: 0,
				Delta: model.Delta{
					Content: nativeChunk.Message.Content,
				},
			},
		},
	}

	if nativeChunk.Done {
		finishReason := "stop"
		chunk.Choices[0].FinishReason = &finishReason
		return chunk, true, nil
	}

	return chunk, false, nil
}

type ollamaTagEntry struct {
	Name    string `json:"name"`
	Details struct {
		Family   string   `json:"family"`
		Families []string `json:"families"`
	} `json:"details"`
}

type ollamaTagsResponse struct {
	Models []ollamaTagEntry `json:"models"`
}

func isEmbeddingOnlyModel(name, family string, families []string) bool {
	lowerName := strings.ToLower(name)
	if strings.Contains(lowerName, "embed") {
		return true
	}

	embeddingFamilies := map[string]bool{
		"bert":                  true,
		"nomic-bert":            true,
		"sentence-transformers": true,
	}
	if embeddingFamilies[strings.ToLower(family)] {
		return true
	}
	for _, f := range families {
		if embeddingFamilies[strings.ToLower(f)] {
			return true
		}
	}
	return false
}

func (p *OllamaProvider) DiscoverModels(ctx context.Context) ([]catalog.ModelInfo, error) {
	if err := p.detectMode(ctx); err != nil {
		openAIModels, err := p.openAI.DiscoverModels(ctx)
		if err != nil {
			return nil, err
		}
		return p.mapOpenAIModelsToOllama(openAIModels), nil
	}

	if !p.useNative {
		openAIModels, err := p.openAI.DiscoverModels(ctx)
		if err != nil {
			return nil, err
		}
		return p.mapOpenAIModelsToOllama(openAIModels), nil
	}

	url := fmt.Sprintf("%s/api/tags", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.openAI.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama tags returned status %d: %s", resp.StatusCode, strings.ReplaceAll(strings.ReplaceAll(string(bodyBytes), "\n", " "), "\r", ""))
	}

	var tagsResp ollamaTagsResponse
	if err := json.Unmarshal(bodyBytes, &tagsResp); err != nil {
		return nil, err
	}

	var models []catalog.ModelInfo
	for _, entry := range tagsResp.Models {
		if entry.Name == "" {
			continue
		}
		if isEmbeddingOnlyModel(entry.Name, entry.Details.Family, entry.Details.Families) {
			continue
		}
		catalog.ModelsMu.RLock()
		entries, ok := catalog.Models[entry.Name]
		catalog.ModelsMu.RUnlock()

		if ok && len(entries) > 0 {
			regInfo := entries[0]
			regInfo.DefaultBaseURL = p.config.BaseURL
			models = append(models, regInfo)
			continue
		}

		models = append(models, catalog.ModelInfo{
			ID:                 entry.Name,
			Provider:           "ollama",
			DisplayName:        entry.Name,
			MaxContextTokens:   8192,
			MaxOutputTokens:    2048,
			CostPerInputToken:  0.0,
			CostPerOutputToken: 0.0,
			Tier:               "free",
			Capabilities:       []string{"function_calling"},
			DefaultBaseURL:     p.config.BaseURL,
		})
	}
	return models, nil
}

func (p *OllamaProvider) mapOpenAIModelsToOllama(openAIModels []catalog.ModelInfo) []catalog.ModelInfo {
	var ollamaModels []catalog.ModelInfo
	for _, m := range openAIModels {
		if isEmbeddingOnlyModel(m.ID, "", nil) {
			continue
		}
		m.Provider = "ollama"
		m.CostPerInputToken = 0.0
		m.CostPerOutputToken = 0.0
		m.Tier = "free"
		ollamaModels = append(ollamaModels, m)
	}
	return ollamaModels
}

func (p *OllamaProvider) UpdateConfig(baseURL string, apiKey string) {
	if baseURL != "" {
		p.config.BaseURL = baseURL
	}
	p.config.APIKey = apiKey
	p.openAI.UpdateConfig(baseURL, apiKey)
}
