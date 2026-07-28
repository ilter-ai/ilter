package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ilter-ai/ilter/internal/model"
)

// RuntimeConfigReader abstracts direct read access over the runtime_config table
// to decouple configuration cache from the storage layer and avoid import cycles.
type RuntimeConfigReader interface {
	GetAll() (map[string]string, error)
	GetBySection(section string) (map[string]string, error)
}

// ─────────────────────────────────────────────────────────────────────
// ConfigSnapshot
// ─────────────────────────────────────────────────────────────────────

// Snapshot is the fully resolved, thread-safe runtime configuration.
// It embeds RuntimeConfigSnapshot and exposes typed accessor methods for
// each configuration section. Callers always read a consistent snapshot
// via ConfigCache.Get().
type Snapshot struct {
	*RuntimeConfigSnapshot
}

// Providers returns the resolved provider configurations.
func (s *Snapshot) Providers() []ProviderConfig { return s.RuntimeConfigSnapshot.Providers }

// MCPServers returns the resolved MCP server configurations.
func (s *Snapshot) MCPServers() []MCPServerConfig { return s.RuntimeConfigSnapshot.MCPServers }

// MCPAccess returns the resolved MCP access rules.
func (s *Snapshot) MCPAccess() []MCPAccessRule { return s.RuntimeConfigSnapshot.MCPAccess }

// GuardRules returns the resolved guardrail rule-set names.
func (s *Snapshot) GuardRules() []string { return s.RuntimeConfigSnapshot.GuardRules }

// CustomRules returns the resolved custom guardrail rules.
func (s *Snapshot) CustomRules() []CustomRuleConfig { return s.RuntimeConfigSnapshot.CustomRules }

// RoutingConfig returns the resolved routing configuration.
func (s *Snapshot) RoutingConfig() RoutingConfig { return s.RuntimeConfigSnapshot.Routing }

// OpenAPITools returns the resolved OpenAPI tool specifications.
func (s *Snapshot) Fallback() FallbackConfig { return s.RuntimeConfigSnapshot.Fallback }

// OpenAPITools returns the resolved OpenAPI tool specifications.
func (s *Snapshot) OpenAPITools() []OpenAPISpecConfig {
	return s.RuntimeConfigSnapshot.OpenAPITools
}

// ─────────────────────────────────────────────────────────────────────
// RuntimeStores
// ─────────────────────────────────────────────────────────────────────

// RuntimeStores holds all DB-backed store instances needed to refresh
// runtime state from the database. Fields may be nil to skip that section
// during Refresh; the corresponding state section will remain at boot defaults.
type RuntimeStores struct {
	RuntimeConfig RuntimeConfigReader
}

// ─────────────────────────────────────────────────────────────────────
// ConfigCache
// ─────────────────────────────────────────────────────────────────────

// Cache provides lock-free, atomic reads of the current configuration
// snapshot. It hot-swaps the snapshot on every successful Refresh, ensuring
// concurrent readers always see a consistent view without blocking.
type Cache struct {
	boot      *BootConfig
	snapshot  atomic.Pointer[Snapshot]
	callbacks []func(snap *Snapshot)
	mu        sync.Mutex
}

// NewConfigCache creates a ConfigCache initialized with boot defaults as the
// initial snapshot. Before any Refresh call, Get() returns the boot-default
// snapshot.
func NewConfigCache(boot *BootConfig) *Cache {
	c := &Cache{boot: boot}
	c.snapshot.Store(&Snapshot{
		RuntimeConfigSnapshot: ResolveRuntime(boot, nil),
	})
	return c
}

// Get returns the current configuration snapshot. This is a lock-free,
// atomic read — it does not block concurrent Refresh calls.
func (c *Cache) Get() *Snapshot {
	return c.snapshot.Load()
}

// OnChange registers a callback that is invoked after every successful
// Refresh. Callbacks are called synchronously and sequentially in the
// order they were registered.
func (c *Cache) OnChange(callback func(snap *Snapshot)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callbacks = append(c.callbacks, callback)
}

// Refresh loads all runtime configuration from DB stores, merges it with the
// boot defaults via ResolveRuntime, and atomically swaps the snapshot.
// After a successful swap, all registered OnChange callbacks are invoked.
func (c *Cache) Refresh(ctx context.Context, stores *RuntimeStores) error {
	state, err := loadStateFromStores(ctx, stores)
	if err != nil {
		return fmt.Errorf("config cache refresh: %w", err)
	}

	snap := &Snapshot{
		RuntimeConfigSnapshot: ResolveRuntime(c.boot, state),
	}
	c.snapshot.Store(snap)
	c.fireCallbacks(snap)
	return nil
}

func (c *Cache) fireCallbacks(snap *Snapshot) {
	c.mu.Lock()
	callbacks := make([]func(*Snapshot), len(c.callbacks))
	copy(callbacks, c.callbacks)
	c.mu.Unlock()

	for _, cb := range callbacks {
		cb(snap)
	}
}

// ─────────────────────────────────────────────────────────────────────
// Background polling
// ─────────────────────────────────────────────────────────────────────

// StartConfigPolling launches a background goroutine that periodically calls
// Refresh on the given ConfigCache. The goroutine stops when ctx is canceled.
// An initial refresh runs immediately on start.
func StartConfigPolling(ctx context.Context, cache *Cache, stores *RuntimeStores, interval time.Duration) {
	go func() {
		if err := cache.Refresh(ctx, stores); err != nil {
			slog.Warn("config cache: initial refresh failed", "error", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := cache.Refresh(ctx, stores); err != nil {
					slog.Warn("config cache: refresh failed", "error", err)
				}
			}
		}
	}()
}

// ─────────────────────────────────────────────────────────────────────
// State loading from stores
// ─────────────────────────────────────────────────────────────────────

// loadStateFromStores reads every runtime configuration section from the
// given stores and assembles a StateConfig. Stores that are nil are skipped.
func loadStateFromStores(_ context.Context, stores *RuntimeStores) (*StateConfig, error) {
	state := &StateConfig{}

	// ── Generic runtime_config entries ──
	if stores.RuntimeConfig != nil {
		values, err := stores.RuntimeConfig.GetAll()
		if err != nil {
			return nil, fmt.Errorf("runtime_config: %w", err)
		}
		if len(values) > 0 {
			state.RuntimeConfigValues = values
		}

		// ── Guardrail rules ──
		grEntries, err := stores.RuntimeConfig.GetBySection("guardrail_rule")
		if err != nil {
			return nil, fmt.Errorf("guardrail rules: %w", err)
		}
		if len(grEntries) > 0 {
			customRules := make([]CustomRuleConfig, 0, len(grEntries))
			guardRuleNames := make([]string, 0, len(grEntries))
			for key, val := range grEntries {
				var gr model.GuardrailRule
				if uErr := json.Unmarshal([]byte(val), &gr); uErr != nil {
					slog.Warn("config cache: skipping unparseable guardrail rule", "key", key, "error", uErr)
					continue
				}
				guardRuleNames = append(guardRuleNames, gr.Name)
				customRules = append(customRules, CustomRuleConfig{
					ID:       gr.Name,
					Patterns: []string{gr.Pattern},
					Mode:     gr.Action,
					Severity: gr.Severity,
					Enabled:  gr.Enabled,
				})
			}
			state.GuardRules = guardRuleNames
			state.CustomRules = customRules
		}
	}

	return state, nil
}
