package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v7"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// ProcessManager wraps a TransportClient (typically StdioClient) adding
// automatic restart with exponential backoff and MCP ping-based health
// checks.  It implements the TransportClient interface.
//
// The health check goroutine sends a JSON-RPC "ping" every healthInterval.
// If the ping fails (or the underlying client reports disconnected) it
// triggers a restart of the child process.  Calls to Start / Call / Close
// are delegated to the inner client.
type ProcessManager struct {
	inner   TransportClient
	server  *ServerInfo
	factory func() (TransportClient, error)

	mu           sync.Mutex
	startOnce    sync.Once
	cancelHealth context.CancelFunc
	backoffObj   *backoff.ExponentialBackOff
	restarting   bool
	closed       bool
}

const (
	healthInterval = 30 * time.Second
)

func NewProcessManager(inner TransportClient, server *ServerInfo, factory func() (TransportClient, error)) *ProcessManager {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 30 * time.Second
	b.Multiplier = 2.0
	// MaxElapsedTime removed in v7, default 0 is correct (maxInterval limits growth)

	return &ProcessManager{
		inner:      inner,
		server:     server,
		factory:    factory,
		backoffObj: b,
	}
}

func (pm *ProcessManager) Start(ctx context.Context) error {
	pm.mu.Lock()
	if pm.closed {
		pm.mu.Unlock()
		return fmt.Errorf("process manager for %q is closed", pm.server.ID)
	}
	pm.mu.Unlock()

	if err := pm.inner.Start(ctx); err != nil {
		return err
	}

	pm.startOnce.Do(func() {
		hctx, cancel := context.WithCancel(context.Background())
		pm.cancelHealth = cancel
		go pm.healthLoop(hctx)
	})

	return nil
}

func (pm *ProcessManager) Call(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	resp, err := pm.inner.Call(ctx, req)
	if err != nil {
		pm.maybeRestart(ctx)
		return nil, err
	}
	return resp, nil
}

func (pm *ProcessManager) Close() error {
	pm.mu.Lock()
	pm.closed = true
	if pm.cancelHealth != nil {
		pm.cancelHealth()
	}
	pm.mu.Unlock()
	return pm.inner.Close()
}

func (pm *ProcessManager) Tools() []ToolDefinition {
	return pm.inner.Tools()
}

func (pm *ProcessManager) IsConnected() bool {
	return pm.inner.IsConnected()
}

func (pm *ProcessManager) NegotiatedVersion() protocol.ID {
	return pm.inner.NegotiatedVersion()
}

func (pm *ProcessManager) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.checkHealth(ctx)
		}
	}
}

func (pm *ProcessManager) checkHealth(ctx context.Context) {
	pm.mu.Lock()
	if pm.restarting || pm.closed {
		pm.mu.Unlock()
		return
	}
	pm.mu.Unlock()

	if !pm.inner.IsConnected() {
		pm.restart(ctx)
		return
	}

	// ping is removed by the 2026-07-28 spec — a downstream server
	// negotiated at that version is health-checked with a lightweight
	// tools/list call instead, which every version supports.
	healthCheckID := json.RawMessage(`"health-check"`)
	healthCheckReq := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      &healthCheckID,
		Method:  MethodPing,
	}
	if pm.inner.NegotiatedVersion() == protocol.V20260728 {
		healthCheckReq.Method = MethodToolsList
		healthCheckReq.Params = json.RawMessage(`{}`)
	}

	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := pm.inner.Call(checkCtx, healthCheckReq)
	if err != nil || resp == nil || resp.Error != nil {
		mcpLog.Warn("health check failed, restarting",
			"server_id", pm.server.ID, "method", healthCheckReq.Method, "error", err)
		pm.restart(ctx)
	}
}

func (pm *ProcessManager) restart(ctx context.Context) {
	pm.mu.Lock()
	if pm.restarting || pm.closed {
		pm.mu.Unlock()
		return
	}
	pm.restarting = true
	b := pm.backoffObj.NextBackOff()
	pm.mu.Unlock()

	select {
	case <-ctx.Done():
		return
	case <-time.After(b):
	}

	mcpLog.Info("restarting client",
		"server_id", pm.server.ID, "backoff_ms", b.Milliseconds())

	_ = pm.inner.Close()

	newClient, err := pm.factory()
	if err != nil {
		mcpLog.Error("failed to create new client on restart",
			"server_id", pm.server.ID, "error", err)
		pm.mu.Lock()
		pm.restarting = false
		pm.mu.Unlock()
		return
	}

	if err := newClient.Start(ctx); err != nil {
		mcpLog.Error("failed to start new client on restart",
			"server_id", pm.server.ID, "error", err)
		pm.mu.Lock()
		pm.restarting = false
		pm.mu.Unlock()
		return
	}

	pm.mu.Lock()
	pm.inner = newClient
	pm.restarting = false
	pm.backoffObj.Reset()
	pm.mu.Unlock()

	mcpLog.Info("client restarted successfully",
		"server_id", pm.server.ID)
}

func (pm *ProcessManager) maybeRestart(ctx context.Context) {
	pm.mu.Lock()
	if pm.restarting || pm.closed {
		pm.mu.Unlock()
		return
	}
	pm.mu.Unlock()
	go pm.restart(ctx)
}

var _ TransportClient = (*ProcessManager)(nil)
