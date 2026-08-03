package config_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ilter-ai/ilter/internal/config"
	dbpkg "github.com/ilter-ai/ilter/internal/db"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "ilter-cache-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("open sqlite: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

// setupTestStores initializes a test database, runs migrations, and
// returns a RuntimeStores with only the RuntimeConfigStore populated.
func setupTestStores(t *testing.T) (*config.RuntimeStores, func()) {
	t.Helper()
	db, cleanup := setupTestDB(t)

	if err := dbpkg.ApplyMigrations(db); err != nil {
		cleanup()
		t.Fatalf("ApplyMigrations: %v", err)
	}

	stores := &config.RuntimeStores{
		RuntimeConfig: dbpkg.NewSQLiteStoreFromDB(db),
	}
	return stores, cleanup
}

func TestConfigCache_BootDefaults(t *testing.T) {
	boot := config.DefaultBootConfig()
	cache := config.NewConfigCache(&boot)

	snap := cache.Get()
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}

	if snap.Server.Host != boot.Server.Host {
		t.Errorf("Server.Host: got %q, want %q", snap.Server.Host, boot.Server.Host)
	}
	if snap.Auth.AdminKey != boot.Auth.AdminKey {
		t.Errorf("Auth.AdminKey: got %q, want %q", snap.Auth.AdminKey, boot.Auth.AdminKey)
	}
	if snap.GuardrailsEnabled != boot.Guardrails.Enabled {
		t.Errorf("GuardrailsEnabled: got %v, want %v", snap.GuardrailsEnabled, boot.Guardrails.Enabled)
	}
	if snap.MCPEnabled != boot.MCP.Enabled {
		t.Errorf("MCPEnabled: got %v, want %v", snap.MCPEnabled, boot.MCP.Enabled)
	}

	expectedThreshold := 0.70
	if snap.CacheSimilarityThreshold != expectedThreshold {
		t.Errorf("CacheSimilarityThreshold: got %f, want %f", snap.CacheSimilarityThreshold, expectedThreshold)
	}

	expectedRetention := int64(90)
	if snap.AuditRetentionDays != expectedRetention {
		t.Errorf("AuditRetentionDays: got %d, want %d", snap.AuditRetentionDays, expectedRetention)
	}

	if len(snap.Providers()) != 0 {
		t.Error("expected empty Providers before refresh")
	}
	if len(snap.MCPServers()) != 0 {
		t.Error("expected empty MCPServers before refresh")
	}
}

func TestConfigCache_Refresh(t *testing.T) {
	boot := config.DefaultBootConfig()
	cache := config.NewConfigCache(&boot)
	stores, cleanup := setupTestStores(t)
	defer cleanup()

	ctx := context.Background()

	dbStore := stores.RuntimeConfig.(*dbpkg.SQLiteStore)
	if err := dbStore.UpsertRuntimeConfig("feature_flag", "rate_limit", "true", "test"); err != nil {
		t.Fatalf("seed feature flag: %v", err)
	}
	if err := dbStore.UpsertRuntimeConfig("feature_flag", "semantic_cache", "false", "test"); err != nil {
		t.Fatalf("seed feature flag: %v", err)
	}

	if err := cache.Refresh(ctx, stores); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	snap := cache.Get()
	if snap == nil {
		t.Fatal("expected non-nil snapshot after refresh")
	}

	if snap.AuditRetentionDays != 90 {
		t.Errorf("boot default AuditRetentionDays overwritten: got %d", snap.AuditRetentionDays)
	}
	if snap.CacheSimilarityThreshold != 0.70 {
		t.Errorf("boot default CacheSimilarityThreshold overwritten: got %f", snap.CacheSimilarityThreshold)
	}
}

