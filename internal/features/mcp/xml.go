package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
)

// Marker constants for tool call position tracking.
const (
	MarkerPrefix = "\ue000ilter:tool:"
	MarkerSuffix = "\ue001"
)

func MarkerFor(i int) string {
	return MarkerPrefix + strconv.Itoa(i) + MarkerSuffix
}

// FindToolCallsOpen finds an opening <tool_calls> tag starting from `from`.
func FindToolCallsOpen(s string, from int) (start, contentStart int, ok bool) {
	const prefix = "<tool_calls"
	for {
		i := strings.Index(s[from:], prefix)
		if i < 0 {
			return 0, 0, false
		}
		start = from + i
		k := start + len(prefix)
		if k >= len(s) {
			return 0, 0, false
		}
		switch s[k] {
		case ' ', '>', '\n', '\r', '\t', '/':
			e := strings.IndexByte(s[k:], '>')
			if e < 0 {
				return 0, 0, false
			}
			return start, k + e + 1, true
		default:
			from = k
		}
	}
}

// StripToolCallXML replaces tool call XML blocks with position markers.
// markerOffset is the starting index for emitted markers; callers that
// need marker indices to be globally unique across turns (e.g. the
// streaming handler that maps each marker to a toolEvents slot) must pass
// their per-request toolOffset. Callers without offset tracking pass 0.
func StripToolCallXML(content string, markerOffset int) (string, int) {
	if content == "" {
		return content, 0
	}
	markerIdx := markerOffset

	tcOff := 0
	for {
		iStart, iContentEnd, ok := FindToolCallsOpen(content, tcOff)
		if !ok {
			break
		}
		closeTag := "</tool_calls>"
		end := strings.Index(content[iContentEnd:], closeTag)
		if end < 0 {
			content = strings.TrimSpace(content[:iStart])
			tcOff = 0
			continue
		}
		fullEnd := iContentEnd + end + len(closeTag)
		inner := content[iContentEnd:fullEnd]
		invokeCount := 0
		scanOff := 0
		for {
			ii := strings.Index(inner[scanOff:], "<invoke")
			if ii < 0 {
				break
			}
			invokeStart := scanOff + ii
			if len(inner) > invokeStart+7 {
				ic := inner[invokeStart+7]
				if ic != ' ' && ic != '>' && ic != '\n' && ic != '\r' && ic != '\t' && ic != '/' {
					scanOff = invokeStart + 7
					continue
				}
			}
			invokeCount++
			iiClose := strings.Index(inner[invokeStart:], "</invoke>")
			if iiClose < 0 {
				scanOff = invokeStart + 7
			} else {
				scanOff = invokeStart + iiClose + len("</invoke>")
			}
		}
		if invokeCount == 0 {
			invokeCount = 1
		}
		var sb strings.Builder
		sb.Grow(invokeCount * len(MarkerFor(0)))
		for range invokeCount {
			sb.WriteString(MarkerFor(markerIdx))
			markerIdx++
		}
		replacement := sb.String()
		content = content[:iStart] + replacement + content[fullEnd:]
		tcOff = iStart + len(replacement)
	}

	invokeOff := 0
	for {
		i := strings.Index(content[invokeOff:], "<invoke")
		if i < 0 {
			break
		}
		start := invokeOff + i
		if len(content) > start+7 {
			c := content[start+7]
			if c != ' ' && c != '>' && c != '\n' && c != '\r' && c != '\t' && c != '/' {
				invokeOff = start + 7
				continue
			}
		}
		end := strings.Index(content[start:], "</invoke>")
		if end < 0 {
			content = strings.TrimSpace(content[:start])
			invokeOff = 0
			continue
		}
		marker := MarkerFor(markerIdx)
		markerIdx++
		content = content[:start] + marker + content[start+end+len("</invoke>"):]
		invokeOff = start + len(marker)
	}

	for {
		idx := strings.Index(content, "<parameter")
		if idx < 0 {
			break
		}
		gt := strings.IndexByte(content[idx:], '>')
		closeTagParam := "</parameter>"
		ci := strings.Index(content[idx:], closeTagParam)
		if gt >= 0 && ci >= 0 {
			end := idx + ci + len(closeTagParam)
			content = content[:idx] + content[end:]
		} else {
			content = strings.TrimSpace(content[:idx])
		}
	}
	for _, tag := range []string{"</tool_calls>", "</invoke>", "</parameter>"} {
		for {
			idx := strings.Index(content, tag)
			if idx < 0 {
				break
			}
			content = content[:idx] + content[idx+len(tag):]
		}
	}

	return strings.TrimSpace(content), markerIdx
}

