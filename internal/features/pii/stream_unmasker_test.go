package pii

import (
	"strings"
	"testing"
)

func TestStreamUnmasker(t *testing.T) {
	mappings := map[string]string{
		"PII:EMAIL:a649c2":      "jane@example.com",
		"PII:TURKISH_ID:8e3f1a": "50882654334",
	}

	tests := []struct {
		name     string
		chunks   []string
		expected string
	}{
		{
			name: "No placeholders",
			chunks: []string{
				"Hello ",
				"world!",
			},
			expected: "Hello world!",
		},
		{
			name: "Direct replacement",
			chunks: []string{
				"My email is ",
				"PII:EMAIL:a649c2",
				" and my phone is...",
			},
			expected: "My email is jane@example.com and my phone is...",
		},
		{
			name: "Colons converted to underscores and casing changes",
			chunks: []string{
				"Mail: ",
				"pii_email_a649c2",
			},
			expected: "Mail: jane@example.com",
		},
		{
			name: "Type name translated to synonym (TCKN)",
			chunks: []string{
				"TC: ",
				"PII_TCKN_8e3f1a",
			},
			expected: "TC: 50882654334",
		},
		{
			name: "Split across multiple chunks",
			chunks: []string{
				"My email is <PII_",
				"EMAIL_",
				"a649c2>",
			},
			expected: "My email is <jane@example.com>",
		},
		{
			name: "Very fragmented split",
			chunks: []string{
				"TC: ",
				"P",
				"I",
				"I",
				"_",
				"T",
				"C",
				"K",
				"N",
				"_",
				"8",
				"e",
				"3",
				"f",
				"1",
				"a",
				"!",
			},
			expected: "TC: 50882654334!",
		},
		{
			name: "Normal word starting with P",
			chunks: []string{
				"Please ",
				"project ",
				"plans.",
			},
			expected: "Please project plans.",
		},
		{
			name: "Mixed plain text and split placeholder",
			chunks: []string{
				"Send to P",
				"II:EMAIL:a6",
				"49c2",
				" please.",
			},
			expected: "Send to jane@example.com please.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unmasker := NewStreamUnmasker(mappings)
			var sb strings.Builder
			for _, chunk := range tt.chunks {
				sb.WriteString(unmasker.Process(chunk))
			}
			sb.WriteString(unmasker.Flush())

			got := sb.String()
			if got != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, got)
			}
		})
	}
}
