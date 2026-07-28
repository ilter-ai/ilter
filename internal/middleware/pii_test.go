package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/features/pii"
	"github.com/ilter-ai/ilter/internal/model"
)

func init() {
	pii.LoadPatterns(pii.DefaultPIIPatterns)
}

func TestPIIMasker_Handler(t *testing.T) {
	masker := NewPIIMaskerMiddleware(nil, config.PIIConfig{Enabled: true}, nil, nil)

	tests := []struct {
		name          string
		inputContent  string
		expectedMatch string
	}{
		{
			name:          "Credit Card",
			inputContent:  "Here is my card: 4321-0987-6543-2107 please use it.",
			expectedMatch: "Here is my card: <MASKED_PII> please use it.",
		},
		{
			name:          "TCKN",
			inputContent:  "My TC is 50882654334, thanks.",
			expectedMatch: "My TC is <MASKED_PII>, thanks.",
		},
		{
			name:          "US SSN",
			inputContent:  "My SSN number is 123-45-6789.",
			expectedMatch: "My SSN number is <MASKED_PII>.",
		},
		{
			name:          "Email",
			inputContent:  "Contact me at user@example.com for details.",
			expectedMatch: "Contact me at <MASKED_PII> for details.",
		},
		{
			name:          "Turkish Phone",
			inputContent:  "Call me at 05321234567 or +905321234567.",
			expectedMatch: "Call me at <MASKED_PII> or <MASKED_PII>.",
		},
		{
			name:          "IPv4 Address",
			inputContent:  "Local server ip is 192.168.1.100.",
			expectedMatch: "Local server ip is <MASKED_PII>.",
		},
		{
			name:          "No PII",
			inputContent:  "Hello world, how are you?",
			expectedMatch: "Hello world, how are you?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := model.ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []model.Message{
					{Role: "user", Content: tt.inputContent},
				},
			}

			bodyBytes, _ := json.Marshal(reqBody)
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

			var processedBody []byte
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				processedBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			})

			rr := httptest.NewRecorder()
			masker.Handler(nextHandler).ServeHTTP(rr, req)

			var parsedBody model.ChatCompletionRequest
			if err := json.Unmarshal(processedBody, &parsedBody); err != nil {
				t.Fatalf("Failed to parse processed body: %v", err)
			}

			if len(parsedBody.Messages) == 0 {
				t.Fatalf("Messages array is empty")
			}

			content := parsedBody.Messages[0].Content.(string)
			if !strings.Contains(content, tt.expectedMatch) {
				t.Errorf("Expected content to contain %q, but got %q", tt.expectedMatch, content)
			}
		})
	}
}

func TestPIIBlockMode_AllTypes(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "Turkish name from 315k dictionary",
			content: "Benim adım Ahmet.",
		},
		{
			name:    "English name from 315k dictionary",
			content: "My name is John.",
		},
		{
			name:    "Credit Card",
			content: "My card is 4321-0987-6543-2107.",
		},
		{
			name:    "TCKN",
			content: "TCKN: 50882654334",
		},
		{
			name:    "US SSN",
			content: "SSN: 123-45-6789",
		},
		{
			name:    "Email",
			content: "Email: test@example.com",
		},
		{
			name:    "Turkish Phone",
			content: "Phone: 05321234567",
		},
		{
			name:    "IPv4",
			content: "Server: 192.168.1.100",
		},
		{
			name:    "Multiple PII types",
			content: "Ben Ali, email test@example.com, kart 4321-0987-6543-2107",
		},
	}

	cfg := config.PIIConfig{
		Enabled: true,
		Mode:    "block",
	}
	masker := NewPIIMaskerMiddleware(nil, cfg, nil, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := model.ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []model.Message{
					{Role: "user", Content: tt.content},
				},
			}
			bodyBytes, _ := json.Marshal(reqBody)
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

			nextCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			rr := httptest.NewRecorder()
			masker.Handler(nextHandler).ServeHTTP(rr, req)

			if nextCalled {
				t.Fatal("Expected next handler to NOT be called in block mode")
			}

			if rr.Code != http.StatusUnprocessableEntity {
				t.Fatalf("Expected status 422, got %d", rr.Code)
			}

			var errResp struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("Failed to parse error response: %v", err)
			}

			if errResp.Error.Code != "pii_blocked" {
				t.Errorf("Expected error code 'pii_blocked', got %q", errResp.Error.Code)
			}
		})
	}
}

