package middleware

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/model"
)

func TestFindAllToolCallsInText_bareInvoke(t *testing.T) {
	// Bare <invoke> without <tool_calls> wrapper -- model emits direct XML
	text := `Let me check the database. <invoke name="sqlite__query"><parameter name="sql">SELECT 1</parameter></invoke>`
	tcs, xmlOffset := mcp.FindAllToolCallsInText(text)
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}
	if tcs[0].Function.Name != "sqlite__query" {
		t.Errorf("expected sqlite__query, got %s", tcs[0].Function.Name)
	}
	if xmlOffset < 0 {
		t.Errorf("expected xmlOffset >= 0 for bare invoke")
	}
	// Pre-tool text should be preserved
	cleanedText := strings.TrimSpace(text[:xmlOffset])
	if cleanedText != "Let me check the database." {
		t.Errorf("expected 'Let me check the database.', got %q", cleanedText)
	}
}

func TestFindAllToolCallsInText_truncatedToolCalls(t *testing.T) {
	// <tool_calls> with unparseable content (cut off mid-tag) -- xmlOffset set but len(tcs)==0
	text := `I'll look that up. <tool_calls><invoke name="`
	tcs, xmlOffset := mcp.FindAllToolCallsInText(text)
	if len(tcs) != 0 {
		t.Errorf("expected 0 tool calls (truncated at name quote), got %d", len(tcs))
	}
	if xmlOffset < 0 {
		t.Errorf("expected xmlOffset >= 0 for truncated <tool_calls>")
	}
}

func TestFindAllToolCallsInText_truncatedInvokeWithClose(t *testing.T) {
	// <tool_calls> without </tool_calls> but invoke is complete -- fallback parses it
	text := `I'll check. <tool_calls><invoke name="sqlite__query"><parameter name="sql">SELECT 1</parameter></invoke>`
	tcs, xmlOffset := mcp.FindAllToolCallsInText(text)
	if len(tcs) != 1 {
		t.Errorf("expected 1 tool call (invoke complete despite truncated wrapper), got %d", len(tcs))
	}
	if xmlOffset < 0 {
		t.Errorf("expected xmlOffset >= 0")
	}
	preText := strings.TrimSpace(text[:xmlOffset])
	if preText != "I'll check." {
		t.Errorf("expected 'I'll check.', got %q", preText)
	}
}

func TestFindAllToolCallsInText_multipleInvokes(t *testing.T) {
	text := `<tool_calls>
<invoke name="sqlite__query"><parameter name="sql">SELECT 1</parameter></invoke>
<invoke name="sqlite__query"><parameter name="sql">SELECT 2</parameter></invoke>
</tool_calls>`
	tcs, xmlOffset := mcp.FindAllToolCallsInText(text)
	if len(tcs) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(tcs))
	}
	if tcs[0].Function.Name != "sqlite__query" || tcs[1].Function.Name != "sqlite__query" {
		t.Error("expected both calls to be sqlite__query")
	}
	if xmlOffset < 0 {
		t.Error("expected xmlOffset >= 0")
	}
}

func TestFindAllToolCallsInText_noToolCalls(t *testing.T) {
	text := "Hello, how can I help you today?"
	tcs, xmlOffset := mcp.FindAllToolCallsInText(text)
	if len(tcs) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(tcs))
	}
	if xmlOffset != -1 {
		t.Errorf("expected xmlOffset -1, got %d", xmlOffset)
	}
}