// ParseXMLArgValue converts a string to its typed value (bool, int64, float64, or string).
func ParseXMLArgValue(v string) any {
	if v == "true" || v == "false" {
		return v == "true"
	}
	if i, err := strconv.ParseInt(v, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	return v
}

// parseXMLToolCall parses a single <invoke> block and returns the ToolCall and remaining text.
func parseXMLToolCall(text string) (*model.ToolCall, string) {
	startTag := "<invoke name=\""
	found := strings.Contains(text, startTag)
	if !found {
		return nil, ""
	}

	nameStart := strings.Index(text, startTag) + len(startTag)
	nameEnd := strings.IndexByte(text[nameStart:], '"')
	if nameEnd < 0 {
		return nil, ""
	}
	toolName := text[nameStart : nameStart+nameEnd]

	args := make(map[string]any)
	paramTag := "<parameter name=\""
	rest := text[nameStart+nameEnd:]
	for {
		invEnd := strings.Index(rest, "</invoke>")
		pIdx := strings.Index(rest, paramTag)
		if pIdx < 0 || (invEnd >= 0 && invEnd < pIdx) {
			break
		}
		keyStart := pIdx + len(paramTag)
		keyEnd := strings.IndexByte(rest[keyStart:], '"')
		if keyEnd < 0 {
			break
		}
		key := rest[keyStart : keyStart+keyEnd]
		valStart := keyStart + keyEnd + 1
		valTag := ">"
		vIdx := strings.Index(rest[valStart:], valTag)
		if vIdx < 0 {
			break
		}
		actualValStart := valStart + vIdx + 1
		endTag := "</parameter>"
		eIdx := strings.Index(rest[actualValStart:], endTag)
		if eIdx < 0 {
			break
		}
		val := rest[actualValStart : actualValStart+eIdx]
		args[key] = ParseXMLArgValue(val)
		rest = rest[actualValStart+eIdx+len(endTag):]
	}

	if toolName == "" {
		return nil, ""
	}

	argsJSON, _ := json.Marshal(args)
	var idBuf [8]byte
	_, _ = rand.Read(idBuf[:])
	tc := &model.ToolCall{
		ID:   "call_xml_" + hex.EncodeToString(idBuf[:]),
		Type: "function",
		Function: model.ToolCallFunctionData{
			Name:      toolName,
			Arguments: string(argsJSON),
		},
	}

	closeTag := "</tool_calls>"
	cIdx := strings.Index(text, closeTag)
	if cIdx >= 0 {
		end := cIdx + len(closeTag)
		return tc, text[end:]
	}

	invClose := "</invoke>"
	icIdx := strings.Index(text[nameStart+nameEnd:], invClose)
	if icIdx >= 0 {
		end := nameStart + nameEnd + icIdx + len(invClose)
		return tc, text[end:]
	}

	return tc, ""
}

// FindAllToolCallsInText scans text for <tool_calls> and bare <invoke> XML blocks.
func FindAllToolCallsInText(text string) ([]model.ToolCall, int) {
	var tcs []model.ToolCall
	xmlOffset := -1

	start, contentEnd, ok := FindToolCallsOpen(text, 0)
	if ok {
		xmlOffset = start
		closeTag := "</tool_calls>"
		closeIdx := strings.Index(text[contentEnd:], closeTag)
		if closeIdx >= 0 {
			afterClose := contentEnd + closeIdx + len(closeTag)
			if _, _, ok2 := FindToolCallsOpen(text, afterClose); ok2 {
				mcpLog.Warn("multiple <tool_calls> blocks found, only first parsed", "first_offset", start)
			}

			inner := text[contentEnd : contentEnd+closeIdx]
			remaining := inner
			for {
				tc, after := parseXMLToolCall(remaining)
				if tc == nil {
					break
				}
				tcs = append(tcs, *tc)
				remaining = after
			}
			if len(tcs) > 0 {
				return tcs, xmlOffset
			}
		}
	}

	tc, _ := parseXMLToolCall(text)
	if tc != nil {
		tcs = append(tcs, *tc)
		if xmlOffset < 0 {
			if idx := strings.Index(text, "<invoke"); idx >= 0 {
				xmlOffset = idx
			}
		}
	}

	return tcs, xmlOffset
}

// FilterNewToolCalls removes tool calls that duplicate existing ones in the last assistant message.
func FilterNewToolCalls(messages []model.Message, toolCalls []model.ToolCall) []model.ToolCall {
	if len(messages) == 0 {
		return toolCalls
	}

	var lastAssistant *model.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistant = &messages[i]
			break
		}
	}
	if lastAssistant == nil {
		return toolCalls
	}

	var newTCs []model.ToolCall
	for _, tc := range toolCalls {
		if !slices.ContainsFunc(lastAssistant.ToolCalls, func(existing model.ToolCall) bool {
			return existing.Function.Name == tc.Function.Name && existing.Function.Arguments == tc.Function.Arguments
		}) {
			newTCs = append(newTCs, tc)
		}
	}
	return newTCs
}

