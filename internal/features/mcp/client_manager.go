package mcp

import (
	"context"
	"fmt"
	"sync"
)

// TransportClient is the interface that MCP transport implementations must satisfy.
type TransportClient interface {
	Start(ctx context.Context) error
	Call(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error)
	Close() error
	IsConnected() bool
	Tools() []ToolDefinition
}

type clientKey struct {
	serverID  string
	transport string
}

// ClientManager manages the lifecycle of MCP transport clients.
type ClientManager struct {
	mu       sync.RWMutex
	clients  map[clientKey]TransportClient
	registry *Registry
}

// NewClientManager creates a new ClientManager.
// If registry is non-nil, tools are synced to it after each client Start.
func NewClientManager(registry *Registry) *ClientManager {
	return &ClientManager{
		clients:  make(map[clientKey]TransportClient),
		registry: registry,
	}
}

// GetOrCreate returns an existing client for the given server or creates a new one.
func (m *ClientManager) GetOrCreate(_ context.Context, server *ServerInfo) (TransportClient, error) {
	k := clientKey{serverID: server.ID, transport: server.Config.Transport}

	m.mu.RLock()
	if c, ok := m.clients[k]; ok && c.IsConnected() {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if c, ok := m.clients[k]; ok && c.IsConnected() {
		return c, nil
	}

	client, err := m.newClient(server)
	if err != nil {
		return nil, err
	}

	// Use background context so the MCP subprocess outlives individual HTTP requests.
	if err := client.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("start client for %s: %w", server.ID, err)
	}

	// Sync discovered tools to the registry (and persist to DB).
	if m.registry != nil {
		if tools := client.Tools(); len(tools) > 0 {
			if err := m.registry.SyncTools(server.ID, tools); err != nil {
				mcpLog.Warn("failed to sync tools after start",
					"server_id", server.ID, "error", err)
			}
		}
	}

	m.clients[k] = client
	return client, nil
}

// NewTransportClient creates the appropriate TransportClient for the server's transport type.
func NewTransportClient(server *ServerInfo) (TransportClient, error) {
	switch server.Config.Transport {
	case "inline":
		return NewInlineClient(server)
	case "stdio":
		return NewStdioClient(server), nil
	case "sse":
		return NewSSEClient(server), nil
	default:
		return nil, fmt.Errorf("unknown transport: %s", server.Config.Transport)
	}
}

func (m *ClientManager) newClient(server *ServerInfo) (TransportClient, error) {
	client, err := NewTransportClient(server)
	if err != nil {
		return nil, err
	}
	if server.Config.Transport == "stdio" {
		client = NewProcessManager(client, server, func() (TransportClient, error) {
			return NewTransportClient(server)
		})
	}
	return client, nil
}

// Remove stops and removes a client by server ID and transport type.
func (m *ClientManager) Remove(serverID, transport string) {
	k := clientKey{serverID: serverID, transport: transport}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[k]; ok {
		if err := c.Close(); err != nil {
			mcpLog.Error("close client", "server", serverID, "error", err)
		}
		delete(m.clients, k)
	}
}

// CloseAll stops and removes all managed clients.
func (m *ClientManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, c := range m.clients {
		if err := c.Close(); err != nil {
			mcpLog.Error("close client", "key", k, "error", err)
		}
		delete(m.clients, k)
	}
}
