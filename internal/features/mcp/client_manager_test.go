package mcp

import (
	"context"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/inline"
)

// registerInlineHandler registers a simple inline handler for testing.
func registerInlineHandler(id string) {
	_ = inline.RegisterTools(id, func(_ context.Context, _ map[string]any) (any, error) {
		return "ok", nil
	}, []inline.ToolDef{{Name: "test", Description: "test"}})
}

func TestClientManagerGetOrCreate(t *testing.T) {
	registerInlineHandler("test-server-1")
	m := NewClientManager(nil)
	server := &ServerInfo{
		ID: "test-server-1",
		Config: config.MCPServerConfig{
			Transport: "inline",
			Name:      "test-server-1",
		},
	}

	client, err := m.GetOrCreate(context.Background(), server)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if !client.IsConnected() {
		t.Error("expected connected client")
	}
}

func TestClientManagerGetOrCreateCaches(t *testing.T) {
	registerInlineHandler("test-server-cache")
	m := NewClientManager(nil)
	server := &ServerInfo{
		ID: "test-server-cache",
		Config: config.MCPServerConfig{
			Transport: "inline",
			Name:      "test-server-cache",
		},
	}

	c1, _ := m.GetOrCreate(context.Background(), server)
	c2, _ := m.GetOrCreate(context.Background(), server)

	if c1 != c2 {
		t.Error("expected same client instance from cache")
	}
}

func TestClientManagerGetOrCreateUnknownTransport(t *testing.T) {
	m := NewClientManager(nil)
	server := &ServerInfo{
		ID: "test-server-bad",
		Config: config.MCPServerConfig{
			Transport: "unknown",
			Name:      "test-server-bad",
		},
	}

	_, err := m.GetOrCreate(context.Background(), server)
	if err == nil {
		t.Fatal("expected error for unknown transport")
	}
}

func TestClientManagerRemove(t *testing.T) {
	registerInlineHandler("test-server-remove")
	m := NewClientManager(nil)
	server := &ServerInfo{
		ID: "test-server-remove",
		Config: config.MCPServerConfig{
			Transport: "inline",
			Name:      "test-server-remove",
		},
	}

	c1, _ := m.GetOrCreate(context.Background(), server)
	m.Remove(server.ID, server.Config.Transport)

	// Should create a new instance
	c2, _ := m.GetOrCreate(context.Background(), server)
	if c1 == c2 {
		t.Error("expected new client after remove")
	}
}

func TestClientManagerRemoveNonexistent(_ *testing.T) {
	m := NewClientManager(nil)
	m.Remove("no-such-server", "inline")
}

func TestClientManagerCloseAll(t *testing.T) {
	registerInlineHandler("s1")
	registerInlineHandler("s2")
	m := NewClientManager(nil)
	s1 := &ServerInfo{ID: "s1", Config: config.MCPServerConfig{Transport: "inline", Name: "s1"}}
	s2 := &ServerInfo{ID: "s2", Config: config.MCPServerConfig{Transport: "inline", Name: "s2"}}

	_, _ = m.GetOrCreate(context.Background(), s1)
	_, _ = m.GetOrCreate(context.Background(), s2)
	m.CloseAll()

	c1, _ := m.GetOrCreate(context.Background(), s1)
	c2, _ := m.GetOrCreate(context.Background(), s2)
	if c1 == nil || c2 == nil {
		t.Error("expected new clients after CloseAll")
	}
}

func TestClientManagerConcurrency(t *testing.T) {
	registerInlineHandler("test-server-concurrent")
	m := NewClientManager(nil)
	server := &ServerInfo{
		ID: "test-server-concurrent",
		Config: config.MCPServerConfig{
			Transport: "inline",
			Name:      "test-server-concurrent",
		},
	}

	done := make(chan bool)
	const n = 20

	for i := 0; i < n; i++ {
		go func() {
			client, err := m.GetOrCreate(context.Background(), server)
			if err != nil {
				t.Errorf("concurrent GetOrCreate failed: %v", err)
			}
			if client == nil {
				t.Error("concurrent GetOrCreate returned nil")
			}
			done <- true
		}()
	}

	for i := 0; i < n; i++ {
		<-done
	}
}
