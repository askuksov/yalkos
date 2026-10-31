-- name: ListLabels :many
SELECT * FROM labels ORDER BY position, lower(name), id;

-- name: ListLabelsByIDs :many
SELECT * FROM labels WHERE id = ANY(@ids::uuid[]);

-- name: GetLabel :one
SELECT * FROM labels WHERE id = $1;

-- name: GetLabelForUpdate :one
SELECT * FROM labels WHERE id = $1 FOR UPDATE;

-- name: LockLabelChildren :many
SELECT id FROM labels WHERE parent_id = $1 ORDER BY id FOR UPDATE;

-- name: GetLabelSettings :one
SELECT type_group_id, default_type_label_id FROM workspaces LIMIT 1;

-- name: ListLabelChildren :many
SELECT * FROM labels WHERE parent_id = $1 ORDER BY position, lower(name), id;

-- name: NextLabelPosition :one
-- Bottom of the label's siblings (top level when parent is NULL).
SELECT (COALESCE(max(position), 0) + 1000)::float8 FROM labels WHERE parent_id IS NOT DISTINCT FROM sqlc.narg(parent_id)::uuid;

-- name: CreateLabel :one
INSERT INTO labels (workspace_id, parent_id, is_group, exclusive, name, description, color, position, created_by, created_at, updated_at)
VALUES ((SELECT id FROM workspaces LIMIT 1), @parent_id, @is_group, @exclusive, @name, @description, @color, @position, @created_by, @now, @now)
RETURNING *;

-- name: UpdateLabel :one
UPDATE labels SET
    name = @name, description = @description, color = @color, parent_id = @parent_id,
    exclusive = @exclusive, position = @position, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteLabel :exec
DELETE FROM labels WHERE id = $1;

-- name: CountLabelChildren :one
SELECT count(*)::int FROM labels WHERE parent_id = $1;

-- name: CountMoveConflicts :one
-- Issues that hold the label and another label of the group.
SELECT count(DISTINCT il.issue_id)::int FROM issue_labels il
JOIN issue_labels o ON o.issue_id = il.issue_id AND o.label_id <> il.label_id
JOIN labels ol ON ol.id = o.label_id
WHERE il.label_id = @label_id AND ol.parent_id = @group_id::uuid;

-- name: CountGroupConflicts :one
-- Issues that hold two or more labels of the group.
SELECT count(*)::int FROM (
    SELECT il.issue_id FROM issue_labels il JOIN labels l ON l.id = il.label_id
    WHERE l.parent_id = @group_id::uuid
    GROUP BY il.issue_id HAVING count(*) > 1
) c;

-- name: SetLabelExclusiveGroup :exec
UPDATE issue_labels SET exclusive_group_id = sqlc.narg(group_id)::uuid WHERE label_id = @label_id;

-- name: SetGroupExclusiveGroup :exec
-- exclusive_group_id of every issue label in the group: the group, or NULL when not exclusive.
UPDATE issue_labels SET exclusive_group_id = sqlc.narg(exclusive_group_id)::uuid
WHERE label_id IN (SELECT l.id FROM labels l WHERE l.parent_id = @group_id::uuid);

-- name: IssueLabelSetsWithLabel :many
-- Live issues holding the label with their full sorted label lists.
SELECT i.id AS issue_id, i.project_id, array_agg(il.label_id ORDER BY il.label_id)::uuid[] AS label_ids
FROM issues i JOIN issue_labels il ON il.issue_id = i.id
WHERE i.deleted_at IS NULL AND i.id IN (SELECT x.issue_id FROM issue_labels x WHERE x.label_id = @label_id)
GROUP BY i.id, i.project_id;

-- name: SetDefaultTypeLabel :exec
UPDATE workspaces SET default_type_label_id = sqlc.narg(label_id)::uuid;
