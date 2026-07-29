package protocol

import "errors"

// ErrorKind is a semantic, version-independent classification of an MCP
// error condition. Each Version implementation maps a kind to its own
// numeric JSON-RPC error code via Version.ErrorCode, so callers (Gateway/Hub
// dispatch) reason about *what* went wrong without hardcoding a
// version-specific number at every call site.
type ErrorKind int

const (
	ErrToolNotFound ErrorKind = iota
	ErrToolExecution
	ErrNotInitialized
	ErrUnsupportedProtocolVersion
	ErrHeaderMismatch
	ErrMissingRequiredClientCapability
	ErrResourceNotFound
	ErrMethodNotFound
)

// ErrNoInitializeHandshake is returned by Version.HandleInitialize for a
// version that has no `initialize` method at all (v20260728's stateless
// core removed the handshake entirely). Callers should route such clients
// through ValidateRequestMeta/server-discover instead of initialize.
var ErrNoInitializeHandshake = errors.New("protocol: this version has no initialize handshake; use per-request _meta or server/discover")

// ErrHandshakeRejected is returned by ParseServerHandshake when a
// downstream server's response indicates it does not support the version
// that was attempted, signaling the outbound negotiator to fall back to
// the next-older version in Supported.
var ErrHandshakeRejected = errors.New("protocol: downstream server rejected this protocol version")
