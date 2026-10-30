-- name: CreateState :one
INSERT INTO states (project_id, name, type, color, position, is_default, created_at, updated_at)
VALUES (@project_id, @name, @type, @color, @position, @is_default, @now, @now)
RETURNING *;

-- name: ListStates :many
SELECT * FROM states WHERE project_id = $1 ORDER BY position, id;

-- name: GetState :one
SELECT * FROM states WHERE id = $1;

-- name: ListStatesByIDs :many
SELECT * FROM states WHERE id = ANY(@ids::uuid[]);

-- name: UpdateState :one
UPDATE states SET name = @name, color = @color, is_default = @is_default, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: ClearDefaultState :one
UPDATE states SET is_default = false, updated_at = @now
WHERE project_id = @project_id AND is_default
RETURNING *;

-- name: SetStatePosition :exec
UPDATE states SET position = @position, updated_at = @now WHERE id = @id;

-- name: DeleteState :exec
DELETE FROM states WHERE id = $1;

-- name: CountStates :one
SELECT count(*) FROM states WHERE project_id = $1;

-- name: CountStatesOfType :one
SELECT count(*) FROM states WHERE project_id = @project_id AND type = @type;

-- name: StateHasIssues :one
SELECT EXISTS (SELECT 1 FROM issues WHERE state_id = $1);

-- name: MoveIssuesToState :many
-- Moves every issue (deleted ones too) of from_state to to_state. When
-- keep_times is false, completed_at/canceled_at become the given values.
WITH old AS (
    SELECT i.id, i.completed_at, i.canceled_at FROM issues i WHERE i.state_id = @from_state::uuid FOR UPDATE
)
UPDATE issues SET
    state_id = @to_state::uuid,
    completed_at = CASE WHEN @keep_times::bool THEN issues.completed_at ELSE sqlc.narg(completed_at)::timestamptz END,
    canceled_at = CASE WHEN @keep_times::bool THEN issues.canceled_at ELSE sqlc.narg(canceled_at)::timestamptz END,
    updated_at = @now
FROM old
WHERE issues.id = old.id
RETURNING issues.id, issues.project_id, old.completed_at AS old_completed_at, old.canceled_at AS old_canceled_at,
    issues.completed_at, issues.canceled_at, issues.deleted_at;
