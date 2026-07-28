-- Prompt templates queries.

-- name: CreatePrompt :execresult
INSERT INTO prompts (name, description, version, content, is_active, labels)
VALUES (?, ?, ?, ?, ?, ?);

-- name: CreatePromptVersion :exec
INSERT INTO prompt_versions (prompt_id, version, content, change_summary)
VALUES (?, ?, ?, ?);

-- name: GetPromptTemplate :one
SELECT id, name, description, version, content, is_active, labels, created_at, updated_at
FROM prompts WHERE id = ?;

-- name: GetPromptTemplateByName :one
SELECT id, name, description, version, content, is_active, labels, created_at, updated_at
FROM prompts WHERE name = ?;

-- name: ListPromptTemplates :many
SELECT id, name, description, version, content, is_active, labels, created_at, updated_at
FROM prompts ORDER BY name;

-- name: UpdatePrompt :exec
UPDATE prompts SET name = ?, description = ?, version = ?, content = ?,
    is_active = ?, labels = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeletePromptTemplate :execrows
DELETE FROM prompts WHERE id = ?;

-- name: GetPromptTemplateVersions :many
SELECT id, prompt_id, version, content, change_summary, created_at
FROM prompt_versions WHERE prompt_id = ? ORDER BY id DESC;
