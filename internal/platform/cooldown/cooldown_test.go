package cooldown

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCandidate_Key(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate Candidate
		wantKey   string
	}{
		{
			name: "Given candidate with provider, model, and keyID - When Key() is called - Then returns consistent delimiter string",
			candidate: Candidate{
				Provider: "openai",
				Model:    "gpt-4",
				KeyID:    "key-123",
			},
			wantKey: "openai|gpt-4|key-123",
		},
		{
			name: "Given candidate with empty values - When Key() is called - Then returns consistent delimiter string",
			candidate: Candidate{
				Provider: "",
				Model:    "",
				KeyID:    "",
			},
			wantKey: "||",
		},
		{
			name: "Given candidate with special characters - When Key() is called - Then returns consistent delimiter string",
			candidate: Candidate{
				Provider: "anthropic",
				Model:    "claude-3-opus-20240229",
				KeyID:    "sk-ant-key-with-dashes",
			},
			wantKey: "anthropic|claude-3-opus-20240229|sk-ant-key-with-dashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.candidate.Key()
			if got != tt.wantKey {
				t.Errorf("Candidate.Key() = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

func TestInMemoryStore_InCooldown_SetCooldown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name           string
		setup          func(s *InMemoryStore)
		candidate      Candidate
		duration       time.Duration
		checkAfter     time.Duration
		wantInCooldown bool
	}{
		{
			name:           "Given empty store - When InCooldown called - Then returns false",
			candidate:      Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-1"},
			duration:       0,
			checkAfter:     0,
			wantInCooldown: false,
		},
		{
			name: "Given candidate with cooldown set - When InCooldown called immediately - Then returns true",
			setup: func(s *InMemoryStore) {
				s.SetCooldown(ctx, Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-1"}, time.Hour)
			},
			candidate:      Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-1"},
			duration:       0,
			checkAfter:     0,
			wantInCooldown: true,
		},
		{
			name: "Given candidate with short cooldown - When InCooldown called after expiry - Then returns false",
			setup: func(s *InMemoryStore) {
				s.SetCooldown(ctx, Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-2"}, 10*time.Millisecond)
			},
			candidate:      Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-2"},
			duration:       0,
			checkAfter:     50 * time.Millisecond,
			wantInCooldown: false,
		},
		{
			name: "Given different candidates - When one has cooldown - Then other is not in cooldown",
			setup: func(s *InMemoryStore) {
				s.SetCooldown(ctx, Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-a"}, time.Hour)
			},
			candidate:      Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-b"},
			duration:       0,
			checkAfter:     0,
			wantInCooldown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testStore := NewInMemoryStore()
			if tt.setup != nil {
				tt.setup(testStore)
			}

			time.Sleep(tt.checkAfter)

			got := testStore.InCooldown(ctx, tt.candidate)
			if got != tt.wantInCooldown {
				t.Errorf("InCooldown() = %v, want %v", got, tt.wantInCooldown)
			}
		})
	}
}

func TestInMemoryStore_SetCooldown_OverwritesExisting(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	candidate := Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-1"}

	// Set initial cooldown
	store.SetCooldown(ctx, candidate, time.Hour)
	if !store.InCooldown(ctx, candidate) {
		t.Error("Expected in cooldown after first set")
	}

	// Overwrite with shorter cooldown
	store.SetCooldown(ctx, candidate, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	if store.InCooldown(ctx, candidate) {
		t.Error("Expected not in cooldown after overwrite with shorter duration and expiry")
	}
}

func TestInMemoryStore_ConcurrentAccess(t *testing.T) {
	// This test runs with -race flag to detect data races
	store := NewInMemoryStore()
	ctx := context.Background()
	candidate := Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-race"}

	const numGoroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Writers
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				store.SetCooldown(ctx, candidate, time.Hour)
			}
		}()
	}

	// Readers
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				_ = store.InCooldown(ctx, candidate)
			}
		}()
	}

	wg.Wait()

	// Final check - should be in cooldown
	if !store.InCooldown(ctx, candidate) {
		t.Error("Expected to be in cooldown after concurrent access")
	}
}

func TestInMemoryStore_LazyCleanup(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	// Set a very short cooldown
	candidate := Candidate{Provider: "openai", Model: "gpt-4", KeyID: "key-cleanup"}
	store.SetCooldown(ctx, candidate, 5*time.Millisecond)

	// Wait for expiry
	time.Sleep(20 * time.Millisecond)

	// Should not be in cooldown (lazy cleanup on read)
	if store.InCooldown(ctx, candidate) {
		t.Error("Expected not in cooldown after expiry (lazy cleanup)")
	}
}