func TestStripToolCallXML_fullBlock(t *testing.T) {
	input := "Before. <tool_calls><invoke name=\"test\"></invoke></tool_calls> After."
	expected := "Before. \ue000ilter:tool:0\ue001 After."
	result, _ := mcp.StripToolCallXML(input, 0)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestStripToolCallXML_truncatedOpenTag(t *testing.T) {
	// No </tool_calls> -- cut everything from <tool_calls> onwards
	input := "Before. <tool_calls><invoke name=\"test\">"
	expected := "Before."
	result, _ := mcp.StripToolCallXML(input, 0)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestStripToolCallXML_bareInvoke(t *testing.T) {
	input := "Let me check. <invoke name=\"sqlite__query\"><parameter name=\"sql\">SELECT 1</parameter></invoke> Done."
	expected := "Let me check. \ue000ilter:tool:0\ue001 Done."
	result, _ := mcp.StripToolCallXML(input, 0)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestStripToolCallXML_empty(t *testing.T) {
	if s, _ := mcp.StripToolCallXML("", 0); s != "" {
		t.Error("expected empty")
	}
}

func TestStripToolCallXML_falsePrefixPreserved(t *testing.T) {
	input := "a <tool_callsX b <invoker> c"
	expected := "a <tool_callsX b <invoker> c"
	result, _ := mcp.StripToolCallXML(input, 0)
	if result != expected {
		t.Errorf("false prefix: expected %q, got %q", expected, result)
	}
}

func TestStripToolCallXML_multiInvoke(t *testing.T) {
	input := "Before. <tool_calls><invoke name=\"a\"></invoke><invoke name=\"b\"></invoke></tool_calls> After."
	expected := "Before. \ue000ilter:tool:0\ue001\ue000ilter:tool:1\ue001 After."
	result, _ := mcp.StripToolCallXML(input, 0)
	if result != expected {
		t.Errorf("multi-invoke: expected %q, got %q", expected, result)
	}
}

// Regression: streaming handler previously reset markerIdx per turn.
func TestStripToolCallXML_markerOffset(t *testing.T) {
	input := "Before. <tool_calls><invoke name=\"a\"></invoke></tool_calls> After."
	expected := "Before. \ue000ilter:tool:5\ue001 After."
	result, _ := mcp.StripToolCallXML(input, 5)
	if result != expected {
		t.Errorf("offset: expected %q, got %q", expected, result)
	}
}

func TestBaseChunkFromChunks_extractsFields(t *testing.T) {
	data1, _ := json.Marshal(model.ChatCompletionChunk{
		ID:      "chatcmpl-abc123",
		Object:  "chat.completion.chunk",
		Created: 1700000000,
		Model:   "deepseek-v4-flash",
	})
	data2, _ := json.Marshal(model.ChatCompletionChunk{
		ID:      "chatcmpl-def456",
		Object:  "chat.completion.chunk",
		Created: 1700000001,
		Model:   "deepseek-v4-flash",
	})

	chunks := []mcp.SSEChunk{
		{Data: []byte("data: " + string(data1))},
		{Data: []byte("data: " + string(data2))},
	}

	chunk := baseChunkFromChunks(chunks)
	if chunk.ID != "chatcmpl-abc123" {
		t.Errorf("expected chatcmpl-abc123, got %s", chunk.ID)
	}
	if chunk.Created != 1700000000 {
		t.Errorf("expected 1700000000, got %d", chunk.Created)
	}
	if chunk.Model != "deepseek-v4-flash" {
		t.Errorf("expected deepseek-v4-flash, got %s", chunk.Model)
	}
}

func TestBaseChunkFromChunks_empty(t *testing.T) {
	chunk := baseChunkFromChunks(nil)
	if chunk.Object != "chat.completion.chunk" {
		t.Errorf("expected chat.completion.chunk, got %s", chunk.Object)
	}
}

func TestReassembleStreamContent_skipsToolCallDeltas(t *testing.T) {
	// Native-style SSE: content deltas then tool_call deltas
	makeData := func(content string) []byte {
		d, _ := json.Marshal(model.ChatCompletionChunk{
			Choices: []model.ChunkChoice{{Delta: model.Delta{Content: content}}},
		})
		return []byte("data: " + string(d))
	}

	chunks := []mcp.SSEChunk{
		{Data: makeData("Let me "), Done: false},
		{Data: makeData("check the "), Done: false},
		{Data: makeData("database."), Done: false},
		{Data: []byte("data: [DONE]"), Done: true},
	}

	content, reasoning := mcp.ReassembleStreamContent(chunks)
	if content != "Let me check the database." {
		t.Errorf("expected 'Let me check the database.', got %q", content)
	}
	if reasoning != "" {
		t.Errorf("expected empty reasoning, got %q", reasoning)
	}
}

func BenchmarkFindAllToolCallsInText(b *testing.B) {
	text := `<tool_calls>
<invoke name="sqlite__query"><parameter name="sql">SELECT * FROM users LIMIT 10</parameter></invoke>
<invoke name="sqlite__query"><parameter name="sql">SELECT * FROM orders LIMIT 5</parameter></invoke>
</tool_calls>`
	b.ResetTimer()
	for b.Loop() {
		mcp.FindAllToolCallsInText(text)
	}
}

func TestFilterNewToolCalls_duplicates(t *testing.T) {
	messages := []model.Message{
		{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: model.ToolCallFunctionData{
						Name:      "sqlite__query",
						Arguments: `{"sql":"SELECT 1"}`,
					},
				},
			},
		},
	}

	existingCall := model.ToolCall{
		Function: model.ToolCallFunctionData{
			Name:      "sqlite__query",
			Arguments: `{"sql":"SELECT 1"}`,
		},
	}
	newCall := model.ToolCall{
		Function: model.ToolCallFunctionData{
			Name:      "sqlite__query",
			Arguments: `{"sql":"SELECT 2"}`,
		},
	}

	result := mcp.FilterNewToolCalls(messages, []model.ToolCall{existingCall, newCall})
	if len(result) != 1 {
		t.Fatalf("expected 1 new tool call, got %d", len(result))
	}
	if result[0].Function.Arguments != `{"sql":"SELECT 2"}` {
		t.Error("expected the NEW call, not the duplicate")
	}
}

func TestFilterNewToolCalls_allNew(t *testing.T) {
	messages := []model.Message{
		{Role: "assistant"},
	}
	tc := model.ToolCall{
		Function: model.ToolCallFunctionData{
			Name:      "test",
			Arguments: `{}`,
		},
	}
	result := mcp.FilterNewToolCalls(messages, []model.ToolCall{tc})
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func TestFilters(t *testing.T) {
	// Verify ParseXMLArgValue covers the basic types
	tests := []struct {
		input string
		want  any
	}{
		{"true", true},
		{"false", false},
		{"42", int64(42)},
		{"3.14", float64(3.14)},
		{"hello", "hello"},
	}
	for _, tt := range tests {
		got := mcp.ParseXMLArgValue(tt.input)
		if got != tt.want {
			t.Errorf("mcp.ParseXMLArgValue(%q) = %v (%T), want %v (%T)", tt.input, got, got, tt.want, tt.want)
		}
	}
}

func TestParseXMLEmptyBuffer(t *testing.T) {
	rec := &bufferedResponseWriter{
		buf: &bytes.Buffer{},
	}
	chunks, found, tcs := mcp.ParseSSEStream(rec.buf)
	if len(chunks) != 0 || found || len(tcs) != 0 {
		t.Error("expected empty parse for empty buffer")
	}
}
