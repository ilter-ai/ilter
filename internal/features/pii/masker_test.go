package pii

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/model"
)

func TestMain(m *testing.M) {
	LoadPatterns(DefaultPIIPatterns)
	os.Exit(m.Run())
}

func TestNameDetectorWithDefaultNames(t *testing.T) {
	nd := NewNameDetector()

	if !nd.IsName("Ali") {
		t.Error("Expected 'Ali' to be recognized as a name")
	}
	if !nd.IsName("john") {
		t.Error("Expected 'john' to be recognized as a name")
	}
	if nd.IsName("RandomNonNameString") {
		t.Error("Expected 'RandomNonNameString' to not be recognized as a name")
	}
}

func TestMaskerModes(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		input        string
		expectedErr  error
		verifyOutput func(t *testing.T, got string, state *ReversibleState)
	}{
		{
			name:        "Mask Mode - Email & CC",
			mode:        "mask",
			input:       "Contact me at ali@example.com or use card 4321098765432107.", // Note: 4321098765432107 is a valid Luhn
			expectedErr: nil,
			verifyOutput: func(t *testing.T, got string, _ *ReversibleState) {
				if !strings.Contains(got, "<MASKED_PII>") {
					t.Errorf("Expected masked output, got: %q", got)
				}
				if strings.Contains(got, "ali@example.com") {
					t.Error("Email was not masked")
				}
			},
		},
		{
			name:        "Block Mode - PII Present",
			mode:        "block",
			input:       "Hey, email is john@example.com",
			expectedErr: model.ErrPIIBlocked,
		},
		{
			name:        "Block Mode - No PII",
			mode:        "block",
			input:       "Hello there, how are you today?",
			expectedErr: nil,
			verifyOutput: func(t *testing.T, got string, _ *ReversibleState) {
				if got != "Hello there, how are you today?" {
					t.Errorf("Expected unchanged string, got: %q", got)
				}
			},
		},
		{
			name:        "Reversible Mode",
			mode:        "reversible",
			input:       "My name is John. Email is john@example.com.",
			expectedErr: nil,
			verifyOutput: func(t *testing.T, got string, state *ReversibleState) {
				if !strings.Contains(got, "PII:") {
					t.Errorf("Expected placeholder in reversible mode, got: %q", got)
				}
				if strings.Contains(got, "John") || strings.Contains(got, "john@example.com") {
					t.Error("Sensitive values leaked in output")
				}
				// Verify mapping populated
				state.mu.Lock()
				mappingSize := len(state.mappings)
				state.mu.Unlock()
				if mappingSize != 2 {
					t.Errorf("Expected 2 placeholders in mapping, got %d", mappingSize)
				}

				// Unmask and verify
				masker := NewMasker("reversible", nil)
				unmasked := masker.Unmask(got, state)
				expected := "My name is John. Email is john@example.com."
				if unmasked != expected {
					t.Errorf("Expected unmasked string to be %q, got %q", expected, unmasked)
				}
			},
		},
		{
			name:        "Names with and without context",
			mode:        "mask",
			input:       "Hello Ali. My name is Ali. Sayın Ayşe hanım.",
			expectedErr: nil,
			verifyOutput: func(t *testing.T, got string, _ *ReversibleState) {
				// "Hello Ali" should not be masked (no name context)
				// "My name is Ali" should be masked ("my name is" context)
				// "Sayın Ayşe" should be masked ("sayın" context)
				if !strings.Contains(got, "Hello Ali") {
					t.Errorf("Expected 'Hello Ali' to be unmasked, got: %q", got)
				}
				if !strings.Contains(got, "My name is <MASKED_PII>") {
					t.Errorf("Expected 'My name is Ali' to be masked, got: %q", got)
				}
				if !strings.Contains(got, "Sayın <MASKED_PII> hanım") {
					t.Errorf("Expected 'Sayın Ayşe' to be masked, got: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masker := NewMasker(tt.mode, nil)
			state := NewReversibleState()
			defer state.Clear()

			got, err := masker.ProcessText(tt.input, state)
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("Expected error %v, got %v", tt.expectedErr, err)
			}

			if tt.verifyOutput != nil {
				tt.verifyOutput(t, got, state)
			}
		})
	}
}

