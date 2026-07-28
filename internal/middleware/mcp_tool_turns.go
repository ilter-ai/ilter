package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func (m *MCPInjectMiddleware) handleNonStreamingOnce(
	w http.ResponseWriter,
	r *http.Request,
	req *model.ChatCompletionRequest,
	next http.Handler,
	originalStream bool,
	markerOffset int,
) (bool, []model.Message, []bool, int) {
	markerIdx := markerOffset

	rec := &bufferedResponseWriter{
		header: make(http.Header),
		buf:    &bytes.Buffer{},
	}

	next.ServeHTTP(rec, r)

	if rec.code != http.StatusOK {
		copyHeaders(w.Header(), rec.header)
		w.WriteHeader(rec.code)
		_, _ = w.Write(rec.buf.Bytes())
		return false, nil, nil, markerIdx
	}

	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(rec.buf.Bytes(), &chatResp); err != nil {
		copyHeaders(w.Header(), rec.header)
		w.WriteHeader(rec.code)
		_, _ = w.Write(rec.buf.Bytes())
		return false, nil, nil, markerIdx
	}

	toolCalls := mcp.ExtractToolCalls(&chatResp)

	if len(toolCalls) == 0 {
		if m.supportsToolsFn != nil && !m.supportsToolsFn(req.Model) {
			if mcp.NormalizeTextToolCalls(&chatResp) {
				toolCalls = mcp.ExtractToolCalls(&chatResp)
			}
		}
	}

	if len(toolCalls) == 0 {
		if originalStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			if len(chatResp.Choices) > 0 {
				w.WriteHeader(rec.code)

				var content string
				content, markerIdx = mcp.StripToolCallXML(chatResp.Choices[0].Message.Content, markerIdx)
				var finishReason *string
				if chatResp.Choices[0].FinishReason != "" {
					fr := chatResp.Choices[0].FinishReason
					finishReason = &fr
				}
				chunk := model.ChatCompletionChunk{
					ID:      chatResp.ID,
					Object:  "chat.completion.chunk",
					Created: chatResp.Created,
					Model:   chatResp.Model,
					Choices: []model.ChunkChoice{
						{
							Index: 0,
							Delta: model.Delta{
								Content:          content,
								ReasoningContent: chatResp.Choices[0].Message.ReasoningContent,
							},
							FinishReason: finishReason,
						},
					},
				}
				chunkBytes, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return false, nil, nil, markerIdx
		}

		cleanResp := chatResp
		for i := range cleanResp.Choices {
			var content string
			content, markerIdx = mcp.StripToolCallXML(cleanResp.Choices[i].Message.Content, markerIdx)
			cleanResp.Choices[i].Message.Content = content
		}
		cleanBytes, _ := json.Marshal(cleanResp)
		copyHeaders(w.Header(), rec.header)
		w.WriteHeader(rec.code)
		_, _ = w.Write(cleanBytes)
		return false, nil, nil, markerIdx
	}

	keyID := reqmeta.GetKeyID(r.Context())

	toolCalls = mcp.FilterNewToolCalls(req.Messages, toolCalls)

	mcpLog.Info("[PROXY] LLM model returned tool calls", "count", len(toolCalls), "tool_calls", toolCalls)

	cleanedAssistantText := ""
	if len(chatResp.Choices) > 0 {
		cleanedAssistantText, markerIdx = mcp.StripToolCallXML(chatResp.Choices[0].Message.Content, markerIdx)
	}

	if m.toolEventWriter != nil && len(toolCalls) > 0 {
		eventData, _ := json.Marshal(toolCalls)
		m.toolEventWriter(w, "ilter.tool_calls", eventData)
	}

	for i := range toolCalls {
		if toolCalls[i].Type == "" {
			toolCalls[i].Type = "function"
		}
	}

	assistantMsg := model.Message{
		Role:      "assistant",
		Content:   cleanedAssistantText,
		ToolCalls: toolCalls,
	}

	if len(toolCalls) == 0 {
		mcpLog.Warn("all tool calls are duplicates, writing original response")
		if originalStream {
			content := ""
			if len(chatResp.Choices) > 0 {
				content, markerIdx = mcp.StripToolCallXML(chatResp.Choices[0].Message.Content, markerIdx)
			}
			chunk := model.ChatCompletionChunk{
				ID: chatResp.ID, Object: "chat.completion.chunk", Created: chatResp.Created, Model: chatResp.Model,
				Choices: []model.ChunkChoice{
					{Index: 0, Delta: model.Delta{Content: content}, FinishReason: strPtr("stop")},
				},
			}
			chunkBytes, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return false, nil, nil, markerIdx
		}
		cleanResp := chatResp
		for i := range cleanResp.Choices {
			var content string
			content, markerIdx = mcp.StripToolCallXML(cleanResp.Choices[i].Message.Content, markerIdx)
			cleanResp.Choices[i].Message.ToolCalls = nil
			cleanResp.Choices[i].FinishReason = "stop"
			cleanResp.Choices[i].Message.Content = content
		}
		cleanBytes, _ := json.Marshal(cleanResp)
		copyHeaders(w.Header(), rec.header)
		w.WriteHeader(rec.code)
		_, _ = w.Write(cleanBytes)
		return false, nil, nil, markerIdx
	}

	toolMsgs, toolErrors := m.executeFn(r.Context(), keyID, "", toolCalls)
	if len(toolMsgs) == 0 {
		mcpLog.Warn("tool execution produced no messages")
		errResp := chatResp
		for i := range errResp.Choices {
			var content string
			content, markerIdx = mcp.StripToolCallXML(errResp.Choices[i].Message.Content, markerIdx)
			if content == "" {
				content = fmt.Sprintf("Tool %s could not be executed. The MCP server may be offline or not responding.", toolCalls[0].Function.Name)
			}
			errResp.Choices[i].Message.Content = content
			errResp.Choices[i].Message.ToolCalls = nil
			errResp.Choices[i].FinishReason = "stop"
		}
		cleanBytes, _ := json.Marshal(errResp)
		copyHeaders(w.Header(), rec.header)
		w.WriteHeader(rec.code)
		_, _ = w.Write(cleanBytes)
		return false, nil, nil, markerIdx
	}

	allMsgs := []model.Message{assistantMsg}
	if len(toolMsgs) > 0 {
		allMsgs = append(allMsgs, toolMsgs...)
	}
	return true, allMsgs, toolErrors, markerIdx
}

