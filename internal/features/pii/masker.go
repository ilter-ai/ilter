package pii

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ilter-ai/ilter/internal/model"
)

type Type string

const (
	PIIEmail      Type = "email"
	PIIPhone      Type = "phone"
	PIICreditCard Type = "credit_card"
	PIITurkishID  Type = "turkish_id"
	PIISSN        Type = "ssn"
	PIIIPv4       Type = "ipv4"
	PIINames      Type = "names"
)

const (
	maxPIIScanSize = 100 * 1024 // 100KB: truncate inputs larger than this to bound regex execution time
)

const (
	ActionMask           = "mask"
	ActionMaskReversible = "mask_reversible"
	ActionBlock          = "block"
	ActionLogOnly        = "log_only"
)

func normalizeAction(a string) string {
	switch a {
	case ActionMask, ActionMaskReversible, ActionBlock, ActionLogOnly:
		return a
	}
	return ActionMask
}

type Pattern struct {
	Name    string
	Regex   string
	Re      *regexp.Regexp
	Enabled bool
	Action  string
}

var (
	LoadedPatterns = make(map[string]Pattern)
	patternMu      sync.RWMutex

	nameContextPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:benim adım|my name is|i'm|ben)\s+([a-zA-ZçıöşğüÇIÖŞĞÜ]+)`),
		regexp.MustCompile(`(?i)(?:dear|sayın|mr\.?|mrs\.?|ms\.?)\s+([a-zA-ZçıöşğüÇIÖŞĞÜ]+)`),
		regexp.MustCompile(`(?i)(?:from|kimden|gönderen):\s*([a-zA-ZçıöşğüÇIÖŞĞÜ]+(?:\s+[a-zA-ZçıöşğüÇIÖŞĞÜ]+)?)`),
	}
)

// LoadPatternsFromDB populates the global LoadedPatterns map by reading from
// the pii_patterns table. It is defined in db.go (separate file to keep the
// database dependency isolated).

// ReversibleState tracks the mappings from placeholders to original PII values.
type ReversibleState struct {
	mu       sync.Mutex
	mappings map[string]string
}

func NewReversibleState() *ReversibleState {
	return &ReversibleState{
		mappings: make(map[string]string),
	}
}

// Clear removes all mappings to prevent memory leaks.
func (s *ReversibleState) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.mappings {
		delete(s.mappings, k)
	}
}

// GetMappings returns a copy of the mappings for debugging/logging.
func (s *ReversibleState) GetMappings() map[string]string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make(map[string]string)
	for k, v := range s.mappings {
		res[k] = v
	}
	return res
}

// Masker handles PII detection, masking, reversible mapping, and blocking.
// NameDetector is lazily initialized on first use via detectorOnce
// so ~10 MB of name data is only allocated when PII name detection is actually needed.
type Masker struct {
	mode         string
	patterns     map[string]bool
	nameDetector *NameDetector
	detectorOnce sync.Once
}

// NewMasker creates a new Masker with embedded name data.
func NewMasker(mode string, enabledPatterns []string) *Masker {
	if mode == "" {
		mode = "mask"
	}
	patterns := make(map[string]bool)
	if len(enabledPatterns) == 0 {
		patterns["names"] = true
		patternMu.RLock()
		for name := range LoadedPatterns {
			patterns[name] = true
		}
		patternMu.RUnlock()
	} else {
		for _, p := range enabledPatterns {
			patterns[p] = true
		}
	}

	// NameDetector not created here — lazy init via getDetector()
	// saves ~10 MB of RAM when PII name detection is disabled.
	return &Masker{
		mode:     mode,
		patterns: patterns,
	}
}

// getDetector lazily initializes the NameDetector on first call.
// The ~10 MB name data is only allocated when PII name detection is actually used.
func (m *Masker) getDetector() *NameDetector {
	m.detectorOnce.Do(func() {
		m.nameDetector = NewNameDetector()
	})
	return m.nameDetector
}

type piiRange struct {
	start  int
	end    int
	value  string
	pType  Type
	action string
}

func generateRandomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "abc123"
	}
	return hex.EncodeToString(bytes)
}

func isTurkishLetter(r rune) bool {
	switch r {
	case 'ç', 'Ç', 'ğ', 'Ğ', 'ı', 'İ', 'ö', 'Ö', 'ş', 'Ş', 'ü', 'Ü':
		return true
	}
	return false
}

