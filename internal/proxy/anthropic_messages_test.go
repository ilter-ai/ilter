package proxy

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/model"
)

func TestTranslateAnthropicMessagesRequest_Basic(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4",
		"system": "be terse",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "hello"}
		],
		"tools": [
			{"name": "get_weather", "description": "get weather", "input_schema": {"type": "object"}}
		]
	}`)

	req, requestedModel, isStream, err := TranslateAnthropicMessagesRequest(body)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4", requestedModel)
	assert.False(t, isStream)
	require.NotNil(t, req.MaxTokens)
	assert.Equal(t, 1024, *req.MaxTokens)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, "system", req.Messages[0].Role)
	assert.Equal(t, "be terse", req.Messages[0].Content)
	assert.Equal(t, "user", req.Messages[1].Role)
	assert.Equal(t, "hello", req.Messages[1].Content)
	require.Len(t, req.Tools, 1)
	assert.Equal(t, "get_weather", req.Tools[0].Function.Name)
}

func TestTranslateAnthropicMessagesRequest_ToolUseRoundTrip(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": "what's the weather"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "Paris"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "22C sunny"}
			]}
		]
	}`)

	req, _, _, err := TranslateAnthropicMessagesRequest(body)
	require.NoError(t, err)
	require.Len(t, req.Messages, 3)

	assistantMsg := req.Messages[1]
	require.Len(t, assistantMsg.ToolCalls, 1)
	assert.Equal(t, "toolu_1", assistantMsg.ToolCalls[0].ID)
	assert.Equal(t, "get_weather", assistantMsg.ToolCalls[0].Function.Name)
	assert.JSONEq(t, `{"city":"Paris"}`, assistantMsg.ToolCalls[0].Function.Arguments)

	toolResultMsg := req.Messages[2]
	assert.Equal(t, "tool", toolResultMsg.Role)
	assert.Equal(t, "toolu_1", toolResultMsg.ToolCallID)
	assert.Equal(t, "22C sunny", toolResultMsg.Content)
}

