package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/model"
)

func TestMockProvider_CannedResponse(t *testing.T) {
	mp := NewMockProvider("test-mock")
	mp.SetCannedResponse("Custom canned response content")

	req := &model.ChatCompletionRequest{
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
		Stream: false,
	}

	httpReq, err := mp.TransformRequest(context.Background(), req)
	require.NoError(t, err)

	client := mp.Client()
	httpResp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer httpResp.Body.Close()

	chatResp, err := mp.TransformResponse(context.Background(), httpResp)
	require.NoError(t, err)

	require.Len(t, chatResp.Choices, 1)
	assert.Equal(t, "Custom canned response content", chatResp.Choices[0].Message.Content)
}

func TestMockProvider_CannedStream(t *testing.T) {
	mp := NewMockProvider("test-mock")
	mp.SetCannedStream([]string{"Hello", "World", "Stream"})

	req := &model.ChatCompletionRequest{
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	}

	httpReq, err := mp.TransformRequest(context.Background(), req)
	require.NoError(t, err)

	client := mp.Client()
	httpResp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	require.NoError(t, err)

	bodyStr := string(bodyBytes)
	assert.Contains(t, bodyStr, "Hello")
	assert.Contains(t, bodyStr, "World")
	assert.Contains(t, bodyStr, "Stream")
	assert.Contains(t, bodyStr, "[DONE]")
}

func TestMockProvider_ErrorSimulation(t *testing.T) {
	mp := NewMockProvider("test-mock")
	simulatedErr := errors.New("simulated network failure")
	mp.SetError(simulatedErr)

	req := &model.ChatCompletionRequest{
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	_, err := mp.TransformRequest(context.Background(), req)
	assert.ErrorIs(t, err, simulatedErr)

	_, err = mp.TransformResponse(context.Background(), &http.Response{})
	assert.ErrorIs(t, err, simulatedErr)

	err = mp.HealthCheck(context.Background())
	assert.ErrorIs(t, err, simulatedErr)

	_, err = mp.DiscoverModels(context.Background())
	assert.ErrorIs(t, err, simulatedErr)
}
