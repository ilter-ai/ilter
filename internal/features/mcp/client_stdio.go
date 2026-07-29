package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// stdioLog is scoped to stdio transport operations.
var stdioLog = slog.With("component", "mcp", "sub", "stdio")

// StdioClient implements TransportClient for MCP servers launched as child
// processes.  JSON-RPC messages are sent via stdin and received from stdout.
type StdioClient struct {
	server *ServerInfo

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	cancel    context.CancelFunc
	connected bool

	stderrBuf bytes.Buffer

	pendingMu sync.RWMutex
	pending   map[string]chan *JSONRPCResponse

	discoveredTools   []ToolDefinition
	negotiatedVersion protocol.ID
}

// NewStdioClient creates a server-side stdio client.  The process is spawned
// in Start().
func NewStdioClient(server *ServerInfo) *StdioClient {
	return &StdioClient{
		server:  server,
		pending: make(map[string]chan *JSONRPCResponse),
	}
}

func (c *StdioClient) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}

	cfg := c.server.Config
	if cfg.Command == "" {
		c.mu.Unlock()
		return fmt.Errorf("stdio server %q has no command configured", c.server.ID)
	}

	if _, err := exec.LookPath(cfg.Command); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("command %q not found on PATH (is it installed?): %w", cfg.Command, err)
	}

	childCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	args := cfg.Args
	if cfg.Command == "npx" && (len(args) == 0 || (args[0] != "-y" && args[0] != "--yes")) {
		args = append([]string{"-y"}, args...)
	}
	cmd := exec.CommandContext(childCtx, cfg.Command, args...)
	cmd.Dir = filepath.Dir(cfg.Command)
	setProcessGroup(cmd)

	cmd.Env = append(cmd.Env, os.Environ()...)
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	c.stderrBuf.Reset()
	cmd.Stderr = &c.stderrBuf

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		c.mu.Unlock()
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		c.mu.Unlock()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err = cmd.Start(); err != nil {
		stdin.Close()
		cancel()
		c.mu.Unlock()
		return fmt.Errorf("start process: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewScanner(stdout)
	c.stdout.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	c.connected = true

	go c.readLoop(childCtx)
	c.mu.Unlock()

	initCtx, initCancel := context.WithTimeout(childCtx, 5*time.Second)
	defer initCancel()

	rawCall := func(ctx context.Context, method string, params json.RawMessage) (*JSONRPCResponse, error) {
		id := json.RawMessage(`"handshake"`)
		return c.Call(ctx, &JSONRPCRequest{JSONRPC: JSONRPCVersion, ID: &id, Method: method, Params: params})
	}
	sendNotification := func(method string, params json.RawMessage) {
		body, _ := json.Marshal(&JSONRPCRequest{JSONRPC: JSONRPCVersion, Method: method, Params: params})
		c.mu.Lock()
		if c.stdin != nil {
			_, _ = c.stdin.Write(body)
			_, _ = c.stdin.Write([]byte("\n"))
		}
		c.mu.Unlock()
	}

	version, err := negotiateOutbound(initCtx, c.server, rawCall, sendNotification)
	if err != nil {
		c.mu.Lock()
		if c.stdin != nil {
			c.stdin.Close()
		}
		cancel()
		c.connected = false
		stderrStr := c.stderrBuf.String()
		c.mu.Unlock()
		if stderrStr != "" {
			stdioLog.Warn("initialize failed, stderr output",
				"server_id", c.server.ID, "command", cfg.Command, "stderr", stderrStr)
		}
		return fmt.Errorf("mcp handshake: %w (stderr: %s)", err, stderrStr)
	}
	c.mu.Lock()
	c.negotiatedVersion = version.ID()
	c.mu.Unlock()
	stdioLog.Debug("negotiated protocol version", "server_id", c.server.ID, "version", version.ID())

	discovered, discErr := c.discoverTools(initCtx)
	if discErr != nil {
		stdioLog.Warn("tools/list failed, tools will be unavailable",
			"server_id", c.server.ID, "error", discErr)
	} else {
		c.mu.Lock()
		c.discoveredTools = discovered
		c.mu.Unlock()
		stdioLog.Debug("discovered tools",
			"server_id", c.server.ID, "count", len(discovered))
	}

	stdioLog.Debug("started", "server_id", c.server.ID, "command", cfg.Command)
	return nil
}

func (c *StdioClient) discoverTools(ctx context.Context) ([]ToolDefinition, error) {
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

func (c *StdioClient) Call(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	if req.ID == nil {
		return nil, fmt.Errorf("stdio transport requires request IDs")
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
		return nil, fmt.Errorf("marshal: %w", err)
	}

	c.mu.Lock()
	if c.stdin == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("stdio client for %q is not started", c.server.ID)
	}
	_, err = c.stdin.Write(body)
	_, _ = c.stdin.Write([]byte("\n"))
	c.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("write to stdin: %w", err)
	}

	select {
	case rpcResp := <-ch:
		return rpcResp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *StdioClient) Tools() []ToolDefinition {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ToolDefinition, len(c.discoveredTools))
	copy(out, c.discoveredTools)
	return out
}

