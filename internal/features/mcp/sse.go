package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
)

// SSEChunk represents a single SSE data chunk from a streaming response.
type SSEChunk struct {
	Data []byte
	Done bool
}

// ParseSSEStream parses an SSE byte stream into chunks and detects tool calls.
func ParseSSEStream(buf *bytes.Buffer) (chunks []SSEChunk, toolCallFound bool, toolCalls []model.ToolCall) {
	scanner := bufio.NewScanner(buf)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var currentData bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			if data == "[DONE]" {
				chunks = append(chunks, SSEChunk{Data: []byte(line), Done: true})
				break
			}
			var cc model.ChatCompletionChunk
			if err := json.Unmarshal([]byte(data), &cc); err == nil {
				for _, c := range cc.Choices {
					if len(c.Delta.ToolCalls) > 0 {
						toolCallFound = true
					}
					if c.FinishReason != nil && *c.FinishReason == "tool_calls" {
						toolCallFound = true
					}
				}
				if toolCallFound {
					toolCalls = MergeStreamToolCalls(toolCalls, cc)
				}
			}
			currentData.Reset()
			currentData.WriteString(line)
		} else if strings.HasPrefix(line, "event:") || strings.HasPrefix(line, ":") {
			currentData.Reset()
			currentData.WriteString(line)
		} else if line == "" {
			if currentData.Len() > 0 {
				chunks = append(chunks, SSEChunk{Data: bytes.Clone(currentData.Bytes())})
				currentData.Reset()
			}
			continue
		} else {
			currentData.WriteString("\n")
			currentData.WriteString(line)
		}
		if strings.TrimSpace(line) == "" && currentData.Len() > 0 {
			chunks = append(chunks, SSEChunk{Data: bytes.Clone(currentData.Bytes())})
			currentData.Reset()
		}
	}
	if currentData.Len() > 0 {
		chunks = append(chunks, SSEChunk{Data: bytes.Clone(currentData.Bytes())})
	}
	return
}

// MergeStreamToolCalls merges tool call deltas from streaming chunks.
func MergeStreamToolCalls(existing []model.ToolCall, chunk model.ChatCompletionChunk) []model.ToolCall {
	for _, c := range chunk.Choices {
		for _, tc := range c.Delta.ToolCalls {
			idx := tc.Index
			if idx < len(existing) {
				existingArgs := existing[idx].Function.Arguments
				newArgs := tc.Function.Arguments
				switch {
				case existingArgs == "" || existingArgs == "{}":
					existing[idx].Function.Arguments = newArgs
				case strings.HasPrefix(newArgs, existingArgs):
					existing[idx].Function.Arguments = newArgs
				default:
					existing[idx].Function.Arguments += newArgs
				}

				if tc.ID != "" {
					existing[idx].ID = tc.ID
				}
				if tc.Function.Name != "" {
					existing[idx].Function.Name = tc.Function.Name
				}
				if tc.Type != "" {
					existing[idx].Type = tc.Type
				}
				if existing[idx].Type == "" {
					existing[idx].Type = "function"
				}
			} else {
				for len(existing) <= idx {
					existing = append(existing, model.ToolCall{})
				}
				tcType := tc.Type
				if tcType == "" {
					tcType = "function"
				}
				existing[idx] = model.ToolCall{
					ID:   tc.ID,
					Type: tcType,
					Function: model.ToolCallFunctionData{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
	}
	return existing
}

// ReassembleStreamContent concatenates all content deltas from SSE chunks.
func ReassembleStreamContent(chunks []SSEChunk) (string, string) {
	var sb, reasoning strings.Builder
	for _, c := range chunks {
		if c.Done {
			break
		}
		body, ok := strings.CutPrefix(string(c.Data), "data: ")
		if !ok {
			continue
		}
		if body == "[DONE]" {
			break
		}
		var cc model.ChatCompletionChunk
		if err := json.Unmarshal([]byte(body), &cc); err != nil {
			continue
		}
		for _, ch := range cc.Choices {
			sb.WriteString(ch.Delta.Content)
			reasoning.WriteString(ch.Delta.ReasoningContent)
		}
	}
	return sb.String(), reasoning.String()
}
