package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/ilter-ai/ilter/internal/model"
)

// chatResponseTranslator turns whatever ilter's internal /v1/chat/completions
// pipeline writes (OpenAI-shaped JSON or SSE) into some other client-facing
// wire format (Anthropic Messages, legacy text completions). Implementations
// hold whatever per-request/per-stream state they need; translatingResponseWriter
// drives them and owns all the mechanical HTTP/SSE plumbing.
type chatResponseTranslator interface {
	// streamChunk handles one decoded SSE chunk during a streamed response.
	streamChunk(w http.ResponseWriter, chunk *model.ChatCompletionChunk)
	// streamDone handles the terminal [DONE] SSE frame.
	streamDone(w http.ResponseWriter)
	// finishSuccess translates a complete non-streaming success body.
	finishSuccess(w http.ResponseWriter, body []byte)
	// finishError translates a complete error body (HTTP status >= 400).
	finishError(w http.ResponseWriter, status int, body []byte)
}

// translatingResponseWriter wraps the real http.ResponseWriter passed into the
// chat-completions handler chain, buffering its output and delegating
// format-specific translation to a chatResponseTranslator. It handles header
// negotiation, buffering, SSE frame splitting, and flushing, so each translator
// only has to implement the actual wire-format mapping. Call Finish once the
// wrapped chain's ServeHTTP call returns.
type translatingResponseWriter struct {
	underlying    http.ResponseWriter
	translator    chatResponseTranslator
	isStream      bool
	status        int
	headerWritten bool
	errorMode     bool
	buf           bytes.Buffer
}

func newTranslatingResponseWriter(w http.ResponseWriter, isStream bool, t chatResponseTranslator) *translatingResponseWriter {
	return &translatingResponseWriter{underlying: w, translator: t, isStream: isStream, status: http.StatusOK}
}

func (w *translatingResponseWriter) Header() http.Header { return w.underlying.Header() }

func (w *translatingResponseWriter) WriteHeader(status int) {
	if w.headerWritten {
		return
	}
	w.headerWritten = true
	w.status = status

	if status >= 400 {
		w.errorMode = true
		w.underlying.Header().Set("Content-Type", "application/json")
		w.underlying.WriteHeader(status)
		return
	}
	if w.isStream {
		w.underlying.Header().Set("Content-Type", "text/event-stream")
		w.underlying.Header().Set("Cache-Control", "no-cache")
		w.underlying.Header().Set("Connection", "keep-alive")
		w.underlying.WriteHeader(status)
		return
	}
	w.underlying.Header().Set("Content-Type", "application/json")
	w.underlying.WriteHeader(status)
}

func (w *translatingResponseWriter) Write(p []byte) (int, error) {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	w.buf.Write(p)
	if !w.errorMode && w.isStream {
		w.drainSSE()
	}
	return len(p), nil
}

func (w *translatingResponseWriter) Flush() {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.underlying.(http.Flusher); ok {
		f.Flush()
	}
}

// drainSSE peels every complete "\n\n"-terminated frame out of the buffer and
// hands it to handleSSEFrame, leaving any trailing partial frame buffered.
func (w *translatingResponseWriter) drainSSE() {
	for {
		data := w.buf.Bytes()
		idx := bytes.Index(data, []byte("\n\n"))
		if idx < 0 {
			return
		}
		frame := make([]byte, idx)
		copy(frame, data[:idx])
		w.buf.Next(idx + 2)
		w.handleSSEFrame(frame)
	}
}

func (w *translatingResponseWriter) handleSSEFrame(frame []byte) {
	frame = bytes.TrimSpace(frame)
	if len(frame) == 0 {
		return
	}
	if bytes.HasPrefix(frame, []byte(":")) || bytes.HasPrefix(frame, []byte("event:")) {
		return
	}
	payload := frame
	if after, ok := bytes.CutPrefix(frame, []byte("data: ")); ok {
		payload = after
	}
	if bytes.Equal(payload, []byte("[DONE]")) {
		w.translator.streamDone(w.underlying)
		w.Flush()
		return
	}

	var chunk model.ChatCompletionChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return
	}
	w.translator.streamChunk(w.underlying, &chunk)
	w.Flush()
}

// Finish flushes any buffered non-streaming body or error body, translated into
// the target wire format. Streaming responses are already emitted frame by
// frame, so Finish is a no-op for them.
func (w *translatingResponseWriter) Finish() {
	switch {
	case w.errorMode:
		w.translator.finishError(w.underlying, w.status, w.buf.Bytes())
	case !w.isStream:
		w.translator.finishSuccess(w.underlying, w.buf.Bytes())
	}
}
