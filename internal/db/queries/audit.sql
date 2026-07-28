-- name: LogConfigCreate :exec
INSERT INTO config_audit_log (entity_type, entity_id, action, new_values, performed_by)
VALUES (?, ?, 'create', ?, ?);

-- name: LogConfigUpdate :exec
INSERT INTO config_audit_log (entity_type, entity_id, action, old_values, new_values, performed_by)
VALUES (?, ?, 'update', ?, ?, ?);

-- name: LogConfigDelete :exec
INSERT INTO config_audit_log (entity_type, entity_id, action, old_values, performed_by)
VALUES (?, ?, 'delete', ?, ?);
