-- API keys queries.

-- name: CreateAPIKey :exec
INSERT INTO api_keys (
    id, name, hashed_key, salt, key_prefix, group_id, user_id, tags,
    monthly_budget_usd, monthly_budget_tokens,
    rate_limit_rpm, rate_limit_tpm, rate_limit_retry_after,
    allowed_models, allowed_providers, enabled,
    created_at, updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?,
    ?, ?,
    ?, ?, ?,
    ?, ?, ?,
    ?, ?
);

-- name: GetAPIKey :one
SELECT id, name, group_id, user_id, tags,
    monthly_budget_usd, monthly_budget_tokens,
    rate_limit_rpm, rate_limit_tpm, rate_limit_retry_after,
    allowed_models, allowed_providers, enabled,
    created_at, updated_at
FROM api_keys
WHERE id = ?;

-- name: GetAPIKeyByHash :one
SELECT id, name, group_id, user_id, tags,
    monthly_budget_usd, monthly_budget_tokens,
    rate_limit_rpm, rate_limit_tpm, rate_limit_retry_after,
    allowed_models, allowed_providers, enabled,
    created_at, updated_at
FROM api_keys
WHERE hashed_key = ?;

-- name: GetAPIKeyWithHash :one
SELECT id, name, group_id, user_id, tags,
    monthly_budget_usd, monthly_budget_tokens,
    rate_limit_rpm, rate_limit_tpm, rate_limit_retry_after,
    allowed_models, allowed_providers, enabled,
    created_at, updated_at, hashed_key, salt
FROM api_keys
WHERE key_prefix = ? AND salt != 'sha256' AND salt IS NOT NULL AND salt != '';

-- name: GetAPIKeyWithHashNoPrefix :one
SELECT id, name, group_id, user_id, tags,
    monthly_budget_usd, monthly_budget_tokens,
    rate_limit_rpm, rate_limit_tpm, rate_limit_retry_after,
    allowed_models, allowed_providers, enabled,
    created_at, updated_at, hashed_key, salt
FROM api_keys
WHERE (key_prefix IS NULL OR key_prefix = '') AND salt != 'sha256' AND salt IS NOT NULL AND salt != '';

-- name: ListAPIKeys :many
SELECT id, name, group_id, user_id, tags,
    monthly_budget_usd, monthly_budget_tokens,
    rate_limit_rpm, rate_limit_tpm, rate_limit_retry_after,
    allowed_models, allowed_providers, enabled,
    created_at, updated_at
FROM api_keys
ORDER BY created_at DESC;

-- name: ListAPIKeysByGroup :many
SELECT id, name, group_id, user_id, tags,
    monthly_budget_usd, monthly_budget_tokens,
    rate_limit_rpm, rate_limit_tpm, rate_limit_retry_after,
    allowed_models, allowed_providers, enabled,
    created_at, updated_at
FROM api_keys
WHERE group_id = ?
ORDER BY created_at DESC;

-- name: UpdateAPIKey :exec
UPDATE api_keys SET
    name = ?, group_id = ?, user_id = ?, tags = ?,
    monthly_budget_usd = ?, monthly_budget_tokens = ?,
    rate_limit_rpm = ?, rate_limit_tpm = ?,
    allowed_models = ?, allowed_providers = ?,
    enabled = ?, updated_at = ?
WHERE id = ?;

-- name: SetKeyRateLimit :exec
UPDATE api_keys SET rate_limit_rpm = ?, rate_limit_retry_after = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteAPIKey :exec
DELETE FROM api_keys WHERE id = ?;

-- name: GetAPIKeyTeamOrg :one
SELECT team_id, org_id FROM api_keys WHERE id = ?;

-- name: CountAPIKeys :one
SELECT COUNT(*), COALESCE(SUM(CASE WHEN enabled = 1 THEN 1 ELSE 0 END), 0) FROM api_keys;
