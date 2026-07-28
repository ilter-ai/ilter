package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/ilter-ai/ilter/internal/model"
)

// ToolCallExecutor runs LLM tool_calls against the MCP registry in parallel
// and formats the results as new chat messages ready to be sent back to the LLM.
type ToolCallExecutor struct {
	executor *Executor
}

func NewToolCallExecutor(executor *Executor) *ToolCallExecutor {
	return &ToolCallExecutor{
		executor: executor,
	}
}

// ToolResult holds a single tool call's response message and whether it failed.
type ToolResult struct {
	Msg     model.Message
	IsError bool
}

// ExecuteToolCalls runs every ToolCall in the slice concurrently and returns
// one assistant message (containing the original tool_calls) followed by one
// tool message per successful execution.
//
// The returned messages can be appended to the chat history before sending
// a follow-up LLM request. The second return value holds per-result error flags.
func (tce *ToolCallExecutor) ExecuteToolCalls(
	ctx context.Context,
	keyID string,
	keyPrefix string,
	toolCalls []model.ToolCall,
) ([]model.Message, []bool) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	assistantMsg := model.Message{
		Role:      "assistant",
		ToolCalls: toolCalls,
		Content:   "", // OpenAI requires empty string when tool_calls is set.
	}

	results := make([]ToolResult, len(toolCalls))
	g, gCtx := errgroup.WithContext(ctx)

	for i, tc := range toolCalls {
		i, tc := i, tc // capture range vars
		g.Go(func() error {
			var args json.RawMessage
			if tc.Function.Arguments != "" {
				args = json.RawMessage(tc.Function.Arguments)
			} else {
				args = json.RawMessage(`{}`)
			}

			callResult := tce.executor.ExecuteTool(gCtx, &ExecuteToolParams{
				ToolName:  tc.Function.Name,
				Arguments: args,
				APIKeyID:  keyID,
				KeyPrefix: keyPrefix,
			})

			if callResult == nil {
				return fmt.Errorf("tool %q returned nil result", tc.Function.Name)
			}

			// Build the tool response message.
			content := formatToolResult(callResult)

			results[i] = ToolResult{
				Msg: model.Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    content,
				},
				IsError: callResult.IsError,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		mcpLog.Error("tool call execution failed", "error", err)
		// Return a best-effort partial result.
	}

	toolMessages := make([]model.Message, 0, len(toolCalls)+1)
	toolMessages = append(toolMessages, assistantMsg)
	errors := make([]bool, 0, len(toolCalls))

	for _, r := range results {
		if r.Msg.Role != "" {
			toolMessages = append(toolMessages, r.Msg)
			errors = append(errors, r.IsError)
		}
	}

	return toolMessages, errors
}

// formatToolResult serializes a CallToolResult into a single string that can
// be used as the content of a "tool" role message.  If the tool returned
// multiple content items they are joined with newlines.
func formatToolResult(cr *CallToolResult) string {
	if cr == nil || len(cr.Content) == 0 {
		if cr != nil && cr.IsError {
			return "tool_error: no output"
		}
		return "ok"
	}

	var out string
	for _, c := range cr.Content {
		switch c.Type {
		case "text":
			if out != "" {
				out += "\n"
			}
			out += c.Text
		case "image":
			if out != "" {
				out += "\n"
			}
			out += fmt.Sprintf("[Image: %s]", c.MIMEType)
		case "resource":
			if out != "" {
				out += "\n"
			}
			out += fmt.Sprintf("[Resource: %s]", c.URI)
		default:
			if out != "" {
				out += "\n"
			}
			out += c.Text
		}
	}

	if out == "" {
		out = "ok"
	}

	return out
}
