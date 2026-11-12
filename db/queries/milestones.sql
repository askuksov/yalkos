-- name: CreateMilestone :one
INSERT INTO milestones (project_id, name, description, target_date, lead_id, sort_order, created_by, created_at, updated_at)
VALUES (@project_id, @name, @description, @target_date, @lead_id,
    (SELECT COALESCE(max(sort_order), 0) + 1000 FROM milestones WHERE project_id = @project_id),
    @created_by, @now, @now)
RETURNING *;

-- name: GetMilestone :one
SELECT * FROM milestones WHERE id = $1;

-- name: GetMilestoneForUpdate :one
SELECT * FROM milestones WHERE id = $1 FOR UPDATE;

-- name: ListMilestonesByIDs :many
SELECT * FROM milestones WHERE id = ANY(@ids::uuid[]);

-- name: ListProjectMilestones :many
SELECT * FROM milestones WHERE project_id = $1 ORDER BY sort_order, id;

-- name: ListMilestonesByProjects :many
SELECT * FROM milestones WHERE project_id = ANY(@project_ids::uuid[]) ORDER BY project_id, sort_order, id;

-- name: ListAllMilestones :many
SELECT m.* FROM milestones m JOIN projects p ON p.id = m.project_id
WHERE @include_archived::boolean OR p.archived_at IS NULL
ORDER BY m.created_at, m.id;

-- name: UpdateMilestone :one
UPDATE milestones SET
    name = @name, description = @description, target_date = @target_date, lead_id = @lead_id,
    status_manual = @status_manual, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: SetMilestoneSortOrder :one
-- Sets the order and moves updated_at; returns the values before the write.
WITH old AS (
    SELECT m.id, m.sort_order, m.updated_at FROM milestones m WHERE m.id = @id FOR UPDATE
)
UPDATE milestones SET sort_order = @sort_order, updated_at = @now
FROM old
WHERE milestones.id = old.id
RETURNING old.sort_order AS old_sort_order, old.updated_at AS old_updated_at;

-- name: DeleteMilestone :exec
DELETE FROM milestones WHERE id = $1;

-- name: ClearIssuesMilestone :many
-- Live issues of the milestone lose it, each comes back with its previous
-- updated_at; deleted ones are cleared by the FK.
WITH old AS (
    SELECT i.id, i.updated_at FROM issues i WHERE i.milestone_id = @milestone_id AND i.deleted_at IS NULL FOR UPDATE
)
UPDATE issues SET milestone_id = NULL, updated_at = @now
FROM old
WHERE issues.id = old.id
RETURNING issues.id, old.updated_at AS old_updated_at;

-- name: MilestoneProgress :many
-- Per milestone over live issues: completed, non-canceled (total) and started-or-completed (active).
SELECT i.milestone_id::uuid AS milestone_id,
    count(*) FILTER (WHERE st.type = 'completed')::int AS completed,
    count(*) FILTER (WHERE st.type <> 'canceled')::int AS total,
    count(*) FILTER (WHERE st.type IN ('started', 'completed'))::int AS active
FROM issues i JOIN states st ON st.id = i.state_id
WHERE i.milestone_id = ANY(@ids::uuid[]) AND i.deleted_at IS NULL
GROUP BY i.milestone_id;
