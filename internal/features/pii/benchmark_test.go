package pii

import (
	"math"
	"strings"
	"testing"
)

func BenchmarkPIIMasker_MaskMode(b *testing.B) {
	masker := NewMasker("mask", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := "My email is john@example.com and my phone is +1-555-1234."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_BlockMode(b *testing.B) {
	masker := NewMasker("block", []string{"email", "phone"})
	state := NewReversibleState()
	defer state.Clear()
	text := "My email is john@example.com and my phone is +1-555-1234."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_LongText(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()

	var parts []string
	for range 50 {
		parts = append(parts, "This is a paragraph without any PII data. Just regular text content.")
	}
	parts = append(parts, "But here is an email: test@example.com")
	text := strings.Join(parts, "\n\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_NoPII(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := "This is a completely clean text with no personal information whatsoever."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_MultipleEmails(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := "Contact us at support@company.com, sales@company.com, or info@company.com. For emergencies: alert@company.com."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_CreditCard(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := "My visa is 4111-1111-1111-1111 and my mastercard is 5500-0000-0000-0004."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_TurkishID(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := "Benim TC kimlik numaram 12345678901 ve adresim İstanbul."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_AllPIITypes(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := `Contact: john.doe@email.com | Phone: +90-532-123-4567
Card: 4532-1234-5678-9012 | TCKN: 12345678901
SSN: 123-45-6789 | IP: 192.168.1.1
Name: Ahmet Yılmaz`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}

func BenchmarkPIIMasker_Unmask(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := "My email is test@example.com and my card is 4111-1111-1111-1111."
	masked, _ := masker.ProcessText(text, state)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = masker.Unmask(masked, state)
	}
}

func BenchmarkPIIPatternDetection_Email(b *testing.B) {
	text := "Contact: john.doe@company.com, jane.smith@domain.org, support@service.net"
	var count int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count = 0
		for _, pattern := range LoadedPatterns {
			if pattern.Name == "email" {
				matches := pattern.Re.FindAllString(text, -1)
				count = len(matches)
			}
		}
	}
	_ = count
}

func BenchmarkPIIPatternDetection_Phone(b *testing.B) {
	text := "Call us at +1-555-123-4567, +90-532-123-45-67, or +44-20-7946-0958"
	var count int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count = 0
		for _, pattern := range LoadedPatterns {
			if pattern.Name == "phone" {
				matches := pattern.Re.FindAllString(text, -1)
				count = len(matches)
			}
		}
	}
	_ = count
}

func BenchmarkMasker_DetectPII(b *testing.B) {
	masker := NewMasker("reversible", nil)
	text := "My email is john@example.com and my card is 4321-0987-6543-2107."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = masker.DetectPII(text)
	}
}

func BenchmarkPIIReversibleState_Clear(b *testing.B) {
	state := NewReversibleState()
	state.mappings["PII:EMAIL:ABC123"] = "test@example.com"
	state.mappings["PII:PHONE:DEF456"] = "+1-555-1234"
	state.mappings["PII:CC:789ABC"] = "4111-1111-1111-1111"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.Clear()
		state.mappings["PII:EMAIL:ABC123"] = "test@example.com"
		state.mappings["PII:PHONE:DEF456"] = "+1-555-1234"
		state.mappings["PII:CC:789ABC"] = "4111-1111-1111-1111"
	}
}

func BenchmarkIsValidLuhnEdgeCases(b *testing.B) {
	cases := []string{
		"0",
		"0000000000000000",
		"1234567890123456",
		"378282246310005",
		"6011111111111117",
		"5555555555554444",
		"",
		"abcd",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range cases {
			isValidLuhn(c)
		}
	}
}

func BenchmarkNameDetector_MisspelledName(b *testing.B) {
	nd := NewNameDetector()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nd.IsName("alii")
		nd.IsName("mohammd")
	}
}

func BenchmarkNameDetector_BulkLookup(b *testing.B) {
	nd := NewNameDetector()
	names := []string{"ali", "ahmet", "mehmet", "ayşe", "fatma", "john", "jane", "michael", "sarah", "david"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			nd.IsName(name)
		}
	}
}

func BenchmarkPipelineThroughput(b *testing.B) {
	masker := NewMasker("reversible", nil)
	text := "My name is John Doe, email is john@example.com, phone is +1-555-123-4567."
	b.ResetTimer()

	totalOps := 0
	for i := 0; i < b.N; i++ {
		state := NewReversibleState()
		masked, err := masker.ProcessText(text, state)
		if err != nil {
			b.Fatal(err)
		}
		_ = masker.Unmask(masked, state)
		state.Clear()
		totalOps += 2
	}

	b.ReportMetric(float64(totalOps)/b.Elapsed().Seconds(), "ops/sec")
}

func BenchmarkReversibleState_GetMappings(b *testing.B) {
	state := NewReversibleState()
	for i := range 100 {
		ph := "PII:EMAIL:" + strings.ToUpper(string([]byte{byte(i%6 + 65), byte(i%6 + 66), byte(i%6 + 67)}))
		state.mappings[ph] = "test" + strings.Repeat("x", i%10) + "@example.com"
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = state.GetMappings()
	}
}

func BenchmarkMasker_ProcessText_ParallelSimulated(b *testing.B) {
	masker := NewMasker("reversible", nil)
	texts := []string{
		"Email: user@test.com",
		"Card: 4111-1111-1111-1111",
		"Phone: +90-555-123-45-67",
		"Clean text without PII",
		"IP: 192.168.1.1 and email: admin@localhost",
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			state := NewReversibleState()
			for _, txt := range texts {
				_, _ = masker.ProcessText(txt, state)
				state.Clear()
			}
		}
	})
}

func BenchmarkMemoryOverhead_PIIState(b *testing.B) {
	text := "Email: user@test.com, Phone: +1-555-1234, Card: 4111-1111-1111-1111, Name: John"
	masker := NewMasker("reversible", nil)
	b.ResetTimer()

	var totalAlloc uint64
	for i := 0; i < b.N; i++ {
		state := NewReversibleState()
		_, _ = masker.ProcessText(text, state)
		totalAlloc += uint64(len(state.mappings))
		state.Clear()
	}
	b.ReportMetric(math.Round(float64(totalAlloc)/float64(b.N)), "avg_entries")
}