func TestLuhnAndTurkishID(t *testing.T) {
	// Valid Luhn check
	if !isValidLuhn("4321098765432107") {
		t.Error("Expected 4321098765432107 to be a valid Luhn credit card")
	}
	if isValidLuhn("1111111111111111") {
		t.Error("Expected 1111111111111111 to be invalid Luhn")
	}

	// Valid Turkish ID check (using a valid simulated algorithm-compliant TC No: 50882654334)
	if !isValidTurkishID("50882654334") {
		t.Error("Expected 50882654334 to be a valid Turkish ID")
	}
	if isValidTurkishID("00000000000") {
		t.Error("Expected 00000000000 to be invalid (cannot start with 0)")
	}
}

func TestLuhnValid(t *testing.T) {
	// Test known-valid Luhn numbers
	validCards := []string{
		"4111111111111111", // Visa test number
		"5500000000000004", // Mastercard test number
		"4321098765432107", // From existing test
		"378282246310005",  // American Express test (15 digits)
	}
	for _, cc := range validCards {
		if !isValidLuhn(cc) {
			t.Errorf("expected %s to be a valid Luhn number", cc)
		}
	}
}

func TestLuhnInvalid(t *testing.T) {
	invalidCards := []string{
		"1111111111111111", // All ones
		"1234567890123456", // Common invalid
		"1234567890123457", // Fails Luhn check
		"4111111111111112", // One digit off from valid
	}
	for _, cc := range invalidCards {
		if isValidLuhn(cc) {
			t.Errorf("expected %s to be an invalid Luhn number", cc)
		}
	}
}

func TestCreditCardWithLuhn(t *testing.T) {
	masker := NewMasker("mask", nil)

	// Valid card should be masked
	out, err := masker.ProcessText("My card is 4111111111111111", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<MASKED_PII>") {
		t.Errorf("expected valid card to be masked, got %q", out)
	}

	// Invalid card should NOT be masked
	out, err = masker.ProcessText("My card is 1111111111111111", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<MASKED_PII>") {
		t.Errorf("expected invalid card to NOT be masked, got %q", out)
	}
	if !strings.Contains(out, "1111111111111111") {
		t.Errorf("expected invalid card number to remain in output, got %q", out)
	}
}

func TestCreditCardEdgeCases(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		if isValidLuhn("") {
			t.Error("empty string should not be valid Luhn")
		}
	})

	t.Run("short numbers", func(t *testing.T) {
		if isValidLuhn("1234") {
			t.Error("expected 4-digit number to be invalid Luhn")
		}
		if isValidLuhn("123456789012") { // 12 digits
			t.Error("expected 12-digit number to be invalid Luhn")
		}
	})

	t.Run("non-digit characters are skipped", func(t *testing.T) {
		// 4111-1111-1111-1111 is valid Luhn (same digits as Visa test)
		if !isValidLuhn("4111-1111-1111-1111") {
			t.Error("expected 4111-1111-1111-1111 to be valid Luhn (digits match valid card)")
		}
	})
}

