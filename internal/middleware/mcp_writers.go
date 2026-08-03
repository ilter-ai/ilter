package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/model"
)

// bufferedResponseWriter buffers a complete HTTP response for inspection.
type bufferedResponseWriter struct {
	header http.Header
	code   int
	buf    *bytes.Buffer
}

func (b *bufferedResponseWriter) Header() http.Header { return b.header }

func (b *bufferedResponseWriter) WriteHeader(code int) { b.code = code }

func (b *bufferedResponseWriter) Write(p []byte) (int, error) {
	if b.code == 0 {
		b.code = http.StatusOK
	}
	return b.buf.Write(p)
}

func (b *bufferedResponseWriter) Flush() {}

// reasoningTeeWriter passes through the SSE stream while extracting
// reasoning_content into separate SSE events.
type reasoningTeeWriter struct {
	w           http.ResponseWriter
	flusher     http.Flusher
	header      http.Header
	code        int
	buf         *bytes.Buffer
	writeBuf    []byte
	headersSent bool
}

func (t *reasoningTeeWriter) Header() http.Header { return t.header }

func (t *reasoningTeeWriter) WriteHeader(code int) { t.code = code }

func (t *reasoningTeeWriter) Write(p []byte) (int, error) {
	if t.code == 0 {
		t.code = http.StatusOK
	}
	t.buf.Write(p)
	t.writeBuf = append(t.writeBuf, p...)

	for {
		idx := bytes.Index(t.writeBuf, []byte("\n\n"))
		if idx < 0 {
			break
		}
		event := t.writeBuf[:idx]
		t.writeBuf = t.writeBuf[idx+2:]

		body, ok := strings.CutPrefix(string(event), "data: ")
		if !ok || body == "[DONE]" {
			continue
		}
		var cc model.ChatCompletionChunk
		if err := json.Unmarshal([]byte(body), &cc); err != nil {
			continue
		}
		for _, ch := range cc.Choices {
			if ch.Delta.ReasoningContent != "" {
				teeChunk := model.ChatCompletionChunk{
					ID:      cc.ID,
					Object:  cc.Object,
					Created: cc.Created,
					Model:   cc.Model,
				}
				teeChunk.Choices = []model.ChunkChoice{{
					Index: ch.Index,
					Delta: model.Delta{ReasoningContent: ch.Delta.ReasoningContent},
				}}
				teeData, _ := json.Marshal(teeChunk)
				// This is the first write to the real t.w this turn — Go
				// implicitly (and, via the Flush below, immediately) commits
				// whatever headers are on t.w right now. Copy the downstream
				// handler's headers (X-Ilter-Model-Actual etc., set on t.header
				// via t.Header()) across first, or they're silently dropped
				// for every response that streams reasoning content.
				if !t.headersSent {
					copyHeaders(t.w.Header(), t.header)
					t.headersSent = true
				}
				fmt.Fprintf(t.w, "data: %s\n\n", string(teeData))
				if t.flusher != nil {
					t.flusher.Flush()
				}
				break
			}
		}
	}
	return len(p), nil
}

func (t *reasoningTeeWriter) Flush() {}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		if k == "Content-Length" || k == "Content-Encoding" || k == "Transfer-Encoding" {
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func baseChunkFromChunks(chunks []mcp.SSEChunk) model.ChatCompletionChunk {
	for _, c := range chunks {
		body, ok := strings.CutPrefix(string(c.Data), "data:")
		if !ok {
			continue
		}
		body = strings.TrimSpace(body)
		if body == "[DONE]" || body == "" {
			continue
		}
		var cc model.ChatCompletionChunk
		if err := json.Unmarshal([]byte(body), &cc); err == nil && (cc.ID != "" || cc.Model != "" || cc.Created > 0) {
			return cc
		}
	}
	return model.ChatCompletionChunk{
		Object: "chat.completion.chunk",
	}
}
