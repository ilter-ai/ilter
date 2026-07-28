package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
)

const (
	gray   = "\x1b[90m"
	blue   = "\x1b[34m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	cyan   = "\x1b[36m"
	reset  = "\x1b[0m"
)

type PrettyHandler struct {
	opts   slog.HandlerOptions
	out    io.Writer
	attrs  []slog.Attr
	prefix string // dotted group prefix, e.g. "http.db."
}

func NewPrettyHandler(out io.Writer, opts slog.HandlerOptions) *PrettyHandler {
	return &PrettyHandler{
		opts: opts,
		out:  out,
	}
}

func (h *PrettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *PrettyHandler) Handle(ctx context.Context, r slog.Record) error {
	var buf bytes.Buffer

	isDebug := r.Level == slog.LevelDebug
	isError := r.Level == slog.LevelError

	timeStr := r.Time.Format("2006-01-02 15:04:05.000")
	buf.WriteString(gray + timeStr + reset + "  ")

	var levelColor string
	switch r.Level {
	case slog.LevelDebug:
		levelColor = gray
	case slog.LevelInfo:
		levelColor = green
	case slog.LevelWarn:
		levelColor = yellow
	case slog.LevelError:
		levelColor = red
	default:
		levelColor = reset
	}
	buf.WriteString(levelColor + levelLabel(r.Level) + reset + "  ")

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		traceID := spanCtx.TraceID().String()
		spanID := spanCtx.SpanID().String()
		if len(traceID) > 8 {
			traceID = traceID[:8]
		}
		if len(spanID) > 8 {
			spanID = spanID[:8]
		}
		buf.WriteString(fmt.Sprintf("%strace=%s span=%s%s  ", gray, traceID, spanID, reset))
	}

	buf.WriteString(r.Message)

	var allAttrs []slog.Attr
	allAttrs = append(allAttrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		allAttrs = append(allAttrs, a)
		return true
	})

	if len(allAttrs) > 0 {
		buf.WriteString("  ")
		first := true
		for _, a := range allAttrs {
			if a.Key == "" {
				continue
			}
			if !first {
				buf.WriteString("  ")
			}
			first = false
			key := h.prefix + a.Key
			valStr := formatValue(a.Value)

			if isError && a.Key == "error" {
				buf.WriteString(fmt.Sprintf("%s%s%s=%s%s%s", blue, key, reset, red, valStr, reset))
			} else {
				buf.WriteString(fmt.Sprintf("%s%s%s=%s", blue, key, reset, valStr))
			}
		}
	}

	buf.WriteByte('\n')

	if isDebug {
		line := buf.Bytes()
		buf.Reset()
		buf.WriteString(gray)
		buf.Write(line[:len(line)-1])
		buf.WriteString(reset)
		buf.WriteByte('\n')
	}

	_, err := h.out.Write(buf.Bytes())
	return err
}

func levelLabel(l slog.Level) string {
	switch l {
	case slog.LevelDebug:
		return "DBUG"
	case slog.LevelInfo:
		return "INFO"
	case slog.LevelWarn:
		return "WARN"
	case slog.LevelError:
		return "ERRO"
	default:
		s := l.String()
		if len(s) > 4 {
			s = s[:4]
		}
		return s
	}
}

func needsQuote(s string) bool {
	return s == "" || strings.ContainsAny(s, " =|\"\n\r\t")
}

// looksLikeJSON reports whether s is shaped like a JSON object or array.
// Log attributes carrying raw JSON (tool call arguments, request bodies)
// are already self-delimited by braces/brackets — quoting them with %q
// escapes every internal `"` as `\"`, which is unreadable at a glance and
// gains nothing over printing the JSON as-is.
func looksLikeJSON(s string) bool {
	if len(s) < 2 {
		return false
	}
	first, last := s[0], s[len(s)-1]
	return (first == '{' && last == '}') || (first == '[' && last == ']')
}

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if looksLikeJSON(s) {
			return s
		}
		if needsQuote(s) {
			return fmt.Sprintf("%q", s)
		}
		return s
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	default:
		return v.String()
	}
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	newAttrs = append(newAttrs, h.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &PrettyHandler{
		opts:   h.opts,
		out:    h.out,
		attrs:  newAttrs,
		prefix: h.prefix,
	}
}

func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &PrettyHandler{
		opts:   h.opts,
		out:    h.out,
		attrs:  h.attrs,
		prefix: h.prefix + name + ".",
	}
}
