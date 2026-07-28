-- Chat conversations and messages (dashboard chat UI).

-- name: ListConversations :many
SELECT
    c.id, c.title, c.created_at, c.updated_at,
    CAST(COALESCE((SELECT content FROM messages WHERE conversation_id = c.id ORDER BY created_at DESC LIMIT 1), '') AS TEXT) AS last_message,
    CAST(COALESCE((SELECT COUNT(*) FROM messages WHERE conversation_id = c.id), 0) AS INTEGER) AS message_count
FROM conversations c
ORDER BY c.updated_at DESC;

-- name: CreateConversation :exec
INSERT INTO conversations (id, title) VALUES (?, ?);

-- name: GetConversation :one
SELECT id, title, created_at, updated_at FROM conversations WHERE id = ?;

-- name: ConversationExists :one
SELECT COUNT(*) FROM conversations WHERE id = ?;

-- name: UpdateConversationTitle :execrows
UPDATE conversations SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: SetConversationTitle :exec
UPDATE conversations SET title = ? WHERE id = ?;

-- name: TouchConversation :exec
UPDATE conversations SET updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: DeleteConversation :execrows
DELETE FROM conversations WHERE id = ?;

-- name: ListMessagesByConversation :many
SELECT id, conversation_id, role, content, model, token_count, cost, reasoning_content, tool_calls, usage_cost, billing_key, created_at
FROM messages
WHERE conversation_id = ?
ORDER BY created_at ASC;

-- name: InsertMessage :execlastid
INSERT INTO messages (conversation_id, role, content, model, token_count, cost, reasoning_content, tool_calls, usage_cost, billing_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMessageCreatedAt :one
SELECT created_at FROM messages WHERE id = ?;

-- name: ListMessagesPaginated :many
SELECT id, conversation_id, role, content, model, token_count, cost, reasoning_content, tool_calls, usage_cost, billing_key, created_at
FROM messages
WHERE conversation_id = sqlc.arg(conversation_id)
  AND (CAST(sqlc.narg(before_id) AS INTEGER) IS NULL OR id < CAST(sqlc.narg(before_id) AS INTEGER))
ORDER BY created_at DESC LIMIT sqlc.arg(limit);
