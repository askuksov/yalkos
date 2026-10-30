-- name: IssueDescendantIDs :many
-- Live descendants of an issue, parents before children.
WITH RECURSIVE d (id, depth) AS (
    SELECT i.id, 1 FROM issues i WHERE i.parent_id = @id::uuid AND i.deleted_at IS NULL
    UNION ALL
    SELECT i.id, d.depth + 1 FROM issues i JOIN d ON i.parent_id = d.id WHERE i.deleted_at IS NULL AND d.depth < 100000
)
SELECT d.id FROM d ORDER BY d.depth, d.id;

-- name: MoveIssue :one
UPDATE issues SET
    project_id = @project_id, number = @number, state_id = @state_id, parent_id = @parent_id,
    completed_at = @completed_at, canceled_at = @canceled_at, updated_at = @now
WHERE id = @id
RETURNING *;