func TestConfigCache_ConcurrentReads(t *testing.T) {
	boot := config.DefaultBootConfig()
	cache := config.NewConfigCache(&boot)
	stores, cleanup := setupTestStores(t)
	defer cleanup()

	ctx := context.Background()

	// Seed a runtime_config value for the refresh
	dbStore := stores.RuntimeConfig.(*dbpkg.SQLiteStore)
	if err := dbStore.UpsertRuntimeConfig("feature_flag", "rate_limit", "true", "test"); err != nil {
		t.Fatalf("seed feature flag: %v", err)
	}

	var wg sync.WaitGroup

	// Readers: concurrent Get() in a tight loop
	for range 10 {
		wg.Go(func() {
			for range 50 {
				snap := cache.Get()
				if snap == nil {
					panic("nil snapshot during concurrent read")
				}
				// Ensure we can read fields without panicking
				_ = snap.Server.Host
				_ = snap.Providers()
				_ = snap.MCPServers()
				_ = snap.GuardRules()
				_ = snap.RoutingConfig()
				_ = snap.OpenAPITools()
			}
		})
	}

	// Writer: Refresh in parallel with readers
	for range 10 {
		wg.Go(func() {
			if err := cache.Refresh(ctx, stores); err != nil {
				t.Errorf("Refresh error: %v", err)
			}
		})
	}

	wg.Wait()
}

func TestConfigCache_OnChange(t *testing.T) {
	boot := config.DefaultBootConfig()
	cache := config.NewConfigCache(&boot)
	stores, cleanup := setupTestStores(t)
	defer cleanup()

	ctx := context.Background()

	var mu sync.Mutex
	callCount := 0
	var lastSnap *config.Snapshot

	cache.OnChange(func(snap *config.Snapshot) {
		mu.Lock()
		callCount++
		lastSnap = snap
		mu.Unlock()
	})

	if err := cache.Refresh(ctx, stores); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	mu.Lock()
	if callCount != 1 {
		t.Errorf("expected 1 callback call, got %d", callCount)
	}
	if lastSnap == nil {
		t.Error("expected non-nil snapshot in callback")
	}
	mu.Unlock()

	if err := cache.Refresh(ctx, stores); err != nil {
		t.Fatalf("second refresh: %v", err)
	}

	mu.Lock()
	if callCount != 2 {
		t.Errorf("expected 2 callback calls, got %d", callCount)
	}
	mu.Unlock()
}

func TestConfigCache_NilStores(t *testing.T) {
	boot := config.DefaultBootConfig()
	cache := config.NewConfigCache(&boot)
	ctx := context.Background()

	// All stores nil — refresh should not error
	stores := &config.RuntimeStores{}
	if err := cache.Refresh(ctx, stores); err != nil {
		t.Fatalf("Refresh with nil stores: %v", err)
	}

	snap := cache.Get()
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}

	if len(snap.Providers()) != 0 {
		t.Error("expected empty Providers with nil store")
	}
	if len(snap.MCPServers()) != 0 {
		t.Error("expected empty MCPServers with nil store")
	}

	if snap.AuditRetentionDays != 90 {
		t.Errorf("boot default lost: got %d", snap.AuditRetentionDays)
	}
}

func TestConfigSnapshot_Accessors(t *testing.T) {
	boot := config.DefaultBootConfig()
	boot.Routing.Enabled = false
	cache := config.NewConfigCache(&boot)

	snap := cache.Get()

	_ = snap.Providers()
	_ = snap.MCPServers()
	_ = snap.MCPAccess()
	_ = snap.GuardRules()
	_ = snap.CustomRules()
	_ = snap.RoutingConfig()
	_ = snap.OpenAPITools()

	if len(snap.Providers()) != 0 {
		t.Error("expected empty Providers before refresh")
	}
	if len(snap.OpenAPITools()) != 0 {
		t.Error("expected empty OpenAPITools before refresh")
	}
	if snap.RoutingConfig().ProviderPreference != "" {
		t.Error("expected empty provider preference before refresh")
	}
	if snap.RoutingConfig().Enabled {
		t.Error("expected routing disabled before refresh")
	}
}
