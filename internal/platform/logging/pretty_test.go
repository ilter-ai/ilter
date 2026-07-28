package logging

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestPrettyHandler(t *testing.T) {
	var buf bytes.Buffer
	opts := slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := NewPrettyHandler(&buf, opts)
	logger := slog.New(handler)

	// 1. Standard log
	logger.Info("Hello, world!", "key1", "val1", "key2", 42)
	output := stripANSI(buf.String())

	if !strings.Contains(output, "INFO") {
		t.Errorf("Expected output to contain level INFO, got: %s", output)
	}
	if !strings.Contains(output, "Hello, world!") {
		t.Errorf("Expected output to contain message, got: %s", output)
	}
	if !strings.Contains(output, "key1=val1") {
		t.Errorf("Expected output to contain formatted string attribute, got: %s", output)
	}
	if !strings.Contains(output, "key2=42") {
		t.Errorf("Expected output to contain formatted int attribute, got: %s", output)
	}

	buf.Reset()

	// 2. Log with OTel Context
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:  [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	logger.InfoContext(ctx, "Traced log message")
	output = stripANSI(buf.String())

	// PrettyHandler truncates trace/span IDs to 8 chars and strips "_id" suffix.
	expectedTraceStr := "trace=01020304"
	expectedSpanStr := "span=01020304"
	if !strings.Contains(output, expectedTraceStr) {
		t.Errorf("Expected output to contain trace ID %s, got: %s", expectedTraceStr, output)
	}
	if !strings.Contains(output, expectedSpanStr) {
		t.Errorf("Expected output to contain span ID %s, got: %s", expectedSpanStr, output)
	}
}
