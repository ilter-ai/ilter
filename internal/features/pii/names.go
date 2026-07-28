package pii

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/bits-and-blooms/bloom/v3"

	piiembed "github.com/ilter-ai/ilter/data/pii"
)

// NameSource provides a list of names. Implementations can load from files,
// embedded data, databases, or any other source.
type NameSource interface {
	Load() ([]string, error)
}

// CompositeSource combines multiple sources, deduplicating by lowercased name.
type CompositeSource struct {
	Sources []NameSource
}

func (c *CompositeSource) Load() ([]string, error) {
	seen := make(map[string]struct{})
	var result []string
	for _, src := range c.Sources {
		names, err := src.Load()
		if err != nil {
			slog.Warn("Failed to load PII names from source", "error", err)
			continue
		}
		for _, name := range names {
			lower := strings.ToLower(name)
			if _, exists := seen[lower]; !exists {
				seen[lower] = struct{}{}
				result = append(result, lower)
			}
		}
	}
	return result, nil
}

// EmbedSource loads names from compile-time embedded data (plain or gzip-compressed).
type EmbedSource struct {
	Data []byte
}

func (e *EmbedSource) Load() ([]string, error) {
	r := bytes.NewReader(e.Data)
	if len(e.Data) >= 2 && e.Data[0] == 0x1f && e.Data[1] == 0x8b {
		gzr, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("decompressing embedded data: %w", err)
		}
		defer gzr.Close()
		return readNames(gzr)
	}
	return readNames(r)
}

func readNames(r io.Reader) ([]string, error) {
	var names []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			names = append(names, strings.ToLower(line))
		}
	}
	return names, scanner.Err()
}

// NameDetector uses Bloom Filter for fast negative detection and
// a sorted list of names with binary search for exact positive verification.
// This double-check eliminates Bloom Filter false positives while keeping
// memory usage minimal (~10 MB for 315k names).
type NameDetector struct {
	bloom *bloom.BloomFilter
	names []string
}

// NewNameDetector creates a NameDetector loading from the given sources.
// If no sources are provided, it uses compile-time embedded name lists
// (en.txt and tr.txt from data/names/).
func NewNameDetector(sources ...NameSource) *NameDetector {
	var names []string
	if len(sources) > 0 {
		composite := &CompositeSource{Sources: sources}
		loaded, err := composite.Load()
		if err == nil && len(loaded) > 0 {
			names = loaded
		}
	}

	if len(names) == 0 {
		enData, err := piiembed.NamesFS.ReadFile("names/en.txt.gz")
		if err != nil {
			slog.Warn("failed to read embedded names data", "error", err)
		}
		trData, err := piiembed.NamesFS.ReadFile("names/tr.txt.gz")
		if err != nil {
			slog.Warn("failed to read embedded names data", "error", err)
		}
		embedded := CompositeSource{Sources: []NameSource{
			&EmbedSource{Data: enData},
			&EmbedSource{Data: trData},
		}}
		loaded, err := embedded.Load()
		if err == nil && len(loaded) > 0 {
			names = loaded
		}
	}

	if len(names) == 0 {
		names = defaultNames
	}

	n := uint(len(names))
	if n == 0 {
		n = 1
	}

	bf := bloom.NewWithEstimates(n, 0.001) // 0.1% false positive rate
	for _, name := range names {
		bf.AddString(name)
	}
	sort.Strings(names)

	slog.Info("PII detector ready", "names", n, "bloom_bits", bf.Cap(), "hash_funcs", bf.K())

	return &NameDetector{bloom: bf, names: names}
}

func (nd *NameDetector) IsName(word string) bool {
	word = strings.ToLower(word)
	if !nd.bloom.TestString(word) {
		return false
	}
	idx := sort.SearchStrings(nd.names, word)
	return idx < len(nd.names) && nd.names[idx] == word
}

// defaultNames is a small embedded fallback for when no external files are available.
var defaultNames = []string{
	"ahmet", "mehmet", "mustafa", "hasan", "hüseyin", "kemal", "ali", "veli",
	"ayşe", "fatma", "emine", "zeynep", "elif", "meryem", "hatice", "asnur",
	"john", "james", "robert", "michael", "william", "david", "richard", "joseph",
	"mary", "patricia", "jennifer", "linda", "barbara", "elizabeth", "susan", "jessica",
	"charles", "thomas", "christopher", "daniel", "matthew", "anthony", "mark", "donald",
	"steven", "paul", "andrew", "joshua", "kenneth", "kevin", "brian", "george",
	"edward", "ronald", "timothy", "jason", "jeffrey", "ryan", "jacob", "gary",
	"nicholas", "eric", "jonathan", "stephen", "larry", "justin", "scott", "brandon",
	"benjamin", "samuel", "raymond", "patrick", "alexander", "jack", "dennis", "jerry",
	"tyler", "aaron", "jose", "adam", "nathan", "henry", "peter", "kyle",
	"walter", "harold", "jeremy", "ethan", "carl", "keith", "roger", "gerald",
	"christian", "terry", "sean", "arthur", "austin", "noah", "lawrence", "wayne",
	"can", "cem", "efe", "berk", "mert", "emre", "burak", "murat",
	"hakan", "serkan", "gökhan", "volkan", "tolga", "kaan", "arda", "yiğit",
	"ozan", "deniz", "umut", "caner", "onur", "sinan", "tarkan", "barış",
	"çetin", "rıza", "yaşar", "kadir", "tarık", "levent", "burhan", "nesrin",
	"gül", "suna", "aylin", "eda", "deniz", "ece", "dilara", "irem",
	"buse", "çağla", "melis", "selen", "aslı", "oze", "beril", "defne",
}
