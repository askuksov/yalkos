-- name: CreateState :one
INSERT INTO states (project_id, name, type, color, position, is_default, created_at, updated_at)
VALUES (@project_id, @name, @type, @color, @position, @is_default, @now, @now)
RETURNING *;

-- name: ListStates :many
SELECT * FROM states WHERE project_id = $1 ORDER BY position, id;
