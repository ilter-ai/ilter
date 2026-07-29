package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ilter-ai/ilter/internal/features/mcp/inline"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
	"github.com/ilter-ai/ilter/internal/version"
)

// InlineClient wraps a Go function as an MCP TransportClient.
//
// The function receives the deserialised CallToolParams and returns a
// CallToolResult.  Because inline tools run in-process there is no actual
// transport round-trip — the response is computed synchronously.
type InlineClient struct {
	server  *ServerInfo
	handler inline.HandlerFunc

	mu        sync.Mutex
	connected bool

	// negotiatedVersion for an inline "server" isn't negotiated via a real
	// round trip (there's no external process to talk to — the handler
	// runs in-process), so it's just resolved from the server's config pin
	// (or newest, if "auto") at Start time. Kept for interface consistency
	// with the real transports and so handleMethod's synthesized
	// initialize response reflects the same version model.
	negotiatedVersion protocol.ID
}

// NewInlineClient creates a client that delegates every tools/call to the
// handler registered in the inline registry for the server's ID.
func NewInlineClient(server *ServerInfo) (*InlineClient, error) {
	h, ok := inline.Lookup(server.ID)
	if !ok {
		return nil, fmt.Errorf("inline server %q has no registered handler", server.ID)
	}
	return &InlineClient{
		server:  server,
		handler: h,
	}, nil
}

func (c *InlineClient) Start(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = true
	c.negotiatedVersion = protocol.Negotiate(protocol.ID(c.server.Config.ProtocolVersion)).ID()
	return nil
}

func (c *InlineClient) Call(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	if req.Method != "tools/call" {
		// For non-call methods (initialize, tools/list, ping) we synthesize
		// a response based on the inline server's metadata.
		return c.handleMethod(req)
	}

	// Deserialize params — req.Params is json.RawMessage ([]byte).
	var rawParams map[string]any
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &rawParams); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams,
				"inline: invalid params: "+err.Error()), nil
		}
	}

	// Extract arguments for the inline handler.
	args, _ := rawParams["arguments"].(map[string]any)

	resultValue, err := c.handler(ctx, args)
	if err != nil {
		result, marshalErr := marshalResult(CallToolResult{IsError: true, Content: []ToolContent{{Type: "text", Text: err.Error()}}})
		if marshalErr != nil {
			return nil, marshalErr
		}
		return &JSONRPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      req.ID,
			Result:  result,
		}, nil
	}

	result, marshalErr := marshalResult(CallToolResult{Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("%v", resultValue)}}})
	if marshalErr != nil {
		return nil, marshalErr
	}
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result:  result,
	}, nil
}

func (c *InlineClient) handleMethod(req *JSONRPCRequest) (*JSONRPCResponse, error) {
	switch req.Method {
	case "initialize":
		c.mu.Lock()
		v := protocol.Negotiate(c.negotiatedVersion)
		c.mu.Unlock()
		resultJSON, err := v.HandleInitialize(req.Params, protocol.ImplementationInfo{Name: "ilter", Version: version.Version})
		if errors.Is(err, protocol.ErrNoInitializeHandshake) {
			// Mirrors gateway.go/hub.go's graceful degradation: a caller
			// that explicitly invokes the legacy `initialize` method is,
			// by definition, not speaking the stateless 2026-07-28 model
			// (which has no such method) — fall back to the newest
			// version that still defines it rather than erroring.
			for _, id := range protocol.Supported {
				if id == v.ID() {
					continue
				}
				fallback := protocol.Negotiate(id)
				resultJSON, err = fallback.HandleInitialize(req.Params, protocol.ImplementationInfo{Name: "ilter", Version: version.Version})
				if err == nil {
					v = fallback
					break
				}
			}
		}
		if err != nil {
			return NewErrorResponse(req.ID, v.ErrorCode(protocol.ErrMethodNotFound), err.Error()), nil
		}
		return NewSuccessResponse(req.ID, resultJSON), nil

	case "tools/list":
		inlineTools := inline.ListTools(c.server.ID)
		tools := make([]ToolDefinition, 0, len(inlineTools))
		for _, t := range inlineTools {
			schema, _ := json.Marshal(t.InputSchema)
			tools = append(tools, ToolDefinition{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: schema,
			})
		}
		if tools == nil {
			tools = []ToolDefinition{}
		}
		return NewSuccessResponse(req.ID, map[string]any{
			"tools": tools,
		}), nil

	case "ping":
		return NewSuccessResponse(req.ID, map[string]any{}), nil

	default:
		return NewErrorResponse(req.ID, ErrorCodeMethodNotFound,
			fmt.Sprintf("Method not found: %s", req.Method)), nil
	}
}

func (c *InlineClient) Tools() []ToolDefinition {
	inlineTools := inline.ListTools(c.server.ID)
	tools := make([]ToolDefinition, 0, len(inlineTools))
	for _, t := range inlineTools {
		schema, _ := json.Marshal(t.InputSchema)
		tools = append(tools, ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return tools
}

func (c *InlineClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	return nil
}

func (c *InlineClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// NegotiatedVersion returns the protocol version resolved for this inline
// "server" at Start time (its config pin, or newest if "auto").
func (c *InlineClient) NegotiatedVersion() protocol.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.negotiatedVersion
}

// Ensure interface compliance.
var _ TransportClient = (*InlineClient)(nil)

// marshalResult is a helper that JSON-encodes a value, returning an error on failure.
func marshalResult(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("inline: marshal failed: %w", err)
	}
	return json.RawMessage(b), nil
}
