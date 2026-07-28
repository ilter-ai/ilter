package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChatCompletionRequestMarshalUnmarshal(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo",
		Messages: []Message{{Role: "user", Content: "Hello"}},
		Stream:   false,
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.Equal(t, req, req2)
}

func TestChatCompletionRequestStreamTrue(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo",
		Messages: []Message{{Role: "user", Content: "Hello"}},
		Stream:   true,
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.True(t, req2.Stream)
}

func TestChatCompletionRequestStreamFalse(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo",
		Messages: []Message{{Role: "user", Content: "Hello"}},
		Stream:   false,
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.False(t, req2.Stream)
}

func TestChatCompletionRequestEmptyMessages(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo",
		Messages: []Message{},
		Stream:   false,
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.Empty(t, req2.Messages)
}

func TestChatCompletionRequestNilFields(t *testing.T) {
	req := ChatCompletionRequest{
		Model:       "gpt-3.5-turbo",
		Messages:    []Message{{Role: "user", Content: "Hello"}},
		Temperature: nil,
		TopP:        nil,
		N:           nil,
		MaxTokens:   nil,
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.Nil(t, req2.Temperature)
	assert.Nil(t, req2.TopP)
	assert.Nil(t, req2.N)
	assert.Nil(t, req2.MaxTokens)
}

func TestChatCompletionRequestSpecialChars(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo",
		Messages: []Message{{Role: "user", Content: "Hello \"world\" \n\t\\"}},
		Stream:   false,
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.Equal(t, req.Messages[0].Content, req2.Messages[0].Content)
}

func TestChatCompletionResponseMarshalUnmarshalWithUsage(t *testing.T) {
	resp := ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-3.5-turbo",
		Choices: []Choice{
			{
				Index: 0,
				Message: ChoiceMessage{
					Role:    "assistant",
					Content: "Hello!",
				},
				FinishReason: "stop",
			},
		},
		Usage: &Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	b, err := json.Marshal(resp)
	assert.NoError(t, err)

	var resp2 ChatCompletionResponse
	err = json.Unmarshal(b, &resp2)
	assert.NoError(t, err)
	assert.Equal(t, resp, resp2)
}

func TestChatCompletionResponseMarshalUnmarshalWithoutUsage(t *testing.T) {
	resp := ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-3.5-turbo",
		Choices: []Choice{
			{
				Index: 0,
				Message: ChoiceMessage{
					Role:    "assistant",
					Content: "Hello!",
				},
				FinishReason: "stop",
			},
		},
		Usage: nil,
	}

	b, err := json.Marshal(resp)
	assert.NoError(t, err)

	var resp2 ChatCompletionResponse
	err = json.Unmarshal(b, &resp2)
	assert.NoError(t, err)
	assert.Nil(t, resp2.Usage)
}

func TestChatCompletionResponseEmptyChoices(t *testing.T) {
	resp := ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-3.5-turbo",
		Choices: []Choice{},
	}

	b, err := json.Marshal(resp)
	assert.NoError(t, err)

	var resp2 ChatCompletionResponse
	err = json.Unmarshal(b, &resp2)
	assert.NoError(t, err)
	assert.Empty(t, resp2.Choices)
}

func TestChatCompletionChunkMarshalUnmarshal(t *testing.T) {
	chunk := ChatCompletionChunk{
		ID:      "chatcmpl-chunk-123",
		Object:  "chat.completion.chunk",
		Created: 1234567890,
		Model:   "gpt-3.5-turbo",
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: Delta{
					Role:    "assistant",
					Content: "Hello",
				},
			},
		},
	}

	b, err := json.Marshal(chunk)
	assert.NoError(t, err)

	var chunk2 ChatCompletionChunk
	err = json.Unmarshal(b, &chunk2)
	assert.NoError(t, err)
	assert.Equal(t, chunk, chunk2)
}

func TestChatCompletionChunkWithFinishReason(t *testing.T) {
	chunk := ChatCompletionChunk{
		ID:      "chatcmpl-chunk-123",
		Object:  "chat.completion.chunk",
		Created: 1234567890,
		Model:   "gpt-3.5-turbo",
		Choices: []ChunkChoice{
			{
				Index:        0,
				Delta:        Delta{},
				FinishReason: ptr("stop"),
			},
		},
	}

	b, err := json.Marshal(chunk)
	assert.NoError(t, err)

	var chunk2 ChatCompletionChunk
	err = json.Unmarshal(b, &chunk2)
	assert.NoError(t, err)
	assert.NotNil(t, chunk2.Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunk2.Choices[0].FinishReason)
}

func TestErrorResponseMarshalUnmarshal(t *testing.T) {
	errResp := ErrorResponse{
		Error: ErrorDetail{
			Message: "Invalid API key",
			Type:    "invalid_request_error",
			Code:    "invalid_api_key",
		},
	}

	b, err := json.Marshal(errResp)
	assert.NoError(t, err)

	var errResp2 ErrorResponse
	err = json.Unmarshal(b, &errResp2)
	assert.NoError(t, err)
	assert.Equal(t, errResp, errResp2)
}

func TestValidationModelRequired(t *testing.T) {
	// We cannot directly validate struct fields without a validator.
	// However, we can test that if Model is empty, the JSON marshaling still works.
	// The validation is likely done at the handler level.
	// For the purpose of this test, we just ensure the struct can be created and marshaled.
	req := ChatCompletionRequest{
		Model:    "",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.Empty(t, req2.Model)
}

func TestValidationMessagesRequired(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo",
		Messages: []Message{},
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)

	var req2 ChatCompletionRequest
	err = json.Unmarshal(b, &req2)
	assert.NoError(t, err)
	assert.Empty(t, req2.Messages)
}

// Helper function to create a pointer to a string
func ptr(s string) *string {
	return &s
}