func TestPIIMasker_ReversibleAndBlock(t *testing.T) {
	t.Run("Reversible Mode", func(t *testing.T) {
		cfg := config.PIIConfig{
			Enabled: true,
			Mode:    "reversible",
		}
		masker := NewPIIMaskerMiddleware(nil, cfg, nil, nil)

		reqBody := model.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []model.Message{
				{Role: "user", Content: "Contact me at user@example.com, my name is John."},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

		var processedBody []byte
		var capturedPlaceholders []string

		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			processedBody, _ = io.ReadAll(r.Body)

			var parsedBody model.ChatCompletionRequest
			_ = json.Unmarshal(processedBody, &parsedBody)
			content := parsedBody.Messages[0].Content.(string)

			// Capture placeholders
			for _, word := range strings.Fields(content) {
				if strings.HasPrefix(word, "PII:") {
					capturedPlaceholders = append(capturedPlaceholders, strings.TrimRight(word, ".,"))
				}
			}

			// Respond back with the placeholders to verify they get unmasked on output
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Placeholder values are ` + content + `"}}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`))
		})

		rr := httptest.NewRecorder()
		masker.Handler(nextHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", rr.Code)
		}

		if len(capturedPlaceholders) < 2 {
			t.Errorf("Expected to capture placeholders for name and email, got: %v", capturedPlaceholders)
		}

		respBody := rr.Body.String()
		if strings.Contains(respBody, "PII:") {
			t.Errorf("Expected placeholders to be unmasked in response, but got: %q", respBody)
		}
		if !strings.Contains(respBody, "user@example.com") || !strings.Contains(respBody, "John") {
			t.Errorf("Expected original values user@example.com and John in response, got: %q", respBody)
		}
	})

	t.Run("Block Mode", func(t *testing.T) {
		cfg := config.PIIConfig{
			Enabled: true,
			Mode:    "block",
		}
		masker := NewPIIMaskerMiddleware(nil, cfg, nil, nil)

		reqBody := model.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []model.Message{
				{Role: "user", Content: "Contact me at user@example.com"},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

		nextCalled := false
		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		rr := httptest.NewRecorder()
		masker.Handler(nextHandler).ServeHTTP(rr, req)

		if nextCalled {
			t.Fatal("Expected next handler to not be called in block mode")
		}

		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("Expected status 422, got %d", rr.Code)
		}

		var errResp map[string]map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("Failed to parse error response: %v", err)
		}

		code := errResp["error"]["code"].(string)
		if code != "pii_blocked" {
			t.Errorf("Expected error code 'pii_blocked', got %q", code)
		}
	})
}

func TestPIIMasker_ReversibleStreamingSplit(t *testing.T) {
	cfg := config.PIIConfig{
		Enabled: true,
		Mode:    "reversible",
	}
	masker := NewPIIMaskerMiddleware(nil, cfg, nil, nil)

	reqBody := model.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: "My name is John."},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

	var processedBody []byte
	var placeholder string

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		processedBody, _ = io.ReadAll(r.Body)

		var parsedBody model.ChatCompletionRequest
		_ = json.Unmarshal(processedBody, &parsedBody)
		content := parsedBody.Messages[0].Content.(string)

		// John should have been masked with something like PII:NAMES:abc123
		// Let's capture the placeholder
		for _, word := range strings.Fields(content) {
			if strings.HasPrefix(word, "PII:") {
				placeholder = strings.TrimRight(word, ".,")
				break
			}
		}

		if placeholder == "" {
			t.Fatal("Expected placeholder to be generated for John")
		}

		// We will write the placeholder in two split parts to the response
		// to simulate chunk streaming splitting the placeholder
		mid := len(placeholder) / 2
		part1 := "Hello, my name is " + placeholder[:mid]
		part2 := placeholder[mid:] + "."

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write([]byte(part1))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte(part2))
	})

	rr := httptest.NewRecorder()
	masker.Handler(nextHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	respBody := rr.Body.String()
	expected := "Hello, my name is John."
	if respBody != expected {
		t.Errorf("Expected response body to be %q, but got %q", expected, respBody)
	}
}