func TestUnmask_ReversiblePlaceholder(t *testing.T) {
	masker := NewMasker("reversible", nil)
	placeholder := "PII:EMAIL:abc123"
	original := "test@example.com"

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Basic placeholder",
			input:    "The email is PII:EMAIL:abc123.",
			expected: "The email is test@example.com.",
		},
		{
			name:     "No placeholder - unchanged",
			input:    "This string has no placeholders at all.",
			expected: "This string has no placeholders at all.",
		},
		{
			name:     "Multiple placeholders of different types",
			input:    "email: PII:EMAIL:abc123, name: PII:NAMES:def456",
			expected: "email: test@example.com, name: John Doe",
		},
	}

	state := NewReversibleState()
	defer state.Clear()
	state.mu.Lock()
	state.mappings[placeholder] = original
	state.mappings["PII:NAMES:def456"] = "John Doe"
	state.mu.Unlock()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := masker.Unmask(tt.input, state)
			if got != tt.expected {
				t.Errorf("Unmask() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestUnmask_EdgeCases(t *testing.T) {
	masker := NewMasker("reversible", nil)

	t.Run("empty string", func(t *testing.T) {
		state := NewReversibleState()
		defer state.Clear()
		state.mu.Lock()
		state.mappings["PII:EMAIL:abc"] = "test@example.com"
		state.mu.Unlock()
		got := masker.Unmask("", state)
		if got != "" {
			t.Errorf("Unmask(\"\") = %q, want empty string", got)
		}
	})

	t.Run("nil state returns text unchanged", func(t *testing.T) {
		got := masker.Unmask("some text PII:EMAIL:abc", nil)
		if got != "some text PII:EMAIL:abc" {
			t.Errorf("Unmask with nil state should return original, got %q", got)
		}
	})

	t.Run("no placeholder match leaves text unchanged", func(t *testing.T) {
		state := NewReversibleState()
		defer state.Clear()
		state.mu.Lock()
		state.mappings["PII:EMAIL:abc"] = "test@example.com"
		state.mu.Unlock()
		input := "This text has no matching placeholder here."
		got := masker.Unmask(input, state)
		if got != input {
			t.Errorf("Unmask() = %q, want %q", got, input)
		}
	})

	t.Run("multiple non-overlapping placeholders unmasked correctly", func(t *testing.T) {
		state := NewReversibleState()
		defer state.Clear()
		state.mu.Lock()
		state.mappings["PII:EMAIL:ab"] = "a@example.com"
		state.mappings["PII:EMAIL:cd"] = "b@example.com"
		state.mu.Unlock()
		input := "Emails: PII:EMAIL:ab and PII:EMAIL:cd"
		expected := "Emails: a@example.com and b@example.com"
		got := masker.Unmask(input, state)
		if got != expected {
			t.Errorf("Unmask() = %q, want %q", got, expected)
		}
	})

	t.Run("special characters in original value", func(t *testing.T) {
		state := NewReversibleState()
		defer state.Clear()
		state.mu.Lock()
		state.mappings["PII:EMAIL:dot"] = "test+alias@example.com"
		state.mappings["PII:EMAIL:star"] = "user*name@example.org"
		state.mu.Unlock()
		input := "Contact: PII:EMAIL:dot or PII:EMAIL:star"
		expected := "Contact: test+alias@example.com or user*name@example.org"
		got := masker.Unmask(input, state)
		if got != expected {
			t.Errorf("Unmask() with special chars = %q, want %q", got, expected)
		}
	})

	t.Run("double unmask is idempotent", func(t *testing.T) {
		state := NewReversibleState()
		defer state.Clear()
		state.mu.Lock()
		state.mappings["PII:EMAIL:abc"] = "user@example.com"
		state.mu.Unlock()
		input := "Email: PII:EMAIL:abc"
		first := masker.Unmask(input, state)
		second := masker.Unmask(first, state)
		if second != first {
			t.Errorf("Double unmask not idempotent: first=%q, second=%q", first, second)
		}
		if second != "Email: user@example.com" {
			t.Errorf("Unmask() = %q, want %q", second, "Email: user@example.com")
		}
	})
}

// TestReDoSProtection verifies Go's regexp (RE2) does not fall into
// catastrophic backtracking on a pattern that would hang backtracking engines
// (PCRE, Python re, etc). RE2 guarantees O(n) matching regardless of pattern
// shape, so this must complete almost instantly even against the classic
// "(a+)+b" adversarial input — no per-call timeout/goroutine wrapper needed,
// and none should be reintroduced (it cannot cancel a running match; it can
// only abandon a goroutine that keeps burning CPU in the background).
func TestReDoSProtection(t *testing.T) {
	re := regexp.MustCompile(`(a+)+b`)
	input := strings.Repeat("a", maxPIIScanSize/2) + "c"

	done := make(chan struct{})
	go func() {
		re.FindAllStringIndex(input, -1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RE2 match did not complete within 2s — catastrophic backtracking regression")
	}
}

func TestRegexTimeoutFailOpen(t *testing.T) {
	masker := NewMasker("mask", nil)

	out, err := masker.ProcessText("Hello, this is a normal message with no PII.", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if out != "Hello, this is a normal message with no PII." {
		t.Errorf("expected unchanged text, got %q", out)
	}
}

func TestPIIFalsePositiveRate(t *testing.T) {
	masker := NewMasker("mask", nil)

	tests := []struct {
		name string
		text string
	}{
		{name: "en_greeting", text: "Hello, how are you today?"},
		{name: "en_weather", text: "What is the weather like in Istanbul?"},
		{name: "en_homework", text: "I need help with my homework."},
		{name: "en_explain", text: "Can you explain quantum computing?"},
		{name: "en_poem", text: "Write a poem about spring."},
		{name: "en_joke", text: "Tell me a joke about programming."},
		{name: "en_cooking", text: "How do I cook pasta?"},
		{name: "en_capital", text: "What is the capital of France?"},
		{name: "en_translate", text: "Translate hello to Spanish."},
		{name: "en_tips", text: "Give me three tips for public speaking."},
		{name: "en_book", text: "The book was very interesting."},
		{name: "en_movie", text: "I enjoyed the movie last night."},
		{name: "en_meeting", text: "The meeting is at 3 PM."},
		{name: "en_document", text: "Please send the document."},
		{name: "en_thanks", text: "Thank you for your help."},
		{name: "en_deadline", text: "The project deadline is next Friday."},
		{name: "en_budget", text: "We need to discuss the budget."},
		{name: "en_system", text: "The system is working correctly."},
		{name: "en_review", text: "Please review the attached file."},
		{name: "en_questions", text: "Let me know if you have questions."},
		{name: "en_opinion", text: "What do you think about the new design?"},
		{name: "en_summary", text: "Can you summarize this article for me?"},
		{name: "en_recipe", text: "I need a recipe for chocolate cake."},
		{name: "en_travel", text: "I am planning a trip to Japan."},
		{name: "en_learning", text: "I want to learn Go programming language."},
		{name: "tr_greeting", text: "Merhaba, bugün nasılsınız?"},
		{name: "tr_weather", text: "Hava çok güzel bugün."},
		{name: "tr_deadline", text: "Proje teslim tarihi önümüzdeki hafta."},
		{name: "tr_thanks", text: "Yardımınız için teşekkür ederim."},
		{name: "tr_meeting", text: "Toplantı saat 14:00'te."},
		{name: "tr_discuss", text: "Bu konuyu daha sonra konuşalım."},
		{name: "tr_report", text: "Raporu hazırladım kontrol eder misiniz?"},
		{name: "tr_system", text: "Sistem şu anda çalışıyor."},
		{name: "tr_sign", text: "Lütfen belgeyi imzalayın."},
		{name: "tr_homework", text: "Ödevimde bana yardımcı olur musunuz?"},
		{name: "tr_movie", text: "Dün gece çok güzel bir film izledim."},
		{name: "tr_music", text: "Türk müziğini çok seviyorum."},
		{name: "tr_food", text: "Bugün akşam yemeğinde makarna var."},
		{name: "tr_holiday", text: "Bayram tatilinde memlekete gideceğim."},
		{name: "tr_book", text: "Bu kitabı okumak için sabırsızlanıyorum."},
		{name: "code_variable", text: "The variable count is set to 100."},
		{name: "code_function", text: "Function getUserById returns a user object."},
		{name: "code_error", text: "Error code 404 Not Found"},
		{name: "code_email_field", text: "The emailAddress field is required."},
		{name: "code_api_key", text: "const API_KEY = sk-abc123def456"},
		{name: "code_query", text: "db.query SELECT star FROM users"},
		{name: "code_commit", text: "git commit -m fix resolve issue with login"},
		{name: "code_install", text: "npm install react-router-dom latest"},
		{name: "code_regex", text: "The regex pattern matches three digits then a dash"},
		{name: "code_port", text: "The port number is 8080."},
		{name: "code_port_equals", text: "port=70000"},
		{name: "code_port_colon", text: "port: 70000"},
		{name: "code_status", text: "Response status 200 OK"},
		{name: "code_version", text: "Version 2.5.1 is now available."},
		{name: "code_build", text: "The build was completed successfully."},
		{name: "code_revision", text: "Revision r123456 is the latest."},
		{name: "code_txn", text: "Transaction ID TXN-2024-001"},
		{name: "code_session", text: "Session token sess_abc123def"},
		{name: "code_class", text: "The UserService class handles user operations."},
		{name: "code_config", text: "Set environment variable DEBUG to true."},
		{name: "code_package", text: "Import the time package for duration handling."},
		{name: "code_log", text: "Log level set to info for production."},
		{name: "code_cache", text: "The cache TTL is set to 300 seconds."},
		{name: "date_iso", text: "The date is 2024-01-15."},
		{name: "date_eu", text: "Today is 15 slash 01 slash 2024."},
		{name: "time_24h", text: "The time is 14:30:00."},
		{name: "version_major", text: "Version 3 dot 0 is released."},
		{name: "build_tag", text: "Build 2024.03.15.001"},
		{name: "time_range", text: "Time range 09:00 to 17:00"},
		{name: "ref_code", text: "Reference REF-2024-001"},
		{name: "order_num", text: "Order number ORD-123-456"},
		{name: "invoice_num", text: "Invoice number INV-2024-001"},
		{name: "flight_num", text: "Flight number TK1234"},
		{name: "room_num", text: "Room number 1234"},
		{name: "postal_code", text: "Postal code formats vary by country."},
		{name: "year_only", text: "The year is 2024."},
		{name: "event_time", text: "The event starts at 8 PM."},
		{name: "chapter", text: "Chapter 5 verse 12."},
		{name: "uuid", text: "UUID 550e8400-e29b-41d4-a716-446655440000"},
		{name: "hash", text: "Hash a1b2c3d4e5f6a7b8c9d0e1f2"},
		{name: "commit_hash", text: "Commit 8d3a9b2c1e5f7a0b"},
		{name: "mac", text: "Mac address 00 colon 1a colon 2b colon 3c colon 4d colon 5e"},
		{name: "hex_value", text: "The hex value is 0xFFAABBCC"},
		{name: "base64", text: "Base64 encoded string appears here"},
		{name: "long_number", text: "The long number is 12345678901234567890."},
		{name: "ref_code_alpha", text: "Reference code A1B2C3D4E5"},
		{name: "jwt_like", text: "Token header.payload.signature"},
		{name: "account_num", text: "Account number ACCT-1234-5678"},
		{name: "tracking", text: "Tracking number 1Z999AA10123456784"},
		{name: "serial", text: "Serial number SN-2024-ABC-001"},
		{name: "hex_color", text: "The background color is FFFFFF."},
		{name: "phone_ext", text: "Dial extension five four three two one."},
		{name: "error_id", text: "Error ID ERR-2024-10-15-001"},
		{name: "edge_dash_numbers", text: "The values are 100-200-3000 and 500-10-200."},
		{name: "edge_iban_like", text: "The IBAN format starts with country code and digits."},
		{name: "edge_vin_like", text: "VIN numbers are 17 characters excluding I O and Q."},
		{name: "edge_zip_like", text: "ZIP codes are geographic postal routing codes."},
		{name: "edge_plate_like", text: "License plates vary by country."},
		{name: "edge_ein_like", text: "The employer ID format is two digits dash seven digits."},
		{name: "edge_itin_like", text: "ITIN numbers start with digit 9."},
		{name: "edge_code_snippet", text: "For i := 0; i less than 10; i plus plus"},
		{name: "edge_coordinates", text: "The GPS coordinates are 41.0082 N 28.9784 E."},
		{name: "edge_temperature", text: "The temperature is 23.5 degrees Celsius."},
		{name: "edge_price", text: "The total price is 149.99 dollars."},
		{name: "edge_percentage", text: "The success rate is 99.9 percent."},
		{name: "edge_math", text: "The sum of 12 345 and 67 890 is 80 235."},
		{name: "edge_phone_word", text: "Please contact our support team for assistance."},
		{name: "edge_no_pii", text: "This is a completely normal sentence without any sensitive data."},
	}

	var falsePositives []string
	for _, tt := range tests {
		state := NewReversibleState()
		result, err := masker.ProcessText(tt.text, state)
		state.Clear()
		if err != nil {
			t.Errorf("Unexpected error for %q: %v", tt.text, err)
			continue
		}
		if result != tt.text {
			falsePositives = append(falsePositives, fmt.Sprintf("  %s: input=%q output=%q", tt.name, tt.text, result))
		}
	}

	rate := float64(len(falsePositives)) / float64(len(tests)) * 100
	t.Logf("False positive rate: %.2f%% (%d/%d)", rate, len(falsePositives), len(tests))
	for _, fp := range falsePositives {
		t.Log(fp)
	}

	if rate >= 5.0 {
		t.Errorf("False positive rate %.2f%% exceeds 5%% threshold (max 5%%)", rate)
	}
}

func BenchmarkPIIMasker_ProcessText(b *testing.B) {
	masker := NewMasker("reversible", nil)
	state := NewReversibleState()
	defer state.Clear()
	text := "My name is John, my email is john@example.com, and my card is 4321-0987-6543-2107."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = masker.ProcessText(text, state)
		state.Clear()
	}
}
