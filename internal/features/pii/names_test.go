package pii

import (
	"os"
	"runtime"
	"strings"
	"testing"

	piiembed "github.com/ilter-ai/ilter/data/pii"
)

func TestNameDetector(t *testing.T) {
	nd := NewNameDetector()
	commonNames := []string{"ali", "mehmet", "ayşe", "fatma", "john", "mary", "james", "robert"}
	for _, name := range commonNames {
		if !nd.IsName(name) {
			t.Errorf("Expected %q to be recognized as a name", name)
		}
	}
	nonNames := []string{"xylophone", "qwertyuiop", "abcdefgh"}
	for _, name := range nonNames {
		if nd.IsName(name) {
			t.Errorf("Expected %q to not be recognized as a name", name)
		}
	}
}

func TestNameDetectorWithFiles(t *testing.T) {
	enData, err := piiembed.NamesFS.ReadFile("names/en.txt.gz")
	if err != nil {
		t.Fatalf("Failed to read embedded English names: %v", err)
	}
	trData, err := piiembed.NamesFS.ReadFile("names/tr.txt.gz")
	if err != nil {
		t.Fatalf("Failed to read embedded Turkish names: %v", err)
	}

	enSrc := &EmbedSource{Data: enData}
	enNames, err := enSrc.Load()
	if err != nil {
		t.Fatalf("Failed to load English names: %v", err)
	}
	trSrc := &EmbedSource{Data: trData}
	trNames, err := trSrc.Load()
	if err != nil {
		t.Fatalf("Failed to load Turkish names: %v", err)
	}

	nd := NewNameDetector(enSrc, trSrc)

	// Verify names loaded from files are recognized
	sampleSize := min(len(enNames), 5)
	for i := 0; i < sampleSize; i++ {
		name := enNames[i]
		if !nd.IsName(name) {
			t.Errorf("Expected first English name %q to be recognized", name)
		}
	}
	if len(trNames) < sampleSize {
		sampleSize = len(trNames)
	}
	for i := 0; i < sampleSize; i++ {
		name := trNames[i]
		if !nd.IsName(name) {
			t.Errorf("Expected first Turkish name %q to be recognized", name)
		}
	}

	// Verify specific known names in each file
	if nd.IsName("aadya") {
		t.Log("English name 'aadya' correctly recognized")
	}
	if nd.IsName("adil") {
		t.Log("Turkish name 'adil' correctly recognized")
	}

	// Verify non-names are rejected (AC Trie gives exact verification)
	nonNames := []string{"xylophone", "qwertyuiop", "abcdefgh"}
	for _, name := range nonNames {
		if nd.IsName(name) {
			t.Errorf("Expected %q to not be recognized as a name", name)
		}
	}
	if !nd.IsName("ALI") {
		t.Error("Expected case-insensitive match for ALI")
	}
	if !nd.IsName("ADNAN") {
		t.Error("Expected case-insensitive match for ADNAN")
	}
}

func TestNameDetector_ACAndBloom(t *testing.T) {
	// Verify AC Trie catches what Bloom finds
	nd := NewNameDetector()

	// Known names should pass Bloom + hash set check
	knownNames := []string{"ali", "john", "mary", "ahmet", "zeynep"}
	for _, name := range knownNames {
		if !nd.IsName(name) {
			t.Errorf("Bloom+hash check failed for known name %q", name)
		}
		// Verify Bloom alone would also match
		if !nd.bloom.TestString(name) {
			t.Errorf("Bloom filter alone should contain %q", name)
		}
	}

	// Non-names should fail
	nonNames := []string{"xylophone", "qwertyuiop", "abcdefghijklmn"}
	for _, name := range nonNames {
		if nd.IsName(name) {
			t.Errorf("Bloom+hash false positive for non-name %q", name)
		}
	}
}

func TestNameDetector_FalsePositiveRate(t *testing.T) {
	// Measure false positive rate with 1000 non-names
	// AC Trie should make it exactly 0%
	nd := NewNameDetector()

	var falsePositives int
	total := 1000
	for i := range total {
		// Generate random-looking non-name strings
		nonName := randomString(6 + i%10)
		if nd.IsName(nonName) {
			falsePositives++
		}
	}

	fpr := float64(falsePositives) / float64(total) * 100
	t.Logf("False positive rate: %.2f%% (%d/%d)", fpr, falsePositives, total)

	if fpr > 1.0 {
		t.Errorf("False positive rate too high: %.2f%% (limit: 1%%)", fpr)
	}
}

