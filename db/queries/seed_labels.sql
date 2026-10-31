-- name: SeedTypeLabels :exec
-- The Type group with its four labels; Task becomes the default type.
-- Keep in sync with the seed in migration 00005.
WITH g AS (
    INSERT INTO labels (workspace_id, is_group, exclusive, name, color, position, created_at, updated_at)
    VALUES (@workspace_id, true, true, 'Type', '#6e7781', 0, @now, @now)
    RETURNING id
), l AS (
    INSERT INTO labels (workspace_id, parent_id, name, color, position, created_at, updated_at)
    SELECT @workspace_id, g.id, v.name, v.color, v.pos, @now, @now
    FROM g, (VALUES ('Bug', '#e5484d', 0), ('Feature', '#8e4ec6', 1), ('Task', '#6e7781', 2), ('Improvement', '#30a46c', 3)) AS v (name, color, pos)
    RETURNING id, name
)
UPDATE workspaces
SET type_group_id = (SELECT id FROM g),
    default_type_label_id = (SELECT id FROM l WHERE name = 'Task')
WHERE workspaces.id = @workspace_id;
