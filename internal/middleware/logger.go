package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ctx, meta := reqmeta.InitRequestMetadata(r.Context())
		r = r.WithContext(ctx)

		srw := &statusResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(srw, r)

		duration := time.Since(start)

		msg := r.Method + " " + r.URL.Path
		attrs := append(
			meta.SlogAttrs(),
			"status", srw.statusCode,
			"dur", fmt.Sprintf("%dms", duration.Milliseconds()),
		)

		if srw.statusCode >= 500 {
			slog.ErrorContext(r.Context(), msg, attrs...)
		} else if srw.statusCode >= 400 {
			slog.WarnContext(r.Context(), msg, attrs...)
		} else {
			slog.InfoContext(r.Context(), msg, attrs...)
		}
	})
}
