package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
	"github.com/ilter-ai/ilter/internal/version"
)

// rawCaller sends a single raw JSON-RPC request/response round trip,
// abstracting over the transport-specific plumbing (stdin/stdout pending
// map for stdio, HTTP POST for SSE, direct in-process dispatch for
// inline) so negotiateOutbound can drive the handshake identically
// regardless of which transport is negotiating.
type rawCaller func(ctx context.Context, method string, params json.RawMessage) (*JSONRPCResponse, error)

// negotiateOutbound determines which protocol.Version ilter should speak to
// a downstream MCP server (ilter acting as the MCP CLIENT here — the
// inbound direction, ilter as server, is negotiated in gateway.go/hub.go).
//
// A manual pin (server.Config.ProtocolVersion set to an exact version
// rather than "auto") skips negotiation entirely and is used as-is,
// erroring only if the server rejects it outright. Otherwise,
// protocol.Supported is tried newest-first via each version's
// BuildClientHandshake, falling back to the next-older version on
// rejection/error — independent of whatever version the INBOUND client
// connected to ilter with, so ilter can bridge a 2026-07-28 inbound client
// to a downstream server that only understands 2024-11-05, or vice versa.
func negotiateOutbound(ctx context.Context, server *ServerInfo, call rawCaller, sendNotification func(method string, params json.RawMessage)) (protocol.Version, error) {
	if pinned := protocol.ID(server.Config.ProtocolVersion); pinned != "" && pinned != "auto" {
		v := protocol.Negotiate(pinned)
		if err := attemptHandshake(ctx, v, call, sendNotification); err != nil {
			return nil, fmt.Errorf("pinned protocol version %q rejected by server %q: %w", pinned, server.ID, err)
		}
		return v, nil
	}

	var lastErr error
	for _, id := range protocol.Supported {
		v := protocol.Negotiate(id)
		if err := attemptHandshake(ctx, v, call, sendNotification); err != nil {
			lastErr = err
			continue
		}
		return v, nil
	}
	return nil, fmt.Errorf("no supported protocol version accepted by server %q: %w", server.ID, lastErr)
}

// attemptHandshake tries exactly one protocol.Version against the
// downstream server: sends that version's handshake message, checks for a
// JSON-RPC-level error, and lets the version's own ParseServerHandshake
// confirm the response actually indicates support for it (not just "some
// response came back" — a server that doesn't understand server/discover
// at all might still return a generic, differently-shaped success).
func attemptHandshake(ctx context.Context, v protocol.Version, call rawCaller, sendNotification func(method string, params json.RawMessage)) error {
	clientInfo := protocol.ImplementationInfo{Name: "ilter", Version: version.Version}
	method, params, needsInitialize := v.BuildClientHandshake(clientInfo)

	resp, err := call(ctx, method, params)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("handshake error (code %d): %s", resp.Error.Code, resp.Error.Message)
	}
	if err := v.ParseServerHandshake(resp.Result); err != nil {
		return err
	}
	if needsInitialize && sendNotification != nil {
		sendNotification(MethodNotificationsInitialized, nil)
	}
	return nil
}
