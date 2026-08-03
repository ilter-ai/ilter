package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
)

// streamMockProvider implements both Provider and StreamingProvider for testing.
type streamMockProvider struct {
	name         string
	typ          string
	chunks       []*model.ChatCompletionChunk
	done         bool
	transformErr error
}

func (s *streamMockProvider) Name() string { return s.name }
func (s *streamMockProvider) Type() string { return s.typ }

func (s *streamMockProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://mock-stream", nil)
}

func (s *streamMockProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{ID: "mock-id", Model: "mock-model"}, nil
}

func (s *streamMockProvider) Client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}
}

func (s *streamMockProvider) HealthCheck(_ context.Context) error { return nil }
func (s *streamMockProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (s *streamMockProvider) TransformStreamChunk(data []byte) (*model.ChatCompletionChunk, bool, error) {
	if s.transformErr != nil {
		return nil, false, s.transformErr
	}
	if bytes.Equal(data, []byte("[DONE]")) {
		return nil, true, nil
	}
	if s.done || len(s.chunks) == 0 {
		return nil, true, nil
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, false, nil
}

// sseLines extracts all "data: ..." lines from an SSE response body.
func sseLines(t *testing.T, body []byte) []string {
	t.Helper()
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			lines = append(lines, after)
		}
	}
	return lines
}

// makeChunk creates a minimal ChatCompletionChunk with the given delta content.
func makeChunk(content string) *model.ChatCompletionChunk {
	return &model.ChatCompletionChunk{
		ID:      "chatcmpl-test",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "test-model",
		Choices: []model.ChunkChoice{
			{Index: 0, Delta: model.Delta{Content: content}},
		},
	}
}

func TestHandleStreaming_NormalFlow(t *testing.T) {
	t.Parallel()
	lb := newTestStreamLB(t, "openai-stream")
	h := NewHandler(lb, nil, nil, nil)

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	chunk1 := makeChunk("Hello ")
	chunk2 := makeChunk("world")
	body := fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n",
		toJSON(t, chunk1), toJSON(t, chunk2))

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	rr := httptest.NewRecorder()
	mock := provider.NewMockProvider("openai-stream")
	route := &smartrouter.Route{
		Provider: mock,
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	start := time.Now()
	h.handleStreaming(context.Background(), func() {}, rr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))

	dataLines := sseLines(t, rr.Body.Bytes())
	require.GreaterOrEqual(t, len(dataLines), 3)
	assert.Contains(t, dataLines[len(dataLines)-1], "[DONE]")
}

func TestHandleStreaming_ClientDisconnect(t *testing.T) {
	t.Parallel()
	lb := newTestStreamLB(t, "openai-stream")
	h := NewHandler(lb, nil, nil, nil)

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	// Pipe-based body so we can cancel mid-stream
	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr}

	ctx, cancel := context.WithCancel(context.Background())

	rr := httptest.NewRecorder()
	route := &smartrouter.Route{
		Provider: &streamMockProvider{name: "openai-stream", typ: "openai"},
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	start := time.Now()
	done := make(chan struct{})
	go func() {
		h.handleStreaming(ctx, cancel, rr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)
		close(done)
	}()

	// Write a chunk then cancel
	chunk1 := toJSON(t, makeChunk("Hello "))
	_, _ = pw.Write([]byte("data: " + chunk1 + "\n\n"))
	cancel()
	_ = pw.Close()

	select {
	case <-done:
		// Expected: handleStreaming returned cleanly after ctx cancellation
	case <-time.After(5 * time.Second):
		t.Fatal("handleStreaming did not return after context cancellation")
	}
}

func TestHandleStreaming_ProviderReadError(t *testing.T) {
	t.Parallel()
	lb := newTestStreamLB(t, "openai-stream")
	h := NewHandler(lb, nil, nil, nil)

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	errBody := &errAfterRead{msg: "connection reset by provider", after: 50}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errBody),
	}

	rr := httptest.NewRecorder()
	route := &smartrouter.Route{
		Provider: &streamMockProvider{name: "openai-stream", typ: "openai"},
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	start := time.Now()
	h.handleStreaming(context.Background(), func() {}, rr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)

	code := rr.Code
	if code != http.StatusOK && code != http.StatusBadGateway {
		t.Fatalf("expected 200 or 502, got %d. Body: %s", code, rr.Body.String())
	}
}

