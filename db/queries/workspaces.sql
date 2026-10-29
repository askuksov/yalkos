-- name: CountWorkspaces :one
SELECT count(*) FROM workspaces;

-- name: CreateWorkspace :one
INSERT INTO workspaces (name) VALUES ($1) RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM workspaces LIMIT 1;
