-- Config settings queries.

-- name: GetConfigSetting :one
SELECT value FROM config_settings WHERE scope = ? AND scope_id = ? AND field = ?;
