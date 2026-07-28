package pii

import (
	"io"
	"regexp"
	"strings"
)

// maxPlaceholderLen is the maximum length of a PII placeholder string.
// Format: PII:TYPE:12hex where TYPE can be up to 11 chars (CREDIT_CARD).
// 4 (PII:) + 1 (:) + 11 (type) + 1 (:) + 12 (hex) = 29. Rounded to 32 for safety.
const maxPlaceholderLen = 32

type unmaskTarget struct {
	regex    *regexp.Regexp
	original string
}

type StreamUnmasker struct {
	targets []unmaskTarget
	buffer  []byte
}

func ExtractHash(placeholder string) string {
	placeholder = strings.ReplaceAll(placeholder, "_", ":")
	parts := strings.Split(placeholder, ":")
	if len(parts) > 0 {
		return strings.ToLower(parts[len(parts)-1])
	}
	return ""
}

// NewStreamUnmasker creates a new StreamUnmasker from a map of placeholder -> original value.
func NewStreamUnmasker(stateMappings map[string]string) *StreamUnmasker {
	targets := make([]unmaskTarget, 0, len(stateMappings))
	seenHashes := make(map[string]bool)

	for ph, original := range stateMappings {
		hash := ExtractHash(ph)
		if hash == "" || seenHashes[hash] {
			continue
		}
		seenHashes[hash] = true

		// Match any type name with this hash, e.g. PII:EMAIL:hash, PII_TCKN_hash, etc.
		re := regexp.MustCompile("(?i)PII[:_][A-Z0-9_]+[:_]" + regexp.QuoteMeta(hash))
		targets = append(targets, unmaskTarget{
			regex:    re,
			original: original,
		})
	}

	return &StreamUnmasker{
		targets: targets,
	}
}

// maxKeep returns the max bytes to retain as tail for partial-placeholder detection.
// We keep maxPlaceholderLen-1 bytes so the next chunk can complete a split placeholder.
func maxKeep(bufLen int) int {
	if bufLen < maxPlaceholderLen-1 {
		return bufLen
	}
	return maxPlaceholderLen - 1
}

// applyTargets runs all replacement targets on buf and returns the result.
func applyTargets(buf string, targets []unmaskTarget) string {
	for _, t := range targets {
		buf = t.regex.ReplaceAllString(buf, t.original)
	}
	return buf
}

// Process unmasks fully formed placeholders in the buffer and returns safe-to-emit content.
// Uses max-length tail buffer approach: keeps last maxPlaceholderLen-1 bytes in case
// a placeholder is split across chunk boundaries.
func (u *StreamUnmasker) Process(chunk string) string {
	if len(u.targets) == 0 {
		return chunk
	}
	u.buffer = append(u.buffer, chunk...)

	buf := string(u.buffer)
	buf = applyTargets(buf, u.targets)

	// Keep tail in case a placeholder is split across chunks
	keep := maxKeep(len(buf))
	emitLen := len(buf) - keep

	if emitLen > 0 {
		emit := buf[:emitLen]
		u.buffer = []byte(buf[emitLen:])
		return emit
	}

	// No safe-to-emit content, keep everything in buffer
	u.buffer = []byte(buf)
	return ""
}

// Flush emits all remaining buffered content after applying replacements one last time.
func (u *StreamUnmasker) Flush() string {
	emit := string(u.buffer)
	if len(u.targets) > 0 {
		emit = applyTargets(emit, u.targets)
	}
	u.buffer = nil
	return emit
}

// UnmaskWriter is an io.Writer that transparently unmasks PII placeholders
// in a streaming fashion. Use it with io.Copy to unmask SSE or JSON responses.
type UnmaskWriter struct {
	unmasker *StreamUnmasker
	w        io.Writer
}

func NewUnmaskWriter(w io.Writer, state *ReversibleState) *UnmaskWriter {
	return &UnmaskWriter{
		unmasker: NewStreamUnmasker(state.GetMappings()),
		w:        w,
	}
}

func (uw *UnmaskWriter) Write(p []byte) (int, error) {
	out := uw.unmasker.Process(string(p))
	if out != "" {
		if _, err := uw.w.Write([]byte(out)); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Must be called after io.Copy completes to avoid data loss.
func (uw *UnmaskWriter) Flush() error {
	out := uw.unmasker.Flush()
	if out != "" {
		_, err := uw.w.Write([]byte(out))
		return err
	}
	return nil
}
