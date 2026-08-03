package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/model"
)

func TestTranslateLegacyCompletionRequest_Basic(t *testing.T) {
	body := []byte(`{"model": "gpt-3.5-turbo-instruct", "prompt": "Once upon a time", "max_tokens": 50, "stream": true}`)

	req, requestedModel, isStream, err := TranslateLegacyCompletionRequest(body)
	require.NoError(t, err)
	assert.Equal(t, "gpt-3.5-turbo-instruct", requestedModel)
	assert.True(t, isStream)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "Once upon a time", req.Messages[0].Content)
	require.NotNil(t, req.MaxTokens)
	assert.Equal(t, 50, *req.MaxTokens)
}

func TestTranslateChatResponseToLegacyCompletion(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-3.5-turbo-instruct",
		Choices: []model.Choice{
			{Message: model.ChoiceMessage{Content: "once upon a time..."}, FinishReason: "stop"},
		},
		Usage: &model.Usage{PromptTokens: 4, CompletionTokens: 6},
	}

	out := translateChatResponseToLegacyCompletion(resp, "gpt-3.5-turbo-instruct")
	assert.Equal(t, "text_completion", out.Object)
	require.Len(t, out.Choices, 1)
	assert.Equal(t, "once upon a time...", out.Choices[0].Text)
	assert.Equal(t, "stop", out.Choices[0].FinishReason)
}
