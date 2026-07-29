-- +goose Up

-- Per-server MCP protocol version pin for outbound negotiation (ilter as
-- MCP client connecting to a registered downstream server). 'auto' means
-- negotiate: try the newest version ilter supports first, fall back to
-- older ones the downstream server actually accepts. An admin may pin an
-- exact version ("2024-11-05" | "2025-03-26" | "2026-07-28") to skip
-- negotiation and force a specific version, e.g. for a server known to
-- misbehave on discovery probes.
ALTER TABLE mcp_servers ADD COLUMN protocol_version TEXT NOT NULL DEFAULT 'auto';

-- Backing store for the 2026-07-28 `io.modelcontextprotocol/tasks`
-- extension: a tools/call on a 2026-07-28 session that runs past a
-- configurable threshold is promoted to a background task instead of
-- blocking the HTTP request. The client polls tasks/get and, for a task
-- that pauses in the input_required state (MRTR pattern), answers via
-- tasks/update.
CREATE TABLE IF NOT EXISTS mcp_tasks (
    id TEXT PRIMARY KEY,
    key_id TEXT DEFAULT '',
    server_id TEXT DEFAULT '',
    tool_name TEXT NOT NULL,
    arguments JSON DEFAULT '{}',
    -- pending | running | input_required | completed | failed
    status TEXT NOT NULL DEFAULT 'pending',
    -- NOT NULL DEFAULT '' (not SQL NULL) so json.RawMessage can scan these
    -- columns directly without every read site needing a nullable-JSON
    -- wrapper type; an empty string means "no result/payload yet".
    result JSON NOT NULL DEFAULT '',
    input_required_payload JSON NOT NULL DEFAULT '',
    error_message TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mcp_tasks_expires_at ON mcp_tasks(expires_at);
CREATE INDEX IF NOT EXISTS idx_mcp_tasks_status ON mcp_tasks(status);

-- Carries the connecting client's hinted/intended MCP protocol version
-- through the whole OAuth PKCE flow (/authorize -> /token), so
-- oauth_endpoints.go can dispatch to the correct per-version OAuthPolicy
-- (DCR-only / DCR+PKCE / DCR+CIMD+iss) consistently across both steps —
-- per the explicit product decision to force OAuth apart per version
-- rather than unify it.
ALTER TABLE oauth_requests ADD COLUMN protocol_version TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE oauth_codes ADD COLUMN protocol_version TEXT NOT NULL DEFAULT 'auto';
