package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
)

// ToolSentinelPrefix is the prefix injected into system messages
// when tools are emulated for non-tool-calling models.
const ToolSentinelPrefix = "You have access to the following tools."

// ToWire converts a ChatCompletionRequest to the wire format suitable for
// models that don't support native tool calling. When exhausted is true and
// the sentinel has already been injected, it replaces it with a "tools done"
// instruction.
func ToWire(req *model.ChatCompletionRequest, exhausted bool) *model.ChatCompletionRequest {
	cp := *req
	cp.Messages = make([]model.Message, len(req.Messages))
	copy(cp.Messages, req.Messages)

	alreadyInjected := HasToolSentinel(cp.Messages)

	switch {
	case !alreadyInjected && len(req.Tools) > 0:
		var sb strings.Builder
		sb.WriteString("You have access to the following tools.\n" +
			"CRITICAL RULES FOR TOOL CALLS:\n" +
			"1. NEVER invent, fake, or simulate tool outputs or JSON data in natural language text.\n" +
			"2. Whenever you need data from a tool, your response MUST contain <tool_calls>...</tool_calls>.\n" +
			"3. Do NOT write conversational text claiming you called a tool or showing fake results without emitting <tool_calls>.\n" +
			"4. Stop immediately after closing </tool_calls>. Wait for the real tool result before answering the user.\n\n" +
			"To call a tool, use this EXACT XML format:\n\n" +
			"<tool_calls>\n<invoke name=\"tool_name\">\n<parameter name=\"arg1\">value1</parameter>\n</invoke>\n</tool_calls>" +
			"\n\nAvailable tools:\n")
		for _, t := range req.Tools {
			fn := t.Function
			fmt.Fprintf(&sb, "- %s", fn.Name)
			if fn.Description != "" {
				fmt.Fprintf(&sb, ": %s", fn.Description)
			}
			sb.WriteString("\n")
			paramsJSON, _ := json.Marshal(fn.Parameters)
			fmt.Fprintf(&sb, "  params: %s\n", string(paramsJSON))
		}

		sysMsg := model.Message{
			Role:    "system",
			Content: sb.String(),
		}
		cp.Messages = append([]model.Message{sysMsg}, cp.Messages...)

	case alreadyInjected && exhausted:
		for i := range cp.Messages {
			if s, ok := cp.Messages[i].Content.(string); ok && strings.Contains(s, ToolSentinelPrefix) {
				cp.Messages[i].Content = "Tools have been executed. Answer the user directly using the tool results above. Do NOT emit any XML or tool calls."
				break
			}
		}

	case alreadyInjected && !exhausted && len(req.Tools) > 0:
		for i := range cp.Messages {
			if s, ok := cp.Messages[i].Content.(string); ok && strings.Contains(s, ToolSentinelPrefix) {
				cp.Messages = append(cp.Messages[:i], cp.Messages[i+1:]...)
				break
			}
		}
		var sb strings.Builder
		sb.WriteString("You have access to the following tools.\n" +
			"CRITICAL RULES FOR TOOL CALLS:\n" +
			"1. NEVER invent, fake, or simulate tool outputs or JSON data in natural language text.\n" +
			"2. Whenever you need data from a tool, your response MUST contain <tool_calls>...</tool_calls>.\n" +
			"3. Do NOT write conversational text claiming you called a tool or showing fake results without emitting <tool_calls>.\n" +
			"4. Stop immediately after closing </tool_calls>. Wait for the real tool result before answering the user.\n\n" +
			"To call a tool, use this EXACT XML format:\n\n" +
			"<tool_calls>\n<invoke name=\"tool_name\">\n<parameter name=\"arg1\">value1</parameter>\n</invoke>\n</tool_calls>" +
			"\n\nAvailable tools:\n")
		for _, t := range req.Tools {
			fn := t.Function
			fmt.Fprintf(&sb, "- %s", fn.Name)
			if fn.Description != "" {
				fmt.Fprintf(&sb, ": %s", fn.Description)
			}
			sb.WriteString("\n")
			paramsJSON, _ := json.Marshal(fn.Parameters)
			fmt.Fprintf(&sb, "  params: %s\n", string(paramsJSON))
		}

		sysMsg := model.Message{
			Role:    "system",
			Content: sb.String(),
		}
		cp.Messages = append([]model.Message{sysMsg}, cp.Messages...)

	default:
	}

	cp.Tools = nil
	cp.ToolChoice = nil

	for i := range cp.Messages {
		msg := &cp.Messages[i]

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			var tcSB strings.Builder
			tcSB.WriteString("<tool_calls>\n")
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(&tcSB, "<invoke name=\"%s\">\n", tc.Function.Name)
				var args map[string]any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
					for k, v := range args {
						valStr, _ := json.Marshal(v)
						fmt.Fprintf(&tcSB, "<parameter name=\"%s\">%s</parameter>\n", k, string(valStr))
					}
				}
				tcSB.WriteString("</invoke>\n")
			}
			tcSB.WriteString("</tool_calls>")

			if s, ok := msg.Content.(string); ok && strings.TrimSpace(s) != "" {
				msg.Content = s + "\n\n" + tcSB.String()
			} else {
				msg.Content = tcSB.String()
			}
			msg.ToolCalls = nil
		}

		if msg.Role == "tool" {
			contentStr := ""
			if s, ok := msg.Content.(string); ok {
				contentStr = s
			}
			// Escape XML tags to prevent prompt injection
			contentStr = strings.ReplaceAll(contentStr, "</tool_result>", "&lt;/tool_result&gt;")
			contentStr = strings.ReplaceAll(contentStr, "<tool_result", "&lt;tool_result")

			toolName := msg.Name
			if toolName == "" {
				toolName = msg.ToolCallID
			}
			msg.Role = "user"
			msg.Content = fmt.Sprintf("<tool_result tool=\"%s\">\n%s\n</tool_result>", toolName, contentStr)
			msg.ToolCallID = ""
			msg.Name = ""
		}
	}

	return &cp
}
