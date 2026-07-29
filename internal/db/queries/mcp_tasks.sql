-- Backing store for the MCP Tasks extension (2026-07-28 only).

-- name: CreateMCPTask :exec
INSERT INTO mcp_tasks (id, key_id, server_id, tool_name, arguments, status, result, input_required_payload, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMCPTask :one
SELECT id, key_id, server_id, tool_name, arguments, status, result,
       input_required_payload, error_message, created_at, updated_at, expires_at
FROM mcp_tasks WHERE id = ?;

-- name: UpdateMCPTaskStatus :exec
UPDATE mcp_tasks
SET status = ?, result = ?, error_message = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateMCPTaskInputRequired :exec
UPDATE mcp_tasks
SET status = 'input_required', input_required_payload = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteExpiredMCPTasks :exec
DELETE FROM mcp_tasks WHERE expires_at < datetime('now');
