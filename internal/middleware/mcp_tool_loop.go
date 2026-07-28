package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/features/guardrails"
	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// toolCallLoop orchestrates the multi-turn tool execution loop.
func (m *MCPInjectMiddleware) toolCallLoop(w http.ResponseWriter, r *http.Request, req *model.ChatCompletionRequest, next http.Handler) {
	maxTurns := 50
	originalStream := req.Stream
	accumulatedMessages := append([]model.Message(nil), req.Messages...)

	totalToolCount := 0
	markerOffset := 0
	for turn := 0; turn < maxTurns; turn++ {
		turnStart := time.Now()
		curReq := *req
		if turn > 0 && curReq.ToolChoice != nil && curReq.ToolChoice != "auto" {
			curReq.ToolChoice = "auto"
		}

		if turn == maxTurns-1 {
			curReq.ToolChoice = "none"
			curReq.Tools = nil
		}

		for i := range accumulatedMessages {
			msg := &accumulatedMessages[i]
			if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
				if s, ok := msg.Content.(string); ok {
					if strings.TrimSpace(s) == "" {
						msg.Content = nil
					}
				} else if msg.Content == nil {
					msg.Content = nil
				}
				for j := range msg.ToolCalls {
					if msg.ToolCalls[j].Type == "" {
						msg.ToolCalls[j].Type = "function"
					}
					if strings.TrimSpace(msg.ToolCalls[j].Function.Arguments) == "" {
						msg.ToolCalls[j].Function.Arguments = "{}"
					}
				}
			}
			if msg.Role == "tool" {
				msg.Name = ""
			}
		}

		curReq.Messages = accumulatedMessages

		useEmulation := m.supportsToolsFn != nil && !m.supportsToolsFn(curReq.Model)
		var wireReq *model.ChatCompletionRequest
		if useEmulation {
			wireReq = mcp.ToWire(&curReq, turn == maxTurns-1)
		} else {
			wireReq = &curReq
		}

		wireBody, err := json.Marshal(wireReq)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		turnReq := r.Clone(r.Context())
		turnReq.Body = io.NopCloser(bytes.NewBuffer(wireBody))
		turnReq.ContentLength = int64(len(wireBody))

		var hasMore bool
		var toolMsgs []model.Message
		var toolErrors []bool

		if originalStream {
			hasMore, toolMsgs, toolErrors, markerOffset = m.handleStreamingOnce(w, turnReq, wireReq, next, markerOffset)
		} else {
			hasMore, toolMsgs, toolErrors, markerOffset = m.handleNonStreamingOnce(w, turnReq, wireReq, next, originalStream, markerOffset)
		}

		if !hasMore || len(toolMsgs) == 0 {
			mcpLog.Info("[PROXY] Tool loop finished - LLM completed response", "turn", turn+1)
			return
		}

		for _, tm := range toolMsgs {
			if tm.Role == "assistant" {
				accumulatedMessages = append(accumulatedMessages, tm)
				break
			}
		}

		toolCallInfo := make(map[string]struct{ name, args string }, len(toolMsgs))
		for _, msg := range accumulatedMessages {
			if msg.Role == "assistant" {
				for _, tc := range msg.ToolCalls {
					toolCallInfo[tc.ID] = struct{ name, args string }{tc.Function.Name, tc.Function.Arguments}
				}
			}
		}

		toolIdx := 0
		for _, tm := range toolMsgs {
			if tm.Role != "tool" {
				continue
			}
			cleanedContent := tm.Content

			if s, ok := tm.Content.(string); ok && s != "" {
				if m.piiMasker != nil && m.piiMasker.Masker() != nil {
					matches := m.piiMasker.Masker().DetectPII(s)
					masked, piiErr := m.piiMasker.Masker().ProcessText(s, nil)
					if piiErr == nil {
						s = masked
					}
					if len(matches) > 0 {
						keyID := reqmeta.GetKeyID(r.Context())
						clientIP := r.RemoteAddr
						for _, match := range matches {
							m.piiMasker.LogPIIEvent(r.Context(), piiActionToAuditLabel(match.Action), keyID, clientIP, match, s)
						}
					}
				}
				if m.guardrailsChecker != nil {
					res := m.guardrailsChecker.Check(r.Context(), []guardrails.Message{{Role: "user", Content: s}})
					if res.Blocked {
						mcpLog.Warn("guardrail blocked tool result", "tool_id", tm.ToolCallID, "rule_id", res.RuleID)
						s = "[Tool result blocked by security guardrails]"
					}
				}
				cleanedContent = s
			}

			toolMsg := tm
			toolMsg.Content = cleanedContent
			accumulatedMessages = append(accumulatedMessages, toolMsg)

			isErr := false
			if toolIdx < len(toolErrors) {
				isErr = toolErrors[toolIdx]
			}

			toolName := "unknown"
			toolArgs := ""
			if info, ok := toolCallInfo[tm.ToolCallID]; ok {
				toolName = info.name
				toolArgs = info.args
			}
			resultSize := 0
			if s, ok := cleanedContent.(string); ok {
				resultSize = len(s)
			}
			mcpLog.Info(
				"[PROXY] Tool executed",
				"tool", toolName,
				"turn", turn+1,
				"call_id", tm.ToolCallID,
				"is_error", isErr,
				"args", toolArgs,
				"result_bytes", resultSize,
				"duration_ms", time.Since(turnStart).Milliseconds(),
			)

			if m.toolEventWriter != nil {
				evtPayload, _ := json.Marshal(map[string]any{
					"call_id":  tm.ToolCallID,
					"content":  cleanedContent,
					"is_error": isErr,
				})
				m.toolEventWriter(w, "ilter.tool_result", evtPayload)
			}
			toolIdx++
			totalToolCount++
		}
	}

	mcpLog.Warn("[PROXY] Reached max tool execution turns", "max_turns", maxTurns)
	if originalStream {
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}
