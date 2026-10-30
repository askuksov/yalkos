-- name: CreateUser :one
INSERT INTO users (workspace_id, email, name, password_hash, role)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower(sqlc.arg(email));

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at, id;

-- name: UpdateUserRole :one
UPDATE users SET role = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: SetUserDisabled :one
UPDATE users SET disabled = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: ListUsersByIDs :many
SELECT * FROM users WHERE id = ANY(@ids::uuid[]);