func (c *StdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		// Kill the entire process group (children too).  Ignore ESRCH:
		// the group may have already exited.
		_ = killProcessGroup(c.cmd.Process.Pid)
		_ = c.cmd.Wait()
	}
	c.connected = false
	return nil
}

func (c *StdioClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// NegotiatedVersion returns the MCP protocol version negotiated with this
// downstream server during Start, or "" if Start hasn't completed yet.
func (c *StdioClient) NegotiatedVersion() protocol.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.negotiatedVersion
}

// Stderr returns any output the child process wrote to stderr.
// Useful for surfacing server-side configuration errors (e.g. "please
// specify a directory") in the API response.
func (c *StdioClient) Stderr() string {
	return c.stderrBuf.String()
}

func (c *StdioClient) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !c.stdout.Scan() {
			if c.stdout.Err() != nil {
				stdioLog.Warn("read error", "server_id", c.server.ID, "error", c.stdout.Err())
			}
			return
		}

		c.handleLine(c.stdout.Bytes())
	}
}

// handleLine processes a single line from the MCP server's stdout.
//
// MCP stdio transport reserves stdout for newline-delimited JSON-RPC objects.
// Many servers (uv/uvx, Python MCP SDK, etc.) leak human-readable startup text
// to stdout during cold start. We handle this gracefully:
//
//  1. Lines not starting with '{' are logged at Debug and skipped (they cannot
//     be valid JSON-RPC messages).
//
//  2. json.Decoder is used instead of json.Unmarshal so that a valid JSON-RPC
//     object followed by trailing text on the same line (e.g. no \n separator
//     between a response and a log line) is still consumed and dispatched.
func (c *StdioClient) handleLine(raw []byte) {
	line := bytes.TrimSpace(raw)
	if len(line) == 0 {
		return
	}

	if line[0] != '{' {
		stdioLog.Debug("skipping non-JSON stdout",
			"server_id", c.server.ID, "line", truncateForLog(line, 200))
		return
	}

	var resp JSONRPCResponse
	dec := json.NewDecoder(bytes.NewReader(line))
	if err := dec.Decode(&resp); err != nil {
		stdioLog.Warn("failed to parse JSON-RPC response",
			"server_id", c.server.ID, "error", err, "line", truncateForLog(line, 200))
		return
	}

	if resp.JSONRPC != JSONRPCVersion {
		stdioLog.Debug("ignoring stdout JSON without valid jsonrpc field",
			"server_id", c.server.ID, "line", truncateForLog(line, 200))
		return
	}

	if n := int(dec.InputOffset()); n < len(line) {
		stdioLog.Debug("discarded trailing bytes after JSON-RPC message",
			"server_id", c.server.ID, "trailing", truncateForLog(line[n:], 120))
	}

	if resp.ID == nil {
		return
	}

	c.pendingMu.RLock()
	ch, ok := c.pending[string(*resp.ID)]
	c.pendingMu.RUnlock()
	if ok {
		select {
		case ch <- &resp:
		default:
		}
	}
}

func truncateForLog(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

var _ TransportClient = (*StdioClient)(nil)
