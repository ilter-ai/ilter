-- Groups and group membership queries.

-- name: CreateGroup :execresult
INSERT INTO groups (name, description, metadata, budget)
VALUES (?, ?, '{}', ?);

-- name: GetGroup :one
SELECT id, name, description, metadata, budget, daily_limit, created_at, updated_at
FROM groups WHERE id = ?;

-- name: GetGroupByName :one
SELECT id, name, description, metadata, budget, daily_limit, created_at, updated_at
FROM groups WHERE name = ?;

-- name: ListGroups :many
SELECT id, name, description, metadata, budget, daily_limit, created_at, updated_at
FROM groups ORDER BY id;

-- name: GetGroupBudget :one
SELECT budget, daily_limit FROM groups WHERE id = ?;

-- name: DeleteGroup :execrows
DELETE FROM groups WHERE id = ?;

-- name: AddUserToGroup :exec
INSERT INTO user_group_memberships (user_id, group_id, role) VALUES (?, ?, ?);

-- name: RemoveUserFromGroup :execrows
DELETE FROM user_group_memberships WHERE user_id = ? AND group_id = ?;

-- name: GetGroupUsers :many
SELECT u.id, u.name, u.email, u.status, u.password_hash, u.metadata, u.budget, u.daily_limit, u.created_at, u.updated_at
FROM users u
JOIN user_group_memberships m ON u.id = m.user_id
WHERE m.group_id = ?
ORDER BY u.id;

-- name: GetUserGroups :many
SELECT g.id, g.name, g.description, g.metadata, g.budget, g.daily_limit, g.created_at, g.updated_at
FROM groups g
JOIN user_group_memberships m ON g.id = m.group_id
WHERE m.user_id = ?
ORDER BY g.id;