func TestTranslateAnthropicMessagesRequest_ImagePreserved(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what is this?"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "aGVsbG8="}}
			]}
		]
	}`)

	req, _, _, err := TranslateAnthropicMessagesRequest(body)
	require.NoError(t, err)
	require.Len(t, req.Messages, 1)

	content, ok := req.Messages[0].Content.([]any)
	require.True(t, ok, "mixed text+image content must be preserved as blocks, not collapsed to a string")
	require.Len(t, content, 2)

	imgBlock, ok := content[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "image", imgBlock["type"])
}

func TestTranslateChatResponseToAnthropic_Text(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4o",
		Choices: []model.Choice{
			{
				Message:      model.ChoiceMessage{Role: "assistant", Content: "hi there"},
				FinishReason: "stop",
			},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3},
	}

	out := translateChatResponseToAnthropic(resp, "claude-opus-4")
	assert.Equal(t, "message", out["type"])
	assert.Equal(t, "end_turn", out["stop_reason"])
	assert.Equal(t, "gpt-4o", out["model"])

	content, ok := out["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])
	assert.Equal(t, "hi there", content[0]["text"])

	usage, ok := out["usage"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 5, usage["input_tokens"])
	assert.Equal(t, 3, usage["output_tokens"])
}

func TestTranslateChatResponseToAnthropic_ToolCall(t *testing.T) {
	resp := &model.ChatCompletionResponse{
		Choices: []model.Choice{
			{
				Message: model.ChoiceMessage{
					Role: "assistant",
					ToolCalls: []model.ToolCall{
						{ID: "call_1", Type: "function", Function: model.ToolCallFunctionData{Name: "get_weather", Arguments: `{"city":"Paris"}`}},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	out := translateChatResponseToAnthropic(resp, "claude-opus-4")
	assert.Equal(t, "tool_use", out["stop_reason"])
	content := out["content"].([]map[string]any)
	require.Len(t, content, 1)
	assert.Equal(t, "tool_use", content[0]["type"])
	assert.Equal(t, "call_1", content[0]["id"])
	assert.Equal(t, "get_weather", content[0]["name"])
}

func TestAnthropicStreamTranslator_TextAndFinish(t *testing.T) {
	tr := newAnthropicStreamTranslator("claude-opus-4")
	var buf bytes.Buffer

	tr.feed(&buf, &model.ChatCompletionChunk{
		ID:    "chatcmpl-1",
		Model: "gpt-4o",
		Choices: []model.ChunkChoice{
			{Delta: model.Delta{Role: "assistant"}},
		},
	})
	tr.feed(&buf, &model.ChatCompletionChunk{
		Choices: []model.ChunkChoice{
			{Delta: model.Delta{Content: "hello"}},
		},
	})
	finishReason := "stop"
	tr.feed(&buf, &model.ChatCompletionChunk{
		Choices: []model.ChunkChoice{
			{FinishReason: &finishReason},
		},
		Usage: &model.Usage{PromptTokens: 10, CompletionTokens: 2},
	})
	tr.finish(&buf)

	out := buf.String()
	assert.Contains(t, out, "event: message_start")
	assert.Contains(t, out, "event: content_block_start")
	assert.Contains(t, out, `"type":"text_delta"`)
	assert.Contains(t, out, "event: content_block_stop")
	assert.Contains(t, out, `"stop_reason":"end_turn"`)
	assert.Contains(t, out, "event: message_stop")

	// content_block_start must precede its delta, which must precede its stop.
	startIdx := strings.Index(out, "event: content_block_start")
	deltaIdx := strings.Index(out, "event: content_block_delta")
	stopIdx := strings.Index(out, "event: content_block_stop")
	assert.True(t, startIdx < deltaIdx && deltaIdx < stopIdx)
}

func TestAnthropicStreamTranslator_ToolCall(t *testing.T) {
	tr := newAnthropicStreamTranslator("claude-opus-4")
	var buf bytes.Buffer

	tr.feed(&buf, &model.ChatCompletionChunk{
		ID: "chatcmpl-1",
		Choices: []model.ChunkChoice{
			{Delta: model.Delta{ToolCalls: []model.ChunkToolCall{
				{Index: 0, ID: "call_1", Type: "function", Function: model.ChunkToolCallFunction{Name: "get_weather"}},
			}}},
		},
	})
	tr.feed(&buf, &model.ChatCompletionChunk{
		Choices: []model.ChunkChoice{
			{Delta: model.Delta{ToolCalls: []model.ChunkToolCall{
				{Index: 0, Function: model.ChunkToolCallFunction{Arguments: `{"city":`}},
			}}},
		},
	})
	tr.feed(&buf, &model.ChatCompletionChunk{
		Choices: []model.ChunkChoice{
			{Delta: model.Delta{ToolCalls: []model.ChunkToolCall{
				{Index: 0, Function: model.ChunkToolCallFunction{Arguments: `"Paris"}`}},
			}}},
		},
	})
	finishReason := "tool_calls"
	tr.feed(&buf, &model.ChatCompletionChunk{
		Choices: []model.ChunkChoice{{FinishReason: &finishReason}},
	})
	tr.finish(&buf)

	out := buf.String()
	assert.Contains(t, out, `"type":"tool_use"`)
	assert.Contains(t, out, `"id":"call_1"`)
	assert.Contains(t, out, `"type":"input_json_delta"`)
	assert.Contains(t, out, `"stop_reason":"tool_use"`)
}

func TestAnthropicResponseWriter_NonStreamingErrorTranslation(t *testing.T) {
	rec := httptest.NewRecorder()
	w := newTranslatingResponseWriter(rec, false, newAnthropicTranslator("claude-opus-4"))

	w.WriteHeader(429)
	_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"insufficient_quota"}}`))
	w.Finish()

	assert.Equal(t, 429, rec.Code)
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "error", out["type"])
	errObj := out["error"].(map[string]any)
	assert.Equal(t, "rate limited", errObj["message"])
}
