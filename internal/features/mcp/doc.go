// Package mcp implements the MCP (Model Context Protocol) integration layer.
//
// Architecture overview:
//
//	gateway/     — MCP Gateway management (register, authorize, route tools)
//	hub/         — WebSocket hub for MCP server connections
//	handler/     — HTTP handler for MCP requests
//	injector/    — Injects MCP tools into AI requests, intercepts tool_calls
//	executor/    — Executes tool calls via MCP servers
//	oauth/       — OAuth server for MCP authorization
//	session/     — MCP session management
//	client/      — MCP protocol clients (stdio, SSE, inline)
//	inline/      — Built-in inline MCP tools (calculator, echo)
//	auth/        — MCP-specific authorization and audit
//	registry/    — MCP tool registry
//	metrics/     — MCP metrics collection
//	tool_config/ — Tool configuration types
//	toolcall/    — Tool call types
//
// The MCP injector runs as middleware (middleware/mcp.go) and intercepts both
// the outbound request (injecting tool definitions) and the inbound response
// (executing tool calls in a loop until all tool calls resolve).
package mcp
