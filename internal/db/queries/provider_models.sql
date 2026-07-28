-- Provider models queries.

-- name: GetAllProviderModels :many
SELECT id, provider, model, active, tier, cost_in, cost_out,
    display_name, max_context_tokens, max_output_tokens,
    capabilities, default_base_url, discovered_at
FROM provider_models
ORDER BY provider, model;

-- name: GetActiveProviderModels :many
SELECT id, provider, model, active, tier, cost_in, cost_out,
    display_name, max_context_tokens, max_output_tokens,
    capabilities, default_base_url, discovered_at
FROM provider_models
WHERE active = 1
ORDER BY provider, model;

-- name: GetProviderModels :many
SELECT id, provider, model, active, tier, cost_in, cost_out,
    display_name, max_context_tokens, max_output_tokens,
    capabilities, default_base_url, discovered_at
FROM provider_models
WHERE provider = ?
ORDER BY model;

-- name: ProviderModelCount :one
SELECT COUNT(*) FROM provider_models WHERE provider = ?;

-- name: GetLatestDiscovery :one
SELECT COALESCE(MAX(discovered_at), '') FROM provider_models WHERE provider = ?;

-- name: GetProviderForModel :one
SELECT provider FROM provider_models WHERE model = ? LIMIT 1;

-- name: GetInactiveModels :many
SELECT model FROM provider_models WHERE active = 0;

-- name: GetModelStatuses :many
SELECT model, active FROM provider_models;

-- name: SaveModelStatus :exec
UPDATE provider_models SET active = ? WHERE model = ?;

-- name: SaveModelTier :exec
UPDATE provider_models SET tier = ? WHERE model = ?;