func TestHandleStreaming_MissingFlusher(t *testing.T) {
	t.Parallel()
	lb := newTestStreamLB(t, "openai-stream")
	h := NewHandler(lb, nil, nil, nil)

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	nfr := &nonFlushRecorder{inner: httptest.NewRecorder()}
	route := &smartrouter.Route{
		Provider: &streamMockProvider{name: "openai-stream", typ: "openai"},
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	start := time.Now()
	h.handleStreaming(context.Background(), func() {}, nfr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)

	if nfr.Code() != http.StatusInternalServerError {
		t.Fatalf("expected 500 (missing Flusher), got %d. Body: %s", nfr.Code(), nfr.inner.Body.String())
	}
}

func TestHandleStreaming_FlushAfterEOFSend(t *testing.T) {
	t.Parallel()
	// Regression test: after io.EOF we must call flusher.Flush()
	lb := newTestStreamLB(t, "flush-eof")
	h := NewHandler(lb, nil, nil, nil)

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	// Empty SSE stream — immediate EOF
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	rr := httptest.NewRecorder()
	route := &smartrouter.Route{
		Provider: &streamMockProvider{name: "flush-eof", typ: "openai"},
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	start := time.Now()
	h.handleStreaming(context.Background(), func() {}, rr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)

	require.Equal(t, http.StatusOK, rr.Code)
}

func TestHandleStreaming_StreamingProviderPath(t *testing.T) {
	t.Parallel()
	// Provider implements StreamingProvider — tests TransformStreamChunk path
	lb := newTestStreamLB(t, "transform-stream")
	h := NewHandler(lb, nil, nil, nil)

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	body := "data: {\"type\":\"content\",\"text\":\"Hello\"}\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	sm := &streamMockProvider{name: "transform-stream", typ: "anthropic"}
	sm.chunks = []*model.ChatCompletionChunk{makeChunk("Hello")}

	rr := httptest.NewRecorder()
	route := &smartrouter.Route{
		Provider: sm,
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	start := time.Now()
	h.handleStreaming(context.Background(), func() {}, rr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)

	require.Equal(t, http.StatusOK, rr.Code)
	dataLines := sseLines(t, rr.Body.Bytes())
	require.GreaterOrEqual(t, len(dataLines), 2)
	assert.Contains(t, dataLines[len(dataLines)-1], "[DONE]")
}

// -- helpers ----------------------------------------------------------------

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func newTestStreamLB(t *testing.T, providerName string) *smartrouter.LoadBalancer {
	t.Helper()
	cfg := &config.Config{
		Providers: []config.ProviderConfig{{
			Name: providerName,
			Type: "openai",
			Models: []config.ModelConfig{{
				Name:   "gpt-4o-mini",
				Weight: 1,
			}},
		}},
	}
	reg := provider.NewRegistry()
	reg.Register(&streamMockProvider{name: providerName, typ: "openai"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)
	return lb
}

// errAfterRead returns an error after a certain number of successful Read calls.
type errAfterRead struct {
	msg   string
	after int
	count int
}

func (e *errAfterRead) Read(p []byte) (int, error) {
	e.count += len(p)
	if e.count > e.after {
		return 0, fmt.Errorf("%s", e.msg)
	}
	return len(p), nil
}

// nonFlushRecorder does NOT implement http.Flusher (httptest.ResponseRecorder does,
// so we store it as a named field instead of embedding to avoid Flush promotion).
type nonFlushRecorder struct {
	inner *httptest.ResponseRecorder
}

func (n *nonFlushRecorder) Header() http.Header         { return n.inner.Header() }
func (n *nonFlushRecorder) WriteHeader(c int)           { n.inner.WriteHeader(c) }
func (n *nonFlushRecorder) Write(b []byte) (int, error) { return n.inner.Write(b) }
func (n *nonFlushRecorder) Code() int                   { return n.inner.Code }

func TestHandleStreaming_OutputLoopDetection_EnforceMode(t *testing.T) {
	t.Parallel()

	lb := newTestStreamLB(t, "openai-stream")
	h := NewHandler(lb, nil, nil, nil)
	h.cfg = &config.Config{
		CostGuard: config.CostGuardConfig{
			LoopSettings: config.LoopSettingsConfig{
				OutputLoopMode:      "enforce",
				OutputLoopThreshold: 3,
				OutputMinSentence:   10,
			},
		},
	}

	// Build SSE body with 5 copies of the same sentence
	sentence := "Tabii, şimdi sorguluyorum:"
	var bodyParts []string
	for range 5 {
		chunk := makeChunk(sentence)
		bodyParts = append(bodyParts, "data: "+toJSON(t, chunk)+"\n\n")
	}
	bodyParts = append(bodyParts, "data: [DONE]\n\n")
	body := strings.Join(bodyParts, "")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	rr := httptest.NewRecorder()
	mock := provider.NewMockProvider("openai-stream")
	route := &smartrouter.Route{
		Provider: mock,
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	start := time.Now()

	h.handleStreaming(ctx, cancel, rr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)

	// Parse SSE response and find the last chunk before [DONE]
	dataLines := sseLines(t, rr.Body.Bytes())
	require.GreaterOrEqual(t, len(dataLines), 2, "should have at least 2 data lines")

	var loopDetectedChunk *model.ChatCompletionChunk
	for _, line := range dataLines {
		if line == "[DONE]" {
			break
		}
		var chunk model.ChatCompletionChunk
		if err := json.Unmarshal([]byte(line), &chunk); err == nil {
			loopDetectedChunk = &chunk
		}
	}

	require.NotNil(t, loopDetectedChunk, "expected at least one valid chunk in SSE body")
	require.Len(t, loopDetectedChunk.Choices, 1, "expected 1 choice in chunk")

	finishReason := loopDetectedChunk.Choices[0].FinishReason
	require.NotNil(t, finishReason, "expected finish_reason to be set")
	assert.Equal(t, "loop_detected", *finishReason, "finish_reason should be loop_detected")

	// Verify upstream context was canceled (stops billing)
	select {
	case <-ctx.Done():
		// expected — context canceled
	default:
		t.Error("expected upstream context to be canceled after output loop detection")
	}
}

func TestHandleStreaming_OutputLoopDetection_ObserveMode(t *testing.T) {
	t.Parallel()

	lb := newTestStreamLB(t, "openai-stream")
	h := NewHandler(lb, nil, nil, nil)
	h.cfg = &config.Config{
		CostGuard: config.CostGuardConfig{
			LoopSettings: config.LoopSettingsConfig{
				OutputLoopMode:      "observe",
				OutputLoopThreshold: 2,
				OutputMinSentence:   10,
			},
		},
	}

	// Build SSE body with 3 copies of the same sentence
	sentence := "Aynı cümle tekrar ediyor. "
	var bodyParts []string
	for range 3 {
		chunk := makeChunk(sentence)
		bodyParts = append(bodyParts, "data: "+toJSON(t, chunk)+"\n\n")
	}
	bodyParts = append(bodyParts, "data: [DONE]\n\n")
	body := strings.Join(bodyParts, "")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	rr := httptest.NewRecorder()
	mock := provider.NewMockProvider("openai-stream")
	route := &smartrouter.Route{
		Provider: mock,
		Model:    config.ModelConfig{Name: "gpt-4o-mini"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	start := time.Now()

	h.handleStreaming(ctx, cancel, rr, resp, route.Provider, route, "gpt-4o-mini", start, httpReq, req)

	// Observe mode: stream should complete fully (all chunks + [DONE]), not cut short
	dataLines := sseLines(t, rr.Body.Bytes())
	require.GreaterOrEqual(t, len(dataLines), 4, "observe mode should pass all chunks through")
	assert.Contains(t, dataLines[len(dataLines)-1], "[DONE]", "last line should be [DONE]")

	// Context should NOT be canceled in observe mode
	select {
	case <-ctx.Done():
		t.Error("context should NOT be canceled in observe mode")
	default:
		// expected
	}
}

func TestHandleStreaming_OutputLoopDetection_OffMode(t *testing.T) {
	t.Parallel()

	lb := newTestStreamLB(t, "openai-stream")
	h := NewHandler(lb, nil, nil, nil)
	h.cfg = &config.Config{
		CostGuard: config.CostGuardConfig{
			LoopSettings: config.LoopSettingsConfig{
				OutputLoopMode:      "off",
				OutputLoopThreshold: 2,
				OutputMinSentence:   10,
			},
		},
	}

	sentence := "Aynı cümle. "
	var bodyParts []string
	for range 5 {
		chunk := makeChunk(sentence)
		bodyParts = append(bodyParts, "data: "+toJSON(t, chunk)+"\n\n")
	}
	bodyParts = append(bodyParts, "data: [DONE]\n\n")
	body := strings.Join(bodyParts, "")

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	rr := httptest.NewRecorder()
	mock := provider.NewMockProvider("openai-stream")
	route := &smartrouter.Route{Provider: mock, Model: config.ModelConfig{Name: "gpt-4o-mini"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &model.ChatCompletionRequest{Model: "gpt-4o-mini", Stream: true, Messages: []model.Message{{Role: "user", Content: "hello"}}}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	h.handleStreaming(ctx, cancel, rr, resp, route.Provider, route, "gpt-4o-mini", time.Now(), httpReq, req)

	// Off mode: no detection, full stream delivered
	dataLines := sseLines(t, rr.Body.Bytes())
	require.GreaterOrEqual(t, len(dataLines), 6, "off mode should deliver all chunks")
	assert.Contains(t, dataLines[len(dataLines)-1], "[DONE]")
}