func TestPIIResponseUnmask(t *testing.T) {
	cfg := config.PIIConfig{
		Enabled: true,
		Mode:    "reversible",
	}
	masker := NewPIIMaskerMiddleware(nil, cfg, nil, nil)

	t.Run("placeholder unmasked in response", func(t *testing.T) {
		reqBody := model.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []model.Message{
				{Role: "user", Content: "my email is user@example.com"},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

		var placeholder string
		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var parsed model.ChatCompletionRequest
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("Failed to parse masked body: %v", err)
			}
			content := parsed.Messages[0].Content.(string)

			for _, word := range strings.Fields(content) {
				if strings.HasPrefix(word, "PII:") {
					placeholder = strings.TrimRight(word, ".,")
					break
				}
			}
			if placeholder == "" {
				t.Fatal("Expected a placeholder in masked body")
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			response := `{"choices":[{"message":{"content":"Your email is ` + placeholder + `"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`
			_, _ = w.Write([]byte(response))
		})

		rr := httptest.NewRecorder()
		masker.Handler(nextHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", rr.Code)
		}

		respBody := rr.Body.String()
		if strings.Contains(respBody, placeholder) {
			t.Errorf("Response should NOT contain placeholder %q, got: %q", placeholder, respBody)
		}
		if !strings.Contains(respBody, "user@example.com") {
			t.Errorf("Expected original email 'user@example.com' in response, got: %q", respBody)
		}
	})
}