// ExtractToolCalls extracts tool calls from a ChatCompletionResponse.
func ExtractToolCalls(resp *model.ChatCompletionResponse) []model.ToolCall {
	for _, ch := range resp.Choices {
		if len(ch.Message.ToolCalls) > 0 {
			tcs := ch.Message.ToolCalls
			for i := range tcs {
				if tcs[i].Type == "" {
					tcs[i].Type = "function"
				}
			}
			return tcs
		}
	}
	return nil
}

// NormalizeTextToolCalls finds XML tool calls in response text content and
// promotes them to structured ToolCall objects.
func NormalizeTextToolCalls(resp *model.ChatCompletionResponse) bool {
	if len(resp.Choices) == 0 {
		return false
	}

	anyFound := false
	for i := range resp.Choices {
		ch := &resp.Choices[i]
		tcs, xmlOffset := FindAllToolCallsInText(ch.Message.Content)
		if len(tcs) == 0 {
			continue
		}

		beforeText := ch.Message.Content
		if xmlOffset >= 0 {
			beforeText = strings.TrimSpace(ch.Message.Content[:xmlOffset])
		}
		// If beforeText contains <think> block, remove thinking text before tool calls
		if idx := strings.Index(beforeText, "<think>"); idx >= 0 {
			if endIdx := strings.Index(beforeText[idx:], "</think>"); endIdx >= 0 {
				beforeText = strings.TrimSpace(beforeText[:idx] + beforeText[idx+endIdx+8:])
			} else {
				beforeText = strings.TrimSpace(beforeText[:idx])
			}
		}
		ch.Message.Content = beforeText

		ch.Message.ToolCalls = append(ch.Message.ToolCalls, tcs...)
		ch.FinishReason = "tool_calls"

		slog.Debug(
			"normalized text tool calls",
			"count", len(tcs),
			"first_tool", tcs[0].Function.Name,
		)

		anyFound = true
	}
	return anyFound
}

// HasToolResult checks if any message has a "tool" role.
func HasToolResult(messages []model.Message) bool {
	for i := range messages {
		if messages[i].Role == "tool" {
			return true
		}
	}
	return false
}

// HasToolSentinel checks if any message contains the tool sentinel prefix.
func HasToolSentinel(messages []model.Message) bool {
	for i := range messages {
		if s, ok := messages[i].Content.(string); ok && strings.Contains(s, ToolSentinelPrefix) {
			return true
		}
	}
	return false
}
