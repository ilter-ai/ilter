-- runtime_config queries
-- Runtime configuration key-value store for feature flags, routing, and provider configs.

-- name: GetConfig :one
SELECT section, key, value, version, COALESCE(updated_by, '') AS updated_by
FROM runtime_config
WHERE section = ? AND key = ?;

-- name: GetConfigSection :many
SELECT key, value
FROM runtime_config
WHERE section = ?
ORDER BY key;

-- name: GetAllConfig :many
SELECT section, key, value
FROM runtime_config
ORDER BY section, key;

-- name: UpsertConfig :exec
INSERT INTO runtime_config (section, key, value, version, updated_by)
VALUES (?, ?, ?, 1, ?)
ON CONFLICT(section, key) DO UPDATE SET
    value = excluded.value,
    version = version + 1,
    updated_at = datetime('now'),
    updated_by = excluded.updated_by;

-- name: DeleteConfig :exec
DELETE FROM runtime_config
WHERE section = ? AND key = ?;

-- name: DeleteConfigSection :exec
DELETE FROM runtime_config
WHERE section = ?;
