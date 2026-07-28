package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

type OpenAIProvider struct {
	config   config.ProviderConfig
	client   *http.Client
	provType string
}

func NewOpenAIProvider(cfg config.ProviderConfig) *OpenAIProvider {
	return &OpenAIProvider{
		config:   cfg,
		client:   NewResilientClient(cfg),
		provType: cfg.Type,
	}
}

func (p *OpenAIProvider) Name() string {
	return p.config.Name
}

func (p *OpenAIProvider) APIKeys() []string {
	return p.config.GetAPIKeys()
}

func (p *OpenAIProvider) Type() string {
	if p.provType != "" {
		return p.provType
	}
	return "openai"
}

func (p *OpenAIProvider) TransformRequest(ctx context.Context, req *model.ChatCompletionRequest) (*http.Request, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	bodyBytes := buf.Bytes()

	url := fmt.Sprintf("%s/chat/completions", p.config.BaseURL)
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
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	for k, v := range p.config.Headers {
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}

func (p *OpenAIProvider) TransformResponse(_ context.Context, resp *http.Response) (*model.ChatCompletionResponse, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned status %d: %s", resp.StatusCode, strings.ReplaceAll(strings.ReplaceAll(string(bodyBytes), "\n", " "), "\r", ""))
	}

	var result struct {
		Error *model.ErrorDetail `json:"error,omitempty"`
		model.ChatCompletionResponse
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if result.Error != nil && result.Error.Message != "" {
		return nil, fmt.Errorf("provider returned error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("provider returned response with no choices (quota exhausted or upstream model error)")
	}

	for _, choice := range result.Choices {
		if strings.TrimSpace(choice.Message.Content) == "" && strings.TrimSpace(choice.FinishReason) == "" {
			return nil, fmt.Errorf("provider returned response with empty choice content (quota exhausted or upstream model error)")
		}
	}

	return &result.ChatCompletionResponse, nil
}

func (p *OpenAIProvider) Client() *http.Client {
	return p.client
}

// discoverModelHeuristics applies naming convention heuristics per provider type
// to estimate pricing, tier, context limits, and capabilities for unknown models.
func (p *OpenAIProvider) discoverModelHeuristics(modelID string) (tier string, costIn, costOut float64, maxCtx, maxOut int, caps []string) {
	tier = "standard"
	costIn = 0.000001
	costOut = 0.000002
	maxCtx = 128000
	maxOut = 4096
	caps = []string{"function_calling"}

	idLower := strings.ToLower(modelID)
	pType := p.provType
	if pType == "" {
		pType = "openai"
	}

	switch pType {
	case "deepseek":
		if strings.Contains(idLower, "reasoner") || strings.Contains(idLower, "r1") {
			costIn = 0.00000055
			costOut = 0.00000219
			tier = "standard"
			maxCtx = 64000
			caps = nil
		} else {
			costIn = 0.00000014
			costOut = 0.00000028
			tier = "economy"
			maxCtx = 64000
			caps = []string{"function_calling", "json_mode"}
		}
	case "qwen":
		switch {
		case strings.Contains(idLower, "turbo"), strings.Contains(idLower, "coder"):
			costIn, costOut, tier = 0.0000003, 0.0000006, "economy"
		case strings.Contains(idLower, "plus"):
			costIn, costOut, tier = 0.0000008, 0.000002, "standard"
		case strings.Contains(idLower, "max"), strings.Contains(idLower, "math"):
			costIn, costOut, tier = 0.0000028, 0.0000084, "premium"
		default:
			costIn, costOut, tier = 0.0000002, 0.0000006, "economy"
		}
	case "gemini":
		switch {
		case strings.Contains(idLower, "flash-lite"):
			costIn, costOut, tier = 0.000000075, 0.0000003, "economy"
		case strings.Contains(idLower, "flash"):
			costIn, costOut, tier = 0.000000075, 0.0000003, "economy"
			caps = append(caps, "vision")
		case strings.Contains(idLower, "pro"):
			costIn, costOut, tier = 0.00000125, 0.000005, "premium"
			caps = append(caps, "vision")
		}
		maxCtx = 1048576
	case "opencode_zen", "opencode_go":
		switch {
		case strings.Contains(idLower, "free"):
			costIn, costOut, tier = 0.0, 0.0, "free"
		case strings.Contains(idLower, "pro"), strings.Contains(idLower, "plus"):
			costIn, costOut, tier = 0.000001, 0.000002, "standard"
		default:
			costIn, costOut, tier = 0.0000005, 0.000001, "economy"
		}
	case "openai":
		switch {
		case strings.Contains(idLower, "mini"):
			costIn, costOut, tier = 0.00000015, 0.0000006, "economy"
		case strings.Contains(idLower, "gpt-4"), strings.Contains(idLower, "gpt-5"):
			costIn, costOut, tier = 0.0000025, 0.00001, "standard"
		}
		caps = []string{"function_calling", "vision", "json_mode"}
	}
	return
}

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/models", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}
	return nil
}

func (p *OpenAIProvider) TransformStreamChunk(data []byte) (*model.ChatCompletionChunk, bool, error) {
	if string(data) == "[DONE]" {
		return nil, true, nil
	}

	var chunk model.ChatCompletionChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, false, fmt.Errorf("failed to decode chunk: %w", err)
	}

	return &chunk, false, nil
}

