package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// MockProvider returns canned responses without making external API calls.
// Mock provider for development and testing. No API key required.
type MockProvider struct {
	name           string
	cannedResponse string
	cannedStream   []string
	errToReturn    error
}

func NewMockProvider(name string) *MockProvider {
	return &MockProvider{name: name}
}

func (m *MockProvider) Name() string { return m.name }
func (m *MockProvider) Type() string { return "mock" }

func (m *MockProvider) SetCannedResponse(content string) {
	m.cannedResponse = content
}

func (m *MockProvider) SetCannedStream(chunks []string) {
	m.cannedStream = chunks
}

func (m *MockProvider) SetError(err error) {
	m.errToReturn = err
}

func (m *MockProvider) TransformRequest(ctx context.Context, req *model.ChatCompletionRequest) (*http.Request, error) {
	if m.errToReturn != nil {
		return nil, m.errToReturn
	}

	content := m.cannedResponse
	if content == "" {
		content = mockResponse(req.Messages)
	}

	var body bytes.Buffer

	if req.Stream {
		var chunks []string
		if len(m.cannedStream) > 0 {
			chunks = m.cannedStream
		} else {
			words := strings.Fields(content)
			chunks = make([]string, len(words))
			for i, w := range words {
				chunks[i] = w + " "
			}
		}

		for _, chunkContent := range chunks {
			chunk := model.ChatCompletionChunk{
				Choices: []model.ChunkChoice{{
					Index: 0,
					Delta: model.Delta{Content: chunkContent},
				}},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(&body, "data: %s\n\n", data)
		}
		fmt.Fprint(&body, "data: [DONE]\n\n")
	} else {
		resp := model.ChatCompletionResponse{
			Choices: []model.Choice{{
				Index: 0,
				Message: model.ChoiceMessage{
					Role:    "assistant",
					Content: content,
				},
			}},
		}
		data, _ := json.Marshal(resp)
		body.Write(data)
	}

	fakeReq, err := http.NewRequestWithContext(ctx, "POST", "http://mock.local/chat", &body)
	if err != nil {
		return nil, fmt.Errorf("mock: create request: %w", err)
	}
	if req.Stream {
		fakeReq.Header.Set("Content-Type", "text/event-stream")
	} else {
		fakeReq.Header.Set("Content-Type", "application/json")
	}
	return fakeReq, nil
}

func (m *MockProvider) TransformResponse(_ context.Context, resp *http.Response) (*model.ChatCompletionResponse, error) {
	if m.errToReturn != nil {
		return nil, m.errToReturn
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mock: read body: %w", err)
	}
	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("mock: unmarshal: %w", err)
	}
	return &chatResp, nil
}

func (m *MockProvider) Client() *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			hdrs := make(http.Header)
			hdrs.Set("Content-Type", req.Header.Get("Content-Type"))
			hdrs.Set("Content-Length", fmt.Sprintf("%d", len(body)))
			return &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(bytes.NewReader(body)),
				ContentLength: int64(len(body)),
				Header:        hdrs,
			}, nil
		}),
	}
}

func (m *MockProvider) HealthCheck(_ context.Context) error {
	if m.errToReturn != nil {
		return m.errToReturn
	}
	return nil
}

func (m *MockProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	if m.errToReturn != nil {
		return nil, m.errToReturn
	}
	models := []catalog.ModelInfo{
		{ID: "mock-default", Provider: m.name, DisplayName: "Mock Default (Dev)", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "free"},
		{ID: "gpt-4o", Provider: m.name, DisplayName: "Gpt 4o", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "premium"},
		{ID: "gpt-4o-mini", Provider: m.name, DisplayName: "Gpt 4o Mini", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "standard"},
		{ID: "gpt-5.1", Provider: m.name, DisplayName: "Gpt 5.1", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "premium"},
		{ID: "gpt-5.2", Provider: m.name, DisplayName: "Gpt 5.2", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "economy"},
		{ID: "claude-sonnet-4", Provider: m.name, DisplayName: "Claude Sonnet 4", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "premium"},
		{ID: "claude-haiku-3", Provider: m.name, DisplayName: "Claude Haiku 3", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "standard"},
		{ID: "deepseek-chat", Provider: m.name, DisplayName: "Deepseek Chat", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "economy"},
		{ID: "gemini-2-flash", Provider: m.name, DisplayName: "Gemini 2 Flash", CostPerInputToken: 0, CostPerOutputToken: 0, Tier: "standard"},
	}
	return models, nil
}

func (m *MockProvider) TransformStreamChunk(data []byte) (*model.ChatCompletionChunk, bool, error) {
	line := strings.TrimSpace(string(data))
	if line == "" {
		return nil, false, nil
	}
	if line == "[DONE]" {
		return nil, true, nil
	}
	var chunk model.ChatCompletionChunk
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return nil, false, fmt.Errorf("mock: unmarshal chunk: %w", err)
	}
	return &chunk, false, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func mockResponse(messages []model.Message) string {
	userMsg := ""
	for _, m := range messages {
		if content, ok := m.Content.(string); ok && m.Role == "user" {
			userMsg = content
			break
		}
	}
	if userMsg == "" {
		return "Hello! (mock response)"
	}
	return fmt.Sprintf("Hello! You said: %q [mock: %s]", userMsg, "ilter dev")
}
