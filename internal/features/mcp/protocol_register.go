package mcp

// Blank-import every MCP protocol version implementation so their init()
// functions register with the protocol package's Negotiate/Register
// registry (see protocol/version.go's package doc for why this indirection
// exists — a direct import from protocol back to these subpackages would
// create an import cycle, since each subpackage must import protocol for
// the Version interface it implements).
//
// This is the only place in the codebase that needs to know all 3 versions
// exist by name; everything else goes through protocol.Negotiate.
import (
	_ "github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20241105"
	_ "github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20250326"
	_ "github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20260728"
)
