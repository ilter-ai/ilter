-- Users queries.

-- name: CreateUser :execresult
INSERT INTO users (name, email, status, metadata, budget, password_hash)
VALUES (?, ?, ?, '{}', ?, ?);

-- name: GetUser :one
SELECT id, name, email, status, password_hash, metadata, budget, daily_limit, created_at, updated_at
FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT id, name, email, status, password_hash, metadata, budget, daily_limit, created_at, updated_at
FROM users WHERE email = ?;

-- name: ListUsers :many
SELECT id, name, email, status, password_hash, metadata, budget, daily_limit, created_at, updated_at
FROM users ORDER BY id;

-- name: GetUserBudget :one
SELECT budget, daily_limit FROM users WHERE id = ?;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = ?;
