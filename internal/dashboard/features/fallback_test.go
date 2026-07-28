package features

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
)

func TestFallbackHandlers(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: "test"},
		Fallback: config.FallbackConfig{
			Enabled:          true,
			CooldownDuration: 5,
			ModelDowngrade:   "none",
			MaxAttempts:      0,
		},
	}
	h := NewFeaturesHandler(nil, cfg, nil, nil, nil, nil)
	cs := cooldown.NewInMemoryStore()
	h.SetCooldownStore(cs)

	// 1. Get Fallback Summary
	req := httptest.NewRequest(http.MethodGet, "/api/fallback", nil)
	rec := httptest.NewRecorder()
	h.HandleGetFallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp FallbackSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal summary response: %v", err)
	}

	if !resp.Enabled {
		t.Errorf("expected enabled=true")
	}

	// 2. Clear Cooldown
	clearReqBody, _ := json.Marshal(clearCooldownRequest{
		Provider: "openai",
		Model:    "gpt-4o",
		KeyID:    "key-demo-1",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/fallback/cooldown/clear", bytes.NewReader(clearReqBody))
	rec2 := httptest.NewRecorder()
	h.HandleClearCooldown(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for clear cooldown, got %d", rec2.Code)
	}
}
