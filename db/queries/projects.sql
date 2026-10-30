-- name: CreateProject :one
INSERT INTO projects (workspace_id, key, name, description, icon, color, created_by, created_at, updated_at)
VALUES ((SELECT id FROM workspaces LIMIT 1), @key, @name, @description, @icon, @color, @created_by, @now, @now)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: GetProjectForUpdate :one
SELECT * FROM projects WHERE id = $1 FOR UPDATE;

-- name: GetProjectByKey :one
-- Current or former key.
SELECT p.* FROM projects p JOIN project_keys k ON k.project_id = p.id WHERE k.key = @key;

-- name: ListProjectsForward :many
SELECT sqlc.embed(projects), lower(name)::text AS sort_name FROM projects
WHERE (@include_archived::bool OR archived_at IS NULL)
  AND (sqlc.narg(after_name)::text IS NULL OR (lower(name), id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid))
ORDER BY lower(name), id
LIMIT @lim;

-- name: ListProjectsBackward :many
SELECT sqlc.embed(projects), lower(name)::text AS sort_name FROM projects
WHERE (@include_archived::bool OR archived_at IS NULL)
  AND (sqlc.narg(before_name)::text IS NULL OR (lower(name), id) < (sqlc.narg(before_name)::text, sqlc.narg(before_id)::uuid))
ORDER BY lower(name) DESC, id DESC
LIMIT @lim;

-- name: ListProjectsByIDs :many
SELECT * FROM projects WHERE id = ANY(@ids::uuid[]);

-- name: UpdateProject :one
UPDATE projects
SET key = @key, name = @name, description = @description, icon = @icon, color = @color, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: SetProjectArchived :one
UPDATE projects SET archived_at = @archived_at, updated_at = @now WHERE id = @id RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;

-- name: InsertProjectKey :exec
INSERT INTO project_keys (key, project_id) VALUES (@key, @project_id) ON CONFLICT (key) DO NOTHING;

-- name: GetProjectKeyOwner :one
SELECT project_id FROM project_keys WHERE key = $1;

-- name: NextIssueNumber :one
UPDATE projects SET sequence_counter = sequence_counter + 1 WHERE id = $1 RETURNING sequence_counter;
