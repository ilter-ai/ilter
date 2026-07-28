package inline

import (
	"context"
	"fmt"
	"sync"
)

// HandlerFunc processes a tools/call for an inline tool.
// It receives the raw arguments from the JSON-RPC params and returns a
// result value that will be serialized into the CallToolResult.Content.
// Returning a non-nil error signals that the tool call failed.
type HandlerFunc func(ctx context.Context, args map[string]any) (any, error)

// ToolDef describes one tool that an inline server exposes.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

var (
	mu       sync.RWMutex
	handlers = make(map[string]HandlerFunc) // serverID -> handler
	toolDefs = make(map[string][]ToolDef)   // serverID -> tool list
)

// RegisterTools registers an inline handler and its tool definitions for the
// given server ID.  Returns an error if the ID is already registered.
func RegisterTools(serverID string, handler HandlerFunc, tools []ToolDef) error {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := handlers[serverID]; ok {
		return fmt.Errorf("inline: duplicate handler for server ID: %s", serverID)
	}
	handlers[serverID] = handler
	if tools == nil {
		toolDefs[serverID] = []ToolDef{}
	} else {
		toolDefs[serverID] = tools
	}
	return nil
}

// Lookup returns the handler for the given server ID.
func Lookup(serverID string) (HandlerFunc, bool) {
	mu.RLock()
	defer mu.RUnlock()
	h, ok := handlers[serverID]
	return h, ok
}

// ListTools returns the registered tool definitions for serverID, or nil.
func ListTools(serverID string) []ToolDef {
	mu.RLock()
	defer mu.RUnlock()
	return toolDefs[serverID]
}