type openAIModelEntry struct {
	ID string `json:"id"`
}

type openAIModelsResponse struct {
	Data []openAIModelEntry `json:"data"`
}

type openRouterModelEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	Pricing       struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	SupportedParameters []string `json:"supported_parameters"`
}

type openRouterModelsResponse struct {
	Data []openRouterModelEntry `json:"data"`
}

func parseOpenRouterPrice(s string) float64 {
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return val
}

func (p *OpenAIProvider) DiscoverModels(ctx context.Context) ([]catalog.ModelInfo, error) {
	// serve a public /models endpoint and intentionally skip this guard.
	if len(p.config.GetAPIKeys()) == 0 && !p.config.DiscoveryPublic {
		slog.Debug("skipping model discovery, no credentials configured",
			"provider", p.config.Name, "type", p.provType)
		return nil, nil
	}

	url := fmt.Sprintf("%s/models", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to discover models, status %d: %s", resp.StatusCode, strings.ReplaceAll(strings.ReplaceAll(string(bodyBytes), "\n", " "), "\r", ""))
	}

	var modelsResp openAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &modelsResp); err != nil {
		return nil, err
	}

	var models []catalog.ModelInfo
	for _, entry := range modelsResp.Data {
		if entry.ID == "" {
			continue
		}

		catalog.ModelsMu.RLock()
		existing, exists := catalog.Models[entry.ID]
		catalog.ModelsMu.RUnlock()

		if exists && len(existing) > 0 {
			regInfo := existing[0]
			regInfo.Provider = p.provType
			if p.provType == "" {
				regInfo.Provider = "openai"
			}
			regInfo.DefaultBaseURL = p.config.BaseURL
			if p.config.Type == "opencode_zen" || p.config.Type == "opencode_go" {
				idLower := strings.ToLower(entry.ID)
				if strings.Contains(idLower, "free") {
					regInfo.CostPerInputToken = 0.0
					regInfo.CostPerOutputToken = 0.0
					regInfo.Tier = "free"
				}
			}
			models = append(models, regInfo)
			continue
		}

		tier, costIn, costOut, maxCtx, maxOut, caps := p.discoverModelHeuristics(entry.ID)

		provType := p.provType
		if provType == "" {
			provType = "openai"
		}
		models = append(models, catalog.ModelInfo{
			ID:                 entry.ID,
			Provider:           provType,
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
	return models, nil
}

func (p *OpenAIProvider) UpdateConfig(baseURL string, apiKey string) {
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

func (p *OpenAIProvider) UpdateKeys(baseURL string, apiKey string, apiKeys []string) {
	if baseURL != "" {
		p.config.BaseURL = baseURL
	}
	p.config.APIKey = apiKey
	p.config.APIKeys = apiKeys
}
