package testutil

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// DelayedMockProvider is a test provider that can simulate delays and failures.
type DelayedMockProvider struct {
	name      string
	transport http.RoundTripper
	callCount atomic.Int64
}

func NewDelayedMockProvider(name string, delay time.Duration, failAfter int) *DelayedMockProvider {
	p := &DelayedMockProvider{name: name}
	p.transport = RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		time.Sleep(delay)
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		default:
		}
		n := p.callCount.Add(1)
		code := http.StatusOK
		if failAfter > 0 && n > int64(failAfter) {
			code = http.StatusInternalServerError
		}
		return &http.Response{StatusCode: code, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	return p
}

func (d *DelayedMockProvider) SetTransport(rt http.RoundTripper) { d.transport = rt }

func (d *DelayedMockProvider) Name() string { return d.name }

func (d *DelayedMockProvider) Type() string { return "delayed-mock" }

func (d *DelayedMockProvider) TransformRequest(_ context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(context.Background(), http.MethodPost, "http://delayed-mock", http.NoBody)
}

func (d *DelayedMockProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID: d.name + "-id", Model: d.name + "-model",
		Choices: []model.Choice{{Message: model.ChoiceMessage{Content: "delayed response from " + d.name}}},
	}, nil
}

func (d *DelayedMockProvider) Client() *http.Client {
	return &http.Client{Transport: d.transport, Timeout: 30 * time.Second}
}

func (d *DelayedMockProvider) HealthCheck(_ context.Context) error { return nil }

func (d *DelayedMockProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}