func TestPIIReversibleCrossRequest(t *testing.T) {
	t.Skip("cross-request unmask requires Redis: set pii.redis_url in config")

	cfg := config.PIIConfig{
		Enabled: true,
		Mode:    "reversible",
	}
	masker := NewPIIMaskerMiddleware(nil, cfg, nil, nil)

	// ── Request 1: user message contains an email → PII masks it → cache updated ──
	reqBody1 := model.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: "my email is test@example.com"},
		},
	}
	bodyBytes1, _ := json.Marshal(reqBody1)
	req1, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes1))

	var placeholder string

	nextHandler1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var parsed model.ChatCompletionRequest
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("Request 1: failed to parse masked body: %v", err)
		}
		content := parsed.Messages[0].Content.(string)

		for _, word := range strings.Fields(content) {
			if strings.HasPrefix(word, "PII:") {
				placeholder = strings.TrimRight(word, ".,")
				break
			}
		}

		if placeholder == "" {
			t.Fatal("Request 1: expected a placeholder in masked body, got none")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`))
	})

	rr1 := httptest.NewRecorder()
	masker.Handler(nextHandler1).ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("Request 1: expected 200, got %d", rr1.Code)
	}

	// ── Request 2: no PII in input, but downstream writes the placeholder from request 1 ──
	reqBody2 := model.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: "What was my email again?"},
		},
	}
	bodyBytes2, _ := json.Marshal(reqBody2)
	req2, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes2))

	nextHandler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed model.ChatCompletionRequest
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("Request 2: failed to parse body: %v", err)
		}
		content := parsed.Messages[0].Content.(string)
		if strings.Contains(content, "PII:") {
			t.Errorf("Request 2: body should not contain placeholders (no PII in input), got: %q", content)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := `{"choices":[{"message":{"content":"Your email is ` + placeholder + `"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`
		_, _ = w.Write([]byte(response))
	})

	rr2 := httptest.NewRecorder()
	masker.Handler(nextHandler2).ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("Request 2: expected 200, got %d", rr2.Code)
	}

	// ── Verify: the shared cache unmasked the placeholder back to the original email ──
	respBody := rr2.Body.String()
	if strings.Contains(respBody, placeholder) {
		t.Errorf("Response 2: should NOT contain placeholder %q, got: %q", placeholder, respBody)
	}
	if !strings.Contains(respBody, "test@example.com") {
		t.Errorf("Response 2: expected original email 'test@example.com' to be restored, got: %q", respBody)
	}
	if !strings.Contains(respBody, "Your email is") {
		t.Errorf("Response 2: expected prefix 'Your email is', got: %q", respBody)
	}
}

func TestPIIMaskedEventsPersisted(t *testing.T) {
	store := dbtest.NewFile(t)

	cfg := config.PIIConfig{
		Enabled: true,
		Mode:    "mask",
	}
	masker := NewPIIMaskerMiddleware(store, cfg, nil, nil)

	// Send request with PII that should be masked
	reqBody := model.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: "Contact me at user@example.com, my phone is 05321234567"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.RemoteAddr = "127.0.0.1:12345"

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`))
	})

	rr := httptest.NewRecorder()
	masker.Handler(nextHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	// Verify masked events were persisted to pii_events table
	var count int
	err := store.DB.QueryRow("SELECT COUNT(*) FROM pii_events WHERE action_taken = 'masked'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query pii_events: %v", err)
	}

	if count == 0 {
		t.Fatal("Expected masked PII events to be persisted, but count is 0")
	}

	// Should have at least 2 events (email + phone)
	if count < 2 {
		t.Errorf("Expected at least 2 masked events (email + phone), got %d", count)
	}

	// Verify the structure of persisted events
	rows, err := store.DB.Query("SELECT pii_type, action_taken, masked_prompt_preview, pii_value, client_ip FROM pii_events WHERE action_taken = 'masked'")
	if err != nil {
		t.Fatalf("Failed to query pii_events details: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var piiType, actionTaken, preview, piiValue, clientIP string
		if err := rows.Scan(&piiType, &actionTaken, &preview, &piiValue, &clientIP); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		if actionTaken != "masked" {
			t.Errorf("Expected action_taken='masked', got %q", actionTaken)
		}
		if len(preview) > 200 {
			t.Errorf("Preview should be truncated to 200 chars, got %d", len(preview))
		}
		if piiValue == "" {
			t.Error("pii_value should not be empty for masked events")
		}
		if clientIP == "" {
			t.Error("client_ip should not be empty")
		}
	}
}

func TestPIIPhoneUnmask(t *testing.T) {
	cfg := config.PIIConfig{
		Enabled: true,
		Mode:    "reversible",
	}
	masker := NewPIIMaskerMiddleware(nil, cfg, nil, nil)

	phone := "05321234567"
	reqBody := model.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: "Call me at " + phone},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

	var placeholder string
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var parsed model.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&parsed); err != nil {
			t.Fatalf("decode: %v", err)
		}
		content := parsed.Messages[0].Content.(string)

		for _, word := range strings.Fields(content) {
			if strings.HasPrefix(word, "PII:") {
				placeholder = strings.TrimRight(word, ".,")
				break
			}
		}
		if placeholder == "" {
			t.Fatal("expected placeholder for phone number")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Calling ` + placeholder + ` now"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`))
	})

	rr := httptest.NewRecorder()
	masker.Handler(nextHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	respBody := rr.Body.String()
	if strings.Contains(respBody, placeholder) {
		t.Errorf("response should NOT contain placeholder %q, got: %q", placeholder, respBody)
	}
	if !strings.Contains(respBody, phone) {
		t.Errorf("response should contain original phone %q, got: %q", phone, respBody)
	}
}
