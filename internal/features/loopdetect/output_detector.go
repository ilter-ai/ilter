package loopdetect

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// OutputLoopResult is returned by OutputDetector.Feed.
type OutputLoopResult struct {
	Detected    bool
	RepeatCount int
	Sentence    string
	Mode        string // "observe" or "enforce"
}

// OutputDetector detects repetitive sentence patterns in streaming LLM output.
// It accumulates delta chunks, identifies complete sentence boundaries, and
// flags when the same sentence appears consecutively past a threshold.
//
// Consecutive identical sentences are by far the strongest signal for an output
// loop. This detector uses a position-tracked buffer to avoid re-scanning.
//
// Thread-safe: intended for single-streamer use (created per streaming request).
type OutputDetector struct {
	mu             sync.Mutex
	threshold      int    // consecutive repeats to trigger (default 6)
	minSentenceLen int    // minimum rune count for a sentence (default 20)
	maxSentenceLen int    // maximum rune count to consider (default 300)
	mode           string // "off" | "observe" | "enforce"

	buf          strings.Builder
	extractPos   int    // byte offset up to which we have extracted sentences
	lastSentence string // last complete sentence (normalised)
	repeatCount  int    // how many times lastSentence has repeated consecutively
}

// NewOutputDetector creates an OutputDetector with the given parameters.
// Zero values are replaced with sensible defaults.
func NewOutputDetector(threshold, minSentenceLen int, mode string) *OutputDetector {
	if threshold <= 0 {
		threshold = 6
	}
	if minSentenceLen <= 0 {
		minSentenceLen = 20
	}
	if mode == "" {
		mode = "observe"
	}
	return &OutputDetector{
		threshold:      threshold,
		minSentenceLen: minSentenceLen,
		maxSentenceLen: 300,
		mode:           mode,
	}
}

// Reset clears the detector state for reuse across requests.
func (d *OutputDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.buf.Reset()
	d.extractPos = 0
	d.lastSentence = ""
	d.repeatCount = 0
}

// Feed processes a text delta and returns the detection result.
// It should be called for every non-empty delta chunk received from the provider.
func (d *OutputDetector) Feed(delta string) OutputLoopResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.mode == "off" || len(delta) == 0 {
		return OutputLoopResult{}
	}

	d.buf.WriteString(delta)
	text := d.buf.String()

	// Scan the *new* portion of the buffer for complete sentences.
	// A sentence is "complete" when it ends with a sentence-end character
	// (., !, ?, :) followed by whitespace, uppercase letter, or end-of-string.
	// '\n' is always a boundary.
	start := d.extractPos
	if start >= len(text) {
		return OutputLoopResult{}
	}

	var lastComplete string // the latest complete sentence found in this scan

	i := start
	for i < len(text) {
		// Find the next sentence-end character
		nextEnd := strings.IndexAny(text[i:], ".!?\n:")
		if nextEnd < 0 {
			break // no sentence boundary in the remaining text
		}
		boundary := i + nextEnd
		ch := text[boundary]

		// Check that the boundary char is actually a sentence end:
		//   - '.' '!' '?' must be followed by whitespace, newline, or end-of-string
		//   - ':' must be followed by whitespace, uppercase letter, or end-of-string
		//   - '\n' is always a boundary
		var maybeBoundary bool
		if ch == '\n' {
			maybeBoundary = true
		} else {
			if boundary+1 >= len(text) {
				maybeBoundary = true // end of accumulated text
			} else {
				nextByte := text[boundary+1]
				maybeBoundary = nextByte == ' ' || nextByte == '\n' || nextByte == '\r' || nextByte == '\t'
				if !maybeBoundary && ch == ':' {
					// ':' followed by uppercase is a common Turkish sentence boundary
					maybeBoundary = nextByte >= 'A' && nextByte <= 'Z'
				}
			}
		}

		if maybeBoundary {
			// Extract the sentence: from after the previous boundary to this boundary (inclusive)
			prevBoundary := start
			if prevBoundary < 0 {
				prevBoundary = 0
			}
			sentence := strings.TrimSpace(text[prevBoundary : boundary+1])
			if utf8.RuneCountInString(sentence) >= d.minSentenceLen &&
				utf8.RuneCountInString(sentence) <= d.maxSentenceLen {
				lastComplete = sentence
			}
			start = boundary + 1 // move past this boundary
		}

		i = boundary + 1
	}

	if lastComplete == "" {
		// No new complete sentence found — keep waiting for more data
		return OutputLoopResult{}
	}

	// Normalise for comparison: trim, collapse whitespace
	normalised := strings.TrimSpace(lastComplete)
	normalised = collapseSpaces(normalised)

	if normalised == d.lastSentence && normalised != "" {
		d.repeatCount++
		// repeatCount counts consecutive occurrences AFTER the first.
		// threshold N means "detect on the Nth total occurrence".
		// So repeatCount = N-1 triggers detection.
		if d.repeatCount+1 >= d.threshold {
			return OutputLoopResult{
				Detected:    true,
				RepeatCount: d.repeatCount + 1,
				Sentence:    lastComplete,
				Mode:        d.mode,
			}
		}
	} else {
		d.lastSentence = normalised
		d.repeatCount = 0
	}

	// Update extraction position to avoid re-scanning
	d.extractPos = start

	return OutputLoopResult{}
}

// collapseSpaces replaces sequences of whitespace with a single space.
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
		} else {
			b.WriteByte(ch)
			inSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}
