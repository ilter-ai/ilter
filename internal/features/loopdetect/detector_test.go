package loopdetect

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

func TestNewDetectorDefaults(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{})
	if d.settings.RateThreshold != 30 {
		t.Errorf("expected RateThreshold 30, got %d", d.settings.RateThreshold)
	}
	if d.settings.FingerprintWindow != 20 {
		t.Errorf("expected FingerprintWindow 20, got %d", d.settings.FingerprintWindow)
	}
	if d.settings.FingerprintDuplicates != 5 {
		t.Errorf("expected FingerprintDuplicates 5, got %d", d.settings.FingerprintDuplicates)
	}
	if d.settings.CostWindow != 5*time.Minute {
		t.Errorf("expected CostWindow 5m, got %v", d.settings.CostWindow)
	}
	if d.settings.CostThreshold != 5.0 {
		t.Errorf("expected CostThreshold 5.0, got %f", d.settings.CostThreshold)
	}
	if d.settings.SessionMaxRequests != 100 {
		t.Errorf("expected SessionMaxRequests 100, got %d", d.settings.SessionMaxRequests)
	}
}

func TestSignalRateAnomaly(t *testing.T) {
	// Set low threshold to test
	d := NewDetector(config.LoopSettingsConfig{
		RateThreshold: 3,
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	messages := []model.Message{{Role: "user", Content: "hello"}}
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// First request: 1 active request, threshold is 3.
	res, err := d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// Second request: 2 active requests.
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// Third request: 3 active requests. >= 3 threshold is hit.
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 1 {
		t.Errorf("expected 1 active signal (rate), got %d", res.ActiveSignals)
	}
	if !res.Warning {
		t.Error("expected warning to be true")
	}
}

func TestSignalFingerprintRepetition(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{
		FingerprintWindow:     5,
		FingerprintDuplicates: 3,
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	messages := []model.Message{{Role: "user", Content: "repeated content"}}
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// Request 1
	res, err := d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// Request 2
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// Request 3: 3rd duplicate, triggers duplicate fingerprint (threshold 3)
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 1 {
		t.Errorf("expected 1 active signal, got %d", res.ActiveSignals)
	}
}

func TestSignalCostAccumulator(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{
		CostThreshold: 2.0,
		CostWindow:    1 * time.Second,
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// Check should be clean
	res, err := d.Check("1", sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// Record some cost below threshold
	d.RecordCost("1", 1.5)
	res, err = d.Check("1", sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// Record more cost to exceed threshold
	d.RecordCost("1", 1.0) // Total = 2.5
	res, err = d.Check("1", sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 1 {
		t.Errorf("expected 1 active signal (cost), got %d", res.ActiveSignals)
	}

	// Wait for window to expire
	time.Sleep(1100 * time.Millisecond)
	res, err = d.Check("1", sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals after expiry, got %d", res.ActiveSignals)
	}
}

func TestSignalSessionCounter(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{
		SessionMaxRequests: 2,
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set("X-Ilter-Session-Id", "session-123")
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// 1st request
	res, err := d.Check("1", sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// 2nd request
	res, err = d.Check("1", sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 0 {
		t.Errorf("expected 0 active signals, got %d", res.ActiveSignals)
	}

	// 3rd request (exceeds max 2 requests per session)
	res, err = d.Check("1", sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 1 {
		t.Errorf("expected 1 active signal (session), got %d", res.ActiveSignals)
	}
}

func TestProgressiveDelay_ExponentialBackoff(t *testing.T) {
	// Set up detector so that 2 signals (Rate + Fingerprint) are active on every request
	d := NewDetector(config.LoopSettingsConfig{
		RateThreshold:         1, // every request triggers rate signal
		FingerprintWindow:     10,
		FingerprintDuplicates: 1, // every request triggers fingerprint signal
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	messages := []model.Message{{Role: "user", Content: "same content"}}
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	expectedDelays := []time.Duration{
		1 * time.Second,  // 1st delay: 1 << 0 = 1
		2 * time.Second,  // 2nd delay: 1 << 1 = 2
		4 * time.Second,  // 3rd delay: 1 << 2 = 4
		8 * time.Second,  // 4th delay: 1 << 3 = 8
		16 * time.Second, // 5th delay: 1 << 4 = 16 (cap)
		16 * time.Second, // 6th delay: capped at 16
	}

	for i, expected := range expectedDelays {
		res, err := d.Check("1", sessionID, messages)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if res.ActiveSignals != 2 {
			t.Fatalf("request %d: expected 2 active signals, got %d", i, res.ActiveSignals)
		}
		if res.Delay != expected {
			t.Errorf("request %d: expected delay %v, got %v", i, expected, res.Delay)
		}
		if res.Blocked {
			t.Errorf("request %d: expected not blocked (2 signals)", i)
		}
	}
}

func TestProgressiveDelay_Reset(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{
		RateThreshold:         1,
		FingerprintWindow:     10,
		FingerprintDuplicates: 2, // need 2 occurrences to trigger
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	messages := []model.Message{{Role: "user", Content: "same content"}}
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// First request: rate fires (≥1), fingerprint: 1st occurrence, 1≥2=false → 1 signal only (no delay)
	res, err := d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 1 {
		t.Fatalf("expected 1 active signal (rate only), got %d", res.ActiveSignals)
	}

	// Second request with same content: rate fires, fingerprint: 2nd occurrence, 2≥2=true → 2 signals
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 2 {
		t.Fatalf("expected 2 active signals, got %d", res.ActiveSignals)
	}
	if res.Delay != 1*time.Second {
		t.Errorf("expected delay 1s, got %v", res.Delay)
	}

	// Third request with same content: 2 signals → delay = 2s (progressive)
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.Delay != 2*time.Second {
		t.Errorf("expected delay 2s, got %v", res.Delay)
	}

	// Now send a request with different content - fingerprint won't match (only 1 occurrence of new hash),
	// so only rate signal is active (activeSignals = 1 < 2 → reset delayCount)
	differentMessages := []model.Message{{Role: "user", Content: "different content"}}
	res, err = d.Check("1", sessionID, differentMessages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 1 {
		t.Fatalf("expected 1 active signal after content change, got %d", res.ActiveSignals)
	}
	if res.Delay != 0 {
		t.Errorf("expected delay 0 after reset, got %v", res.Delay)
	}

	// Use a brand new content to prove delay count restarts from 0 after reset.
	// First occurrence: 1 signal (rate only, fingerprint not duplicated yet) → no delay change.
	freshMessages := []model.Message{{Role: "user", Content: "fresh content after reset"}}
	res, err = d.Check("1", sessionID, freshMessages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 1 {
		t.Fatalf("expected 1 active signal for new content, got %d", res.ActiveSignals)
	}

	// Second occurrence of same fresh content: 2 signals → delay restarts from 1s
	res, err = d.Check("1", sessionID, freshMessages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 2 {
		t.Fatalf("expected 2 active signals, got %d", res.ActiveSignals)
	}
	if res.Delay != 1*time.Second {
		t.Errorf("expected delay 1s after reset (restarted from 1), got %v", res.Delay)
	}
}

func TestProgressiveDelay_Cap(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{
		RateThreshold:         1,
		FingerprintWindow:     10,
		FingerprintDuplicates: 1,
		SessionMaxRequests:    1000, // don't trigger session signal
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	messages := []model.Message{{Role: "user", Content: "same content"}}
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// Send enough requests to push past the cap
	var lastDelay time.Duration
	for i := 0; i < 10; i++ {
		res, err := d.Check("1", sessionID, messages)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if res.ActiveSignals != 2 {
			t.Fatalf("request %d: expected 2 active signals, got %d", i, res.ActiveSignals)
		}
		if res.Delay > 16*time.Second {
			t.Errorf("request %d: delay exceeds 16s cap: %v", i, res.Delay)
		}
		if res.Blocked {
			t.Errorf("request %d: expected not blocked (only 2 signals)", i)
		}
		lastDelay = res.Delay
	}
	if lastDelay != 16*time.Second {
		t.Errorf("expected final delay capped at 16s, got %v", lastDelay)
	}
}

func TestDelayAndBlockTransition(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{
		RateThreshold:         1,
		FingerprintWindow:     10,
		FingerprintDuplicates: 1,
		SessionMaxRequests:    1, // session triggers on 2nd request
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set("X-Ilter-Session-Id", "transition-test")
	messages := []model.Message{{Role: "user", Content: "test"}}
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// Request 1: 2 signals (rate + fingerprint) → delay, not blocked
	// Session count = 1, not > 1, so no session signal
	res, err := d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 2 {
		t.Fatalf("expected 2 active signals, got %d", res.ActiveSignals)
	}
	if res.Delay != 1*time.Second {
		t.Errorf("expected delay 1s for 2 signals, got %v", res.Delay)
	}
	if res.Blocked {
		t.Error("expected not blocked for 2 signals")
	}

	// Request 2: same session, session count = 2 > 1 → 3 signals → blocked
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	// rate(1) + fingerprint(repeat) + session(exceeds 1, since count=2 > 1) = 3 signals
	if res.ActiveSignals != 3 {
		t.Fatalf("expected 3 active signals to trigger block, got %d", res.ActiveSignals)
	}
	if !res.Blocked {
		t.Error("expected blocked for 3+ signals")
	}
	if res.Delay != 0 {
		t.Errorf("expected no delay when blocked, got %v", res.Delay)
	}
}

func TestMultiTierReaction(t *testing.T) {
	d := NewDetector(config.LoopSettingsConfig{
		RateThreshold:         1, // triggers rate signal immediately
		FingerprintWindow:     5,
		FingerprintDuplicates: 1, // triggers fingerprint signal immediately on any message
		CostThreshold:         1.0,
		CostWindow:            10 * time.Second,
		SessionMaxRequests:    1, // session triggers on 2nd request
	})

	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set("X-Ilter-Session-Id", "session-xyz")
	messages := []model.Message{{Role: "user", Content: "foo"}}
	sessionID := r.Header.Get("X-Ilter-Session-Id")

	// Case 1: 2 signals active (Rate and Fingerprint)
	// Both Rate (threshold 1) and Fingerprint (duplicates 1, window 5) will be active.
	// Cost is 0. Session is 1 (not > 1 yet).
	res, err := d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSignals != 2 {
		t.Fatalf("expected exactly 2 active signals, got %d", res.ActiveSignals)
	}
	if !res.Warning {
		t.Error("expected Warning to be true")
	}
	if res.Blocked {
		t.Error("expected Blocked to be false")
	}
	if res.Delay != 1*time.Second {
		t.Errorf("expected 1s delay on first delay, got %v", res.Delay)
	}

	// Second request with 2 signals active -> delay should double to 2s
	res, err = d.Check("1", sessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	// Wait, now session is 2 which is > 1! So session signal is also active.
	// That makes Rate, Fingerprint, and Session active (3 signals).
	// This should block!
	if res.ActiveSignals != 3 {
		t.Fatalf("expected 3 active signals, got %d", res.ActiveSignals)
	}
	if !res.Blocked {
		t.Error("expected Blocked to be true")
	}
}
