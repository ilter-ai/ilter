package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// streamingMockProvider returns SSE chunks from its roundTripFunc.
type streamingMockProvider struct {
	transport http.RoundTripper
}

func (m *streamingMockProvider) Name() string { return "streaming-mock" }
func (m *streamingMockProvider) Type() string { return "streaming-mock" }
func (m *streamingMockProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://streaming-mock", nil)
}

func (m *streamingMockProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID:      "stream-test-id",
		Model:   "gpt-4o",
		Choices: []model.Choice{{Message: model.ChoiceMessage{Content: "stream response"}}},
	}, nil
}

func (m *streamingMockProvider) Client() *http.Client {
	return &http.Client{Transport: m.transport}
}
func (m *streamingMockProvider) HealthCheck(_ context.Context) error { return nil }
func (m *streamingMockProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

// sseChunk builds a single SSE data line from a ChatCompletionChunk.
func sseChunk(content string) string {
	chunk := model.ChatCompletionChunk{
		ID:      "chatcmpl-stream",
		Object:  "chat.completion.chunk",
		Created: 1677652288,
		Model:   "gpt-4o",
		Choices: []model.ChunkChoice{
			{
				Index: 0,
				Delta: model.Delta{Content: content},
			},
		},
	}
	b, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(b))
}

// sseDone returns the SSE termination marker.
func sseDone() string {
	return "data: [DONE]\n\n"
}

// setupStreamingE2E creates a chi router for testing streaming responses.
// It uses a roundTripFunc that returns SSE data built from the provided chunks.
func setupStreamingE2E(t *testing.T, sseContent string) *chi.Mux {
	t.Helper()

	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "streaming-mock",
				Type: "streaming-mock",
				Models: []config.ModelConfig{
					{Name: "gpt-4o", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: false,
		},
	}

	reg := provider.NewRegistry()
	transport := testutil.RoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		body := io.NopCloser(strings.NewReader(sseContent))
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header:     make(http.Header),
		}
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})
	reg.Register(&streamingMockProvider{transport: transport})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("Load balancer err: %v", err)
	}

	proxyHandler := proxy.NewHandler(lb, nil, nil, nil)
	proxyHandler.SetConfig(cfg)

	authMw := middleware.NewAuthMiddleware(cfg.Auth, nil)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	})
	r.Use(authMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)
	return r
}

// TestStreamingE2E_BasicSSE verifies a basic streaming response returns proper SSE chunks
// and the [DONE] termination marker.
func TestStreamingE2E_BasicSSE(t *testing.T) {
	sseData := sseChunk("Hello") + sseChunk(" world") + sseDone()
	r := setupStreamingE2E(t, sseData)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Say hello"}},
		Stream:   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", contentType)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Error("response body does not contain SSE data lines")
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("response body does not contain [DONE] marker")
	}
	if !strings.Contains(body, "Hello") {
		t.Error("response body does not contain expected content")
	}
}

// TestStreamingE2E_ContentThroughput verifies that streaming content is forwarded correctly
// with all chunks preserved through the middleware chain.
func TestStreamingE2E_ContentThroughput(t *testing.T) {
	// Multiple chunks to test accumulation
	sseData := sseChunk("The ") + sseChunk("quick ") + sseChunk("brown ") + sseChunk("fox ") + sseDone()
	r := setupStreamingE2E(t, sseData)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Tell me about foxes"}},
		Stream:   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "The ") {
		t.Error("missing first chunk content")
	}
	if !strings.Contains(body, "quick ") {
		t.Error("missing subsequent chunk content")
	}
	if !strings.Contains(body, "brown ") {
		t.Error("missing another chunk content")
	}
	if !strings.Contains(body, "fox ") {
		t.Error("missing last chunk content")
	}
}

// TestStreamingE2E_SSEFormat verifies the SSE wire format: "data: <json>\n\n" for each chunk
// and "data: [DONE]\n\n" as the termination marker.
func TestStreamingE2E_SSEFormat(t *testing.T) {
	sseData := sseChunk("test") + sseDone()
	r := setupStreamingE2E(t, sseData)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Test"}},
		Stream:   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	body := rr.Body.String()
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")

	dataLineCount := 0
	doneFound := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			data := after
			if data == "[DONE]" {
				doneFound = true
			} else {
				dataLineCount++
				// Each data line (except [DONE]) must be valid JSON
				var parsed map[string]any
				if err := json.Unmarshal([]byte(data), &parsed); err != nil {
					t.Errorf("SSE data line is not valid JSON: %q - %v", data, err)
				}
			}
		}
	}

	if dataLineCount == 0 {
		t.Error("no SSE data lines found")
	}
	if !doneFound {
		t.Error("missing [DONE] termination marker")
	}
}

// TestStreamingE2E_NonStreamingResponse verifies that a request with stream=false returns a
// normal JSON response, not SSE chunks.
func TestStreamingE2E_NonStreamingResponse(t *testing.T) {
	sseData := sseChunk("Hello") + sseDone()
	r := setupStreamingE2E(t, sseData)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Say hello"}},
		Stream:   false,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	// Response should be valid JSON, not SSE
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("non-streaming response is not valid JSON: %v - body: %s", err, rr.Body.String())
	}
}

// TestStreamingE2E_EmptyChunks verifies that empty chunks between SSE data lines are handled
// gracefully (they should be skipped).
func TestStreamingE2E_EmptyChunks(t *testing.T) {
	// SSE with empty lines interspersed (common in real SSE streams)
	sseData := "data: {\"test\":\"keep\"}\n\n\n\ndata: [DONE]\n\n"
	r := setupStreamingE2E(t, sseData)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Test"}},
		Stream:   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestStreamingE2E_Headers verifies that the streaming response has correct SSE headers.
func TestStreamingE2E_Headers(t *testing.T) {
	sseData := sseChunk("Hello") + sseDone()
	r := setupStreamingE2E(t, sseData)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
		Stream:   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if rr.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type: text/event-stream, got %s", rr.Header().Get("Content-Type"))
	}
	if rr.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %s", rr.Header().Get("Cache-Control"))
	}
	if rr.Header().Get("Connection") != "keep-alive" {
		t.Errorf("expected Connection: keep-alive, got %s", rr.Header().Get("Connection"))
	}
}

// TestStreamingE2E_NoBodyResponse verifies the handler gracefully handles an empty body from provider.
func TestStreamingE2E_NoBodyResponse(t *testing.T) {
	// Empty SSE (no chunks at all, not even [DONE])
	r := setupStreamingE2E(t, "")

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
		Stream:   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestStreamingE2E_ChunkOrdering verifies that multiple SSE chunks maintain their ordering.
func TestStreamingE2E_ChunkOrdering(t *testing.T) {
	sseData := sseChunk("first ") + sseChunk("second ") + sseChunk("third ") + sseDone()
	r := setupStreamingE2E(t, sseData)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Count"}},
		Stream:   true,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	body := rr.Body.String()
	firstIdx := strings.Index(body, "first ")
	secondIdx := strings.Index(body, "second ")
	thirdIdx := strings.Index(body, "third ")

	if firstIdx == -1 {
		t.Error("missing 'first ' in output")
	}
	if secondIdx == -1 {
		t.Error("missing 'second ' in output")
	}
	if thirdIdx == -1 {
		t.Error("missing 'third ' in output")
	}

	if firstIdx > secondIdx {
		t.Error("chunk ordering broken: 'first ' appears after 'second '")
	}
	if secondIdx > thirdIdx {
		t.Error("chunk ordering broken: 'second ' appears after 'third '")
	}
}
