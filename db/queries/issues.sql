-- name: CreateIssue :one
INSERT INTO issues (project_id, number, title, description, state_id, priority, assignee_id, creator_id,
    parent_id, due_date, sort_order, created_at, updated_at, completed_at, canceled_at, milestone_id)
VALUES (@project_id, @number, @title, @description, @state_id, @priority, @assignee_id, @creator_id,
    @parent_id, @due_date, @sort_order, @now, @now, @completed_at, @canceled_at, @milestone_id)
RETURNING *;

-- name: InsertIssueNumber :exec
INSERT INTO issue_numbers (project_id, number, issue_id) VALUES (@project_id, @number, @issue_id);

-- name: GetIssue :one
-- Live issue with its project's current key.
SELECT sqlc.embed(issues), p.key AS project_key
FROM issues JOIN projects p ON p.id = issues.project_id
WHERE issues.id = $1 AND issues.deleted_at IS NULL;

-- name: GetIssueForUpdate :one
SELECT sqlc.embed(issues), p.key AS project_key
FROM issues JOIN projects p ON p.id = issues.project_id
WHERE issues.id = $1 AND issues.deleted_at IS NULL
FOR UPDATE OF issues;

-- name: GetIssueByIdentifier :one
-- KEY-N through every key and number the issue has had.
SELECT sqlc.embed(issues), p.key AS project_key
FROM project_keys k
JOIN issue_numbers n ON n.project_id = k.project_id AND n.number = @number
JOIN issues ON issues.id = n.issue_id
JOIN projects p ON p.id = issues.project_id
WHERE k.key = @key AND issues.deleted_at IS NULL;

-- name: ListIssuesByIDs :many
SELECT sqlc.embed(issues), p.key AS project_key
FROM issues JOIN projects p ON p.id = issues.project_id
WHERE issues.id = ANY(@ids::uuid[]) AND issues.deleted_at IS NULL;

-- name: UpdateIssue :one
UPDATE issues SET
    title = @title, description = @description, state_id = @state_id, priority = @priority,
    assignee_id = @assignee_id, parent_id = @parent_id, due_date = @due_date, sort_order = @sort_order,
    completed_at = @completed_at, canceled_at = @canceled_at, milestone_id = @milestone_id, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: SoftDeleteIssue :exec
UPDATE issues SET deleted_at = @now, parent_id = NULL, updated_at = @now WHERE id = @id;

-- name: DetachChildren :many
UPDATE issues SET parent_id = NULL, updated_at = @now
WHERE parent_id = @parent_id AND deleted_at IS NULL
RETURNING id;

-- name: IssueAncestorIDs :many
-- The issue itself and every ancestor up the parent chain.
WITH RECURSIVE anc (id, parent_id, depth) AS (
    SELECT i.id, i.parent_id, 0 FROM issues i WHERE i.id = @id::uuid
    UNION ALL
    SELECT i.id, i.parent_id, anc.depth + 1 FROM issues i JOIN anc ON i.id = anc.parent_id WHERE anc.depth < 100000
)
SELECT anc.id FROM anc;

-- name: MinSortOrder :one
-- Sort order for a new issue on top: min - 1000, or 0 in an empty project.
SELECT COALESCE(min(sort_order) - 1000, 0)::float8 FROM issues WHERE project_id = $1 AND deleted_at IS NULL;

-- name: GetDefaultState :one
SELECT * FROM states WHERE project_id = $1 AND is_default;

-- name: SubIssueProgress :many
SELECT c.parent_id,
    count(*) FILTER (WHERE st.type = 'completed') AS completed,
    count(*) FILTER (WHERE st.type <> 'canceled') AS total
FROM issues c JOIN states st ON st.id = c.state_id
WHERE c.parent_id = ANY(@parent_ids::uuid[]) AND c.deleted_at IS NULL
GROUP BY c.parent_id;

-- name: LockLabelsForShare :many
-- Labels and their groups, share-locked: a concurrent regroup or exclusivity
-- change (which locks them for update) waits for this issue write.
SELECT * FROM labels
WHERE id = ANY(@ids::uuid[])
   OR id IN (SELECT l.parent_id FROM labels l WHERE l.id = ANY(@ids::uuid[]) AND l.parent_id IS NOT NULL)
ORDER BY id
FOR SHARE;

-- name: IssueLabelIDs :many
SELECT issue_id, label_id FROM issue_labels WHERE issue_id = ANY(@issue_ids::uuid[]) ORDER BY issue_id, label_id;

-- name: DeleteIssueLabels :exec
DELETE FROM issue_labels WHERE issue_id = $1;

-- name: InsertIssueLabels :exec
-- exclusive_group_id is the label's group when that group is exclusive.
INSERT INTO issue_labels (issue_id, label_id, exclusive_group_id)
SELECT @issue_id::uuid, l.id, CASE WHEN g.exclusive THEN g.id END
FROM labels l LEFT JOIN labels g ON g.id = l.parent_id
WHERE l.id = ANY(@label_ids::uuid[]);
