-- Guardrail rules (CRUD) and guardrail events (write path).

-- name: ListGuardrailRules :many
SELECT id, name, COALESCE(type, 'custom') AS type, mode, severity, enabled,
    COALESCE(target_type, 'global') AS target_type, target_id
FROM guardrail_rules
ORDER BY name;

-- name: GetEnabledGuardrailRules :many
SELECT id, COALESCE(type, 'custom') AS type, patterns, mode, severity,
    COALESCE(target_type, 'global') AS target_type, target_id
FROM guardrail_rules
WHERE enabled = 1
ORDER BY severity DESC, name;

-- name: ToggleGuardrailRule :execrows
UPDATE guardrail_rules SET enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: CreateGuardrailRule :exec
INSERT INTO guardrail_rules (id, name, description, patterns, mode, severity, target_type, target_id, type)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateGuardrailRule :execrows
UPDATE guardrail_rules
SET name = COALESCE(NULLIF(CAST(sqlc.arg(name) AS TEXT), ''), name),
    type = COALESCE(NULLIF(CAST(sqlc.arg(type) AS TEXT), ''), type),
    description = COALESCE(NULLIF(CAST(sqlc.arg(description) AS TEXT), ''), description),
    patterns = CASE WHEN CAST(sqlc.arg(patterns) AS TEXT) = '[]' THEN patterns ELSE CAST(sqlc.arg(patterns) AS TEXT) END,
    mode = COALESCE(NULLIF(CAST(sqlc.arg(mode) AS TEXT), ''), mode),
    severity = COALESCE(NULLIF(CAST(sqlc.arg(severity) AS TEXT), ''), severity),
    enabled = COALESCE(sqlc.narg(enabled), enabled),
    target_type = COALESCE(sqlc.narg(target_type), target_type),
    target_id = COALESCE(sqlc.narg(target_id), target_id),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id);

-- name: DeleteGuardrailRule :execrows
DELETE FROM guardrail_rules WHERE id = ?;

-- name: InsertGuardrailEvent :exec
INSERT INTO guardrail_events (key_id, guardrail_type, action_taken, model, provider, details)
VALUES (?, ?, ?, ?, ?, ?);
