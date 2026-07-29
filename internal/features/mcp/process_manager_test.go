package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// mockTransportForHealthCheck records which JSON-RPC method the health
// check actually sent, so tests can assert ping-vs-tools/list selection
// based on NegotiatedVersion without spinning up a real process.
type mockTransportForHealthCheck struct {
	connected atomic.Bool
	version   protocol.ID
	lastCall  atomic.Value // string method name
}

func (m *mockTransportForHealthCheck) Start(context.Context) error { return nil }
func (m *mockTransportForHealthCheck) Close() error                { return nil }
func (m *mockTransportForHealthCheck) IsConnected() bool           { return m.connected.Load() }
func (m *mockTransportForHealthCheck) Tools() []ToolDefinition     { return nil }
func (m *mockTransportForHealthCheck) NegotiatedVersion() protocol.ID {
	return m.version
}
func (m *mockTransportForHealthCheck) Call(_ context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	m.lastCall.Store(req.Method)
	return NewSuccessResponse(req.ID, map[string]any{}), nil
}

func TestProcessManager_HealthCheck_UsesPingForOlderVersions(t *testing.T) {
	for _, v := range []protocol.ID{protocol.V20241105, protocol.V20250326} {
		mock := &mockTransportForHealthCheck{version: v}
		mock.connected.Store(true)
		pm := NewProcessManager(mock, &ServerInfo{ID: "s1", Config: config.MCPServerConfig{ID: "s1"}}, nil)

		pm.checkHealth(context.Background())

		got, _ := mock.lastCall.Load().(string)
		if got != MethodPing {
			t.Errorf("version %q: health check used method %q, want %q", v, got, MethodPing)
		}
	}
}

func TestProcessManager_HealthCheck_UsesToolsListFor2026(t *testing.T) {
	mock := &mockTransportForHealthCheck{version: protocol.V20260728}
	mock.connected.Store(true)
	pm := NewProcessManager(mock, &ServerInfo{ID: "s1", Config: config.MCPServerConfig{ID: "s1"}}, nil)

	pm.checkHealth(context.Background())

	got, _ := mock.lastCall.Load().(string)
	if got != MethodToolsList {
		t.Errorf("2026-07-28: health check used method %q, want %q (ping was removed by this version)", got, MethodToolsList)
	}
}

func TestProcessManager_NegotiatedVersion_Delegates(t *testing.T) {
	mock := &mockTransportForHealthCheck{version: protocol.V20250326}
	pm := NewProcessManager(mock, &ServerInfo{ID: "s1", Config: config.MCPServerConfig{ID: "s1"}}, nil)
	if pm.NegotiatedVersion() != protocol.V20250326 {
		t.Errorf("NegotiatedVersion() = %q, want %q", pm.NegotiatedVersion(), protocol.V20250326)
	}
}

func TestProcessManager_HealthCheck_DisconnectedTriggersRestart(t *testing.T) {
	// A disconnected client should attempt a restart rather than sending
	// any health-check RPC at all (there's nothing connected to call).
	mock := &mockTransportForHealthCheck{version: protocol.V20250326}
	mock.connected.Store(false)

	restarted := make(chan struct{}, 1)
	pm := NewProcessManager(mock, &ServerInfo{ID: "s1", Config: config.MCPServerConfig{ID: "s1"}}, func() (TransportClient, error) {
		select {
		case restarted <- struct{}{}:
		default:
		}
		newMock := &mockTransportForHealthCheck{version: protocol.V20250326}
		newMock.connected.Store(true)
		return newMock, nil
	})

	pm.checkHealth(context.Background())

	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected restart to be attempted for a disconnected client")
	}
}
