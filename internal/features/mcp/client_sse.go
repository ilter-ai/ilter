package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sseLog is scoped to SSE transport operations.
var sseLog = slog.With("component", "mcp", "sub", "sse")

// SSEClient implements TransportClient for the SSE transport.
//
// Lifecycle:
//  1. Start() dials the server's SSE endpoint and waits for the "endpoint"
//     event to learn the POST URL.
//  2. Call() POSTs a JSON-RPC message to that URL and waits for the
//     corresponding response on the SSE stream.
//  3. Close() tears down the connection.
type SSEClient struct {
	server *ServerInfo

	mu        sync.RWMutex
	sseCancel context.CancelFunc
	sseDone   chan struct{}
	postURL   string
	connected bool

	pendingMu sync.RWMutex
	pending   map[string]chan *JSONRPCResponse

	httpClient *http.Client

	// discoveredTools caches the tools/list result from startup.
	discoveredTools []ToolDefinition
}

// NewSSEClient creates an unconnected SSE client.
func NewSSEClient(server *ServerInfo) *SSEClient {
	return &SSEClient{
		server:     server,
		sseDone:    make(chan struct{}),
		pending:    make(map[string]chan *JSONRPCResponse),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *SSEClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connected {
		return nil
	}

	if c.server.Config.URL == "" {
		return fmt.Errorf("SSE server %q has no url configured", c.server.ID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.server.Config.URL, nil)
	if err != nil {
		return fmt.Errorf("sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	setSSEAuthHeaders(req, c.server)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sse dial: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("sse dial: %s returned HTTP %d", c.server.Config.URL, resp.StatusCode)
	}

	childCtx, cancel := context.WithCancel(ctx)
	c.sseCancel = cancel

	go c.readLoop(childCtx, resp.Body)

	// Poll until readLoop sets postURL (with timeout).
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			resp.Body.Close()
			return fmt.Errorf("sse: timed out waiting for endpoint event from %q", c.server.ID)
		case <-childCtx.Done():
			return fmt.Errorf("sse: context canceled while waiting for endpoint event")
		default:
			c.mu.RLock()
			pu := c.postURL
			c.mu.RUnlock()
			if pu != "" {
				c.connected = true
				sseLog.Debug("connected", "server_id", c.server.ID,
					"post_url", pu)

				// Discover tools via tools/list (non-blocking on connect).
				discCtx, discCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer discCancel()
				discovered, discErr := c.discoverTools(discCtx)
				if discErr != nil {
					sseLog.Warn("tools/list failed, tools will be unavailable",
						"server_id", c.server.ID, "error", discErr)
				} else {
					c.mu.Lock()
					c.discoveredTools = discovered
					c.mu.Unlock()
					sseLog.Debug("discovered tools",
						"server_id", c.server.ID, "count", len(discovered))
				}

				return nil
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (c *SSEClient) Call(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	if req.ID == nil {
		return nil, fmt.Errorf("SSE transport requires request IDs (notifications not supported)")
	}

	idStr := string(*req.ID)

	ch := make(chan *JSONRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[idStr] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, idStr)
		c.pendingMu.Unlock()
	}()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.mu.RLock()
	pu := c.postURL
	c.mu.RUnlock()
	if pu == "" {
		return nil, fmt.Errorf("SSE client for %q has no post URL (not started?)", c.server.ID)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pu, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create post request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setSSEAuthHeaders(httpReq, c.server)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post message: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("post message: HTTP %d", resp.StatusCode)
	}

	select {
	case rpcResp := <-ch:
		return rpcResp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.sseDone:
		return nil, fmt.Errorf("SSE connection closed while waiting for response")
	}
}

func (c *SSEClient) Close() error {
	c.mu.Lock()
	if c.sseCancel != nil {
		c.sseCancel()
	}
	connected := c.connected
	c.mu.Unlock()

	if connected {
		<-c.sseDone
	}

	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
	return nil
}

func (c *SSEClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *SSEClient) Tools() []ToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ToolDefinition, len(c.discoveredTools))
	copy(out, c.discoveredTools)
	return out
}

// discoverTools calls tools/list with pagination via the SSE POST URL.
func (c *SSEClient) discoverTools(ctx context.Context) ([]ToolDefinition, error) {
	var allTools []ToolDefinition
	cursor := ""
	const maxPages = 100

	for page := 0; page < maxPages; page++ {
		params := json.RawMessage("{}")
		if cursor != "" {
			params = json.RawMessage(`{"cursor":"` + cursor + `"}`)
		}

		listID := json.RawMessage(`"tools/list"`)
		req := &JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      &listID,
			Method:  MethodToolsList,
			Params:  params,
		}

		resp, err := c.Call(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("tools/list call: %w", err)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("tools/list error (code %d): %s", resp.Error.Code, resp.Error.Message)
		}

		var result ListToolsResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("parse tools/list result: %w", err)
		}

		allTools = append(allTools, result.Tools...)

		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	return allTools, nil
}

// readLoop runs in a goroutine and processes SSE events from the server's
// response body.  It is terminated via context cancellation.
func (c *SSEClient) readLoop(ctx context.Context, body io.ReadCloser) {
	defer close(c.sseDone)
	defer body.Close()

	reader := bufio.NewReader(body)
	var eventType string
	var dataBuf strings.Builder

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				sseLog.Debug("read error", "server_id", c.server.ID, "error", err)
			}
			return
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")

		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			dataBuf.WriteString(data)

		case line == "":
			c.handleEvent(ctx, eventType, dataBuf.String())
			eventType = ""
			dataBuf.Reset()
		}
	}
}

func (c *SSEClient) handleEvent(_ context.Context, eventType, data string) {
	switch eventType {
	case "endpoint":
		c.mu.Lock()
		c.postURL = data
		c.mu.Unlock()

	case "message":
		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			sseLog.Warn("failed to parse message event",
				"server_id", c.server.ID, "error", err)
			return
		}
		c.dispatchResponse(&resp)

	case "":
		// Could be a raw data event (some servers omit the event: line).
		var resp JSONRPCResponse
		if json.Unmarshal([]byte(data), &resp) == nil && resp.ID != nil {
			c.dispatchResponse(&resp)
		}
	}
}

func (c *SSEClient) dispatchResponse(resp *JSONRPCResponse) {
	c.pendingMu.RLock()
	ch, ok := c.pending[string(*resp.ID)]
	c.pendingMu.RUnlock()
	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

func setSSEAuthHeaders(req *http.Request, server *ServerInfo) {
	if server.Config.AuthType == "bearer" {
		req.Header.Set("Authorization", "Bearer "+server.Config.AuthKeyEnv)
	} else if server.Config.AuthType == "basic" {
		req.Header.Set("Authorization", "Basic "+server.Config.AuthKeyEnv)
	}
}