func (m *MCPInjectMiddleware) handleStreamingOnce(
	w http.ResponseWriter,
	r *http.Request,
	req *model.ChatCompletionRequest,
	next http.Handler,
	toolOffset int,
) (bool, []model.Message, []bool, int) {
	flusher, _ := w.(http.Flusher)
	rec := &reasoningTeeWriter{
		w:       w,
		flusher: flusher,
		header:  make(http.Header),
		buf:     &bytes.Buffer{},
	}

	next.ServeHTTP(rec, r)

	if rec.code != http.StatusOK {
		copyHeaders(w.Header(), rec.header)
		w.WriteHeader(rec.code)
		_, _ = w.Write(rec.buf.Bytes())
		return false, nil, nil, toolOffset
	}

	chunks, toolCallFound, reconstructedToolCalls := mcp.ParseSSEStream(rec.buf)
	fullText, reasoningText := mcp.ReassembleStreamContent(chunks)
	allTCs, _ := mcp.FindAllToolCallsInText(fullText)

	if m.supportsToolsFn != nil && !m.supportsToolsFn(req.Model) && (!toolCallFound || len(reconstructedToolCalls) == 0) && len(allTCs) > 0 {
		mcpLog.Debug("found text tool calls in stream", "count", len(allTCs))
		for i := range allTCs {
			tc := &allTCs[i]
			if tc.ID == "" {
				tc.ID = fmt.Sprintf("call_stream_%d", i)
			}
			if tc.Type == "" {
				tc.Type = "function"
			}
			reconstructedToolCalls = append(reconstructedToolCalls, *tc)
		}
		toolCallFound = true
	}

	cleanedText, markerIdx := mcp.StripToolCallXML(fullText, toolOffset)

	if !toolCallFound || len(reconstructedToolCalls) == 0 {
		copyHeaders(w.Header(), rec.header)
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(rec.code)
		if cleanedText != strings.TrimSpace(fullText) {
			chunk := baseChunkFromChunks(chunks)
			chunk.Choices = []model.ChunkChoice{{
				Index:        0,
				Delta:        model.Delta{Content: cleanedText},
				FinishReason: strPtr("stop"),
			}}
			chunkBytes, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
		} else {
			for _, c := range chunks {
				body, ok := strings.CutPrefix(string(c.Data), "data: ")
				if ok && body != "[DONE]" {
					var cc model.ChatCompletionChunk
					if err := json.Unmarshal([]byte(body), &cc); err == nil {
						modified := false
						for i := range cc.Choices {
							if cc.Choices[i].Delta.ReasoningContent != "" {
								cc.Choices[i].Delta.ReasoningContent = ""
								modified = true
							}
						}
						if modified {
							newData, _ := json.Marshal(cc)
							fmt.Fprintf(w, "data: %s\n\n", string(newData))
							continue
						}
					}
				}
				_, _ = w.Write(c.Data)
				_, _ = w.Write([]byte("\n\n"))
			}
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return false, nil, nil, markerIdx
	}

	mcpLog.Debug("streaming response contained tool_calls", "count", len(reconstructedToolCalls))
	keyID := reqmeta.GetKeyID(r.Context())

	newToolCalls := mcp.FilterNewToolCalls(req.Messages, reconstructedToolCalls)
	if len(newToolCalls) == 0 {
		mcpLog.Debug("all tool calls are duplicates in stream, writing clean response", "tool", reconstructedToolCalls[0].Function.Name)
		copyHeaders(w.Header(), rec.header)
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(rec.code)
		if cleanedText != "" || reasoningText != "" || len(reconstructedToolCalls) > 0 {
			chunk := baseChunkFromChunks(chunks)
			chunk.Choices = []model.ChunkChoice{{
				Index:        0,
				Delta:        model.Delta{Content: cleanedText},
				FinishReason: strPtr("stop"),
			}}
			chunkBytes, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return false, nil, nil, markerIdx
	}
	reconstructedToolCalls = newToolCalls

	// This is the first write to the real w in this turn (every other branch
	// above already copies headers before its first write) — without this,
	// X-Ilter-Model-Actual and friends never reach the client whenever the
	// loop needs 2+ turns: headers can only be set before the first byte, and
	// Go silently drops any header set after that on a later, header-copying
	// turn once this one has already committed the response.
	copyHeaders(w.Header(), rec.header)

	if m.toolEventWriter != nil && len(reconstructedToolCalls) > 0 {
		eventData, _ := json.Marshal(reconstructedToolCalls)
		m.toolEventWriter(w, "ilter.tool_calls", eventData)
	}

	if cleanedText != "" || reasoningText != "" || len(reconstructedToolCalls) > 0 {
		emitText := cleanedText
		if len(reconstructedToolCalls) > 0 && !strings.Contains(emitText, mcp.MarkerPrefix) {
			var sb strings.Builder
			sb.Grow(len(reconstructedToolCalls) * len(mcp.MarkerFor(0)))
			for range reconstructedToolCalls {
				sb.WriteString(mcp.MarkerFor(markerIdx))
				markerIdx++
			}
			emitText += sb.String()
		}
		chunk := baseChunkFromChunks(chunks)
		chunk.Choices = []model.ChunkChoice{{
			Index: 0,
			Delta: model.Delta{Content: emitText},
		}}
		chunkBytes, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	toolMsgs, toolErrors := m.executeFn(r.Context(), keyID, "", reconstructedToolCalls)

	assistantMsg := model.Message{
		Role:      "assistant",
		Content:   cleanedText,
		ToolCalls: reconstructedToolCalls,
	}

	if len(toolMsgs) == 0 {
		mcpLog.Warn("streaming tool execution produced no messages")
		errContent := cleanedText
		if stripped, _ := mcp.StripToolCallXML(errContent, 0); stripped != "" {
			errContent = stripped
		}
		if errContent == "" && len(reconstructedToolCalls) > 0 {
			errContent = fmt.Sprintf(
				"Tool %s could not be executed. The MCP server may be offline or not responding.",
				reconstructedToolCalls[0].Function.Name,
			)
		}
		if errContent != "" || reasoningText != "" || len(reconstructedToolCalls) > 0 {
			errChunk := baseChunkFromChunks(chunks)
			errChunk.Choices = []model.ChunkChoice{{
				Index:        0,
				Delta:        model.Delta{Content: errContent},
				FinishReason: strPtr("stop"),
			}}
			errBytes, _ := json.Marshal(errChunk)
			fmt.Fprintf(w, "data: %s\n\n", string(errBytes))
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return false, nil, nil, markerIdx
	}

	allMsgs := []model.Message{assistantMsg}
	if len(toolMsgs) > 0 {
		allMsgs = append(allMsgs, toolMsgs...)
	}

	return true, allMsgs, toolErrors, markerIdx
}
