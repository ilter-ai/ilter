-- MCP server registry: servers config and their discovered tools.

-- name: ListMCPServers :many
SELECT id, name, description, transport, url, command, args, env,
       handler, enabled, timeout_ms, max_retries, auth_type, auth_key_env,
       protocol_version
FROM mcp_servers;

-- name: ListMCPTools :many
SELECT name, description, schema
FROM mcp_tools
WHERE server_id = ?
ORDER BY name;

-- name: DeleteMCPToolsByServer :exec
DELETE FROM mcp_tools WHERE server_id = ?;

-- name: UpsertMCPTool :exec
INSERT OR REPLACE INTO mcp_tools (id, server_id, name, description, schema)
VALUES (?, ?, ?, ?, ?);