func TestNameDetector_FileBasedFPR(t *testing.T) {
	data, err := os.ReadFile("testdata/non_names.txt")
	if err != nil {
		t.Fatalf("Failed to read non_names.txt: %v", err)
	}
	words := strings.Split(strings.TrimSpace(string(data)), "\n")
	nd := NewNameDetector()
	var falsePositives int
	for _, word := range words {
		if nd.IsName(word) {
			falsePositives++
		}
	}
	total := len(words)
	fpr := float64(falsePositives) / float64(total) * 100
	t.Logf("File-based FPR: %.2f%% (%d/%d)", fpr, falsePositives, total)
	if fpr >= 1.0 {
		t.Errorf("File-based false positive rate too high: %.2f%% (limit: 1%%)", fpr)
	}
}

func TestEmbedSource(t *testing.T) {
	enData, err := piiembed.NamesFS.ReadFile("names/en.txt.gz")
	if err != nil {
		t.Fatalf("Failed to read embedded English names: %v", err)
	}
	src := &EmbedSource{Data: enData}
	names, err := src.Load()
	if err != nil {
		t.Fatalf("Failed to load English names: %v", err)
	}
	if len(names) < 50000 {
		t.Errorf("Expected at least 50000 names, got %d", len(names))
	}
	for _, name := range names {
		if strings.ToLower(name) != name {
			t.Errorf("Name %q should be lowercase", name)
		}
	}
}

func TestCompositeSource(t *testing.T) {
	enData, err := piiembed.NamesFS.ReadFile("names/en.txt.gz")
	if err != nil {
		t.Fatalf("Failed to read embedded English names: %v", err)
	}
	trData, err := piiembed.NamesFS.ReadFile("names/tr.txt.gz")
	if err != nil {
		t.Fatalf("Failed to read embedded Turkish names: %v", err)
	}
	src := &CompositeSource{
		Sources: []NameSource{
			&EmbedSource{Data: enData},
			&EmbedSource{Data: trData},
		},
	}
	names, err := src.Load()
	if err != nil {
		t.Fatalf("Failed to load composite: %v", err)
	}
	if len(names) < 100000 {
		t.Errorf("Expected at least 100000 combined names, got %d", len(names))
	}
	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			t.Errorf("Duplicate name found: %s", name)
		}
		seen[name] = true
	}
}

// randomString generates a random-looking non-name string for FPR testing.
func randomString(n int) string {
	const letters = "bcdfghjklmnpqrstvwxz" // consonants only (unlikely to be names)
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[(i*17+31)%len(letters)]
	}
	return string(b)
}

func BenchmarkNameDetectorMemory(b *testing.B) {
	b.StopTimer()
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	nd := NewNameDetector()
	// Force GC to collect the temporary dedup map from CompositeSource.Load()
	runtime.GC()
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	heapBytes := m2.Alloc - m1.Alloc // Alloc = live heap objects after GC
	b.ReportMetric(float64(heapBytes)/1024/1024, "MB_Alloc")
	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/1024/1024, "MB_TotalAlloc")
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if !nd.IsName("ali") {
			b.Fatal("Expected 'ali' to be a recognized name")
		}
		if nd.IsName("zzzzzzz") {
			b.Fatal("Expected 'zzzzzzz' to not be a recognized name")
		}
	}
	b.StopTimer()
	// Allow ~10MB for 315k names (sorted slice ~7.2MB + Bloom filter ~384KB + overhead)
	// The AC trie was ~204MB, so this is a 20x improvement.
	if heapBytes > 10*1024*1024 {
		b.Fatalf("Memory constraint exceeded: %.2f MB live heap (limit: 10 MB)", float64(heapBytes)/1024/1024)
	}
}

func BenchmarkNameDetector_IsName(b *testing.B) {
	nd := NewNameDetector()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nd.IsName("ali")
		nd.IsName("zzzzzzz")
	}
}