func isValidLuhn(cc string) bool {
	var digits []int
	for _, r := range cc {
		if r >= '0' && r <= '9' {
			digits = append(digits, int(r-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 16 {
		return false
	}
	// Reject all-zero numbers (degenerate case that passes Luhn checksum)
	allZero := true
	for _, d := range digits {
		if d != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return false
	}
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

func isValidTurkishID(id string) bool {
	if len(id) != 11 {
		return false
	}
	if id[0] == '0' {
		return false
	}
	var d [11]int
	for i, r := range id {
		if r < '0' || r > '9' {
			return false
		}
		d[i] = int(r - '0')
	}
	sumOdd := d[0] + d[2] + d[4] + d[6] + d[8]
	sumEven := d[1] + d[3] + d[5] + d[7]
	d10 := (sumOdd*7 - sumEven) % 10
	if d10 < 0 {
		d10 += 10
	}
	if d[9] != d10 {
		return false
	}
	sumAll := 0
	for i := 0; i < 10; i++ {
		sumAll += d[i]
	}
	if d[10] != sumAll%10 {
		return false
	}
	return d[10]%2 == 0
}

func (m *Masker) findPIIRanges(text string) []piiRange {
	if len(text) > maxPIIScanSize {
		slog.Warn("PII scan input truncated", "size", len(text), "max", maxPIIScanSize)
		text = text[:maxPIIScanSize]
	}
	var ranges []piiRange

	patternMu.RLock()
	for name, p := range LoadedPatterns {
		if !p.Enabled {
			continue
		}
		if m.patterns[name] && p.Re != nil {
			indexes := p.Re.FindAllStringIndex(text, -1)
			for _, idx := range indexes {
				val := text[idx[0]:idx[1]]
				if name == "credit_card" {
					if !isValidLuhn(val) {
						continue
					}
				} else if name == "tckn" || name == "turkish_id" {
					if !isValidTurkishID(val) {
						continue
					}
				} else if name == "us_zip" {
					// "port=70000" are common in prompts and rarely real ZIP disclosure.
					// Full ZIP validation needs a DB; this heuristic handles 90%+ FPs.
					if idx[0] > 0 && (text[idx[0]-1] == '=' || text[idx[0]-1] == ':') {
						continue
					}
				}
				piiType := Type(name)
				if name == "tckn" {
					piiType = PIITurkishID
				}
				ranges = append(ranges, piiRange{
					start:  idx[0],
					end:    idx[1],
					value:  val,
					pType:  piiType,
					action: normalizeAction(p.Action),
				})
			}
		}
	}
	patternMu.RUnlock()

	if m.patterns["names"] {
		namesPattern, hasNamesPattern := LoadedPatterns["names"]
		namesAction := ActionMask
		if hasNamesPattern && namesPattern.Enabled {
			namesAction = normalizeAction(namesPattern.Action)
		}
		for _, re := range nameContextPatterns {
			indexes := re.FindAllStringSubmatchIndex(text, -1)
			for _, idx := range indexes {
				if len(idx) < 4 {
					continue
				}
				grpStart := idx[2]
				grpEnd := idx[3]
				if grpStart < 0 || grpEnd < 0 {
					continue
				}
				candidate := text[grpStart:grpEnd]

				currentWordStart := -1
				for offset, r := range candidate {
					isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || isTurkishLetter(r)
					if isLetter {
						if currentWordStart == -1 {
							currentWordStart = offset
						}
					} else {
						if currentWordStart != -1 {
							word := candidate[currentWordStart:offset]
							if m.getDetector().IsName(word) {
								ranges = append(ranges, piiRange{
									start:  grpStart + currentWordStart,
									end:    grpStart + offset,
									value:  word,
									pType:  PIINames,
									action: namesAction,
								})
							}
							currentWordStart = -1
						}
					}
				}
				if currentWordStart != -1 {
					word := candidate[currentWordStart:]
					if m.getDetector().IsName(word) {
						ranges = append(ranges, piiRange{
							start:  grpStart + currentWordStart,
							end:    grpStart + len(candidate),
							value:  word,
							pType:  PIINames,
							action: namesAction,
						})
					}
				}
			}
		}
	}

	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start == ranges[j].start {
			return ranges[i].end < ranges[j].end
		}
		return ranges[i].start < ranges[j].start
	})

	var uniqueRanges []piiRange
	lastEnd := -1
	for _, r := range ranges {
		if r.start >= lastEnd {
			uniqueRanges = append(uniqueRanges, r)
			lastEnd = r.end
		}
	}

	return uniqueRanges
}

func (m *Masker) ProcessText(text string, state *ReversibleState) (string, error) {
	ranges := m.findPIIRanges(text)
	if len(ranges) == 0 {
		return text, nil
	}

	// Apply global mode as override to per-pattern actions.
	// "block" rejects any PII. "reversible" generates placeholders
	// for all masked PII. Individual patterns with explicit
	// ActionBlock or ActionLogOnly retain their specific behavior.
	for i := range ranges {
		switch m.mode {
		case "block":
			ranges[i].action = ActionBlock
		case "reversible":
			if ranges[i].action != ActionBlock && ranges[i].action != ActionLogOnly {
				ranges[i].action = ActionMaskReversible
			}
		}
	}

	for _, r := range ranges {
		if r.action == ActionBlock {
			return "", model.ErrPIIBlocked
		}
	}

	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		switch r.action {
		case ActionLogOnly:
			continue
		case ActionMaskReversible:
			if state == nil {
				text = text[:r.start] + "<MASKED_PII>" + text[r.end:]
				continue
			}
			state.mu.Lock()
			placeholder := ""
			for {
				h := generateRandomHex(6)
				placeholder = "PII:" + strings.ToUpper(string(r.pType)) + ":" + h
				if _, exists := state.mappings[placeholder]; !exists {
					break
				}
			}
			state.mappings[placeholder] = r.value
			state.mu.Unlock()
			text = text[:r.start] + placeholder + text[r.end:]
		case ActionMask, "":
			text = text[:r.start] + "<MASKED_PII>" + text[r.end:]
		}
	}

	return text, nil
}

// Unmask restores the original PII values using the mappings inside ReversibleState.
func (m *Masker) Unmask(text string, state *ReversibleState) string {
	if state == nil {
		return text
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for placeholder, original := range state.mappings {
		hash := ExtractHash(placeholder)
		if hash == "" {
			continue
		}
		re := regexp.MustCompile("(?i)PII[:_][A-Z0-9_]+[:_]" + regexp.QuoteMeta(hash))
		text = re.ReplaceAllString(text, original)
	}
	return text
}

type Match struct {
	Type   string
	Value  string
	Action string
}

func (m *Masker) DetectPII(text string) []Match {
	ranges := m.findPIIRanges(text)
	if len(ranges) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var res []Match
	for _, r := range ranges {
		key := string(r.pType)
		if !seen[key] {
			seen[key] = true
			res = append(res, Match{Type: key, Value: r.value, Action: r.action})
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Type < res[j].Type })
	return res
}

func (m *Masker) Mode() string {
	return m.mode
}
