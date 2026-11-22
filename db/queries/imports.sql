-- name: UpsertImportSource :one
INSERT INTO import_sources (kind, source_id, url, workspace, first_run_at, last_run_at)
VALUES (@kind, @source_id, @url, @workspace, @now, @now)
ON CONFLICT (kind, source_id) DO UPDATE
SET url = EXCLUDED.url, workspace = EXCLUDED.workspace, last_run_at = EXCLUDED.last_run_at
RETURNING *;

-- name: ListImportMap :many
SELECT * FROM import_map WHERE source_id = @source_id AND kind = @kind ORDER BY plane_id;

-- name: InsertImportMap :exec
INSERT INTO import_map (source_id, kind, plane_id, target_id, source_updated_at, imported_at)
VALUES (@source_id, @kind, @plane_id, @target_id, @source_updated_at, @now);

-- name: SetImportMapUpdated :exec
UPDATE import_map SET source_updated_at = @source_updated_at
WHERE source_id = @source_id AND kind = @kind AND plane_id = @plane_id;

-- name: ImportCreateUser :one
-- Cannot log in: disabled. The caller passes a hash of a random secret it
-- discards, so a login attempt costs the same as for any other account.
INSERT INTO users (workspace_id, email, name, password_hash, role, disabled)
VALUES (@workspace_id, @email, @name, @password_hash, 'member', true)
RETURNING *;

-- name: ImportCreateProject :one
INSERT INTO projects (workspace_id, key, name, description, created_by, created_at, updated_at)
VALUES ((SELECT id FROM workspaces LIMIT 1), @key, @name, @description, @created_by, @created_at, @updated_at)
RETURNING *;

-- name: ImportCreateState :one
INSERT INTO states (project_id, name, type, color, position, is_default, created_at, updated_at)
VALUES (@project_id, @name, @type, @color, @position, @is_default, @created_at, @updated_at)
RETURNING *;

-- name: ImportCreateLabel :one
-- Never exclusive; a group is a top-level label with is_group.
INSERT INTO labels (workspace_id, parent_id, is_group, exclusive, name, color, position, created_by, created_at, updated_at)
VALUES ((SELECT id FROM workspaces LIMIT 1), @parent_id, @is_group, false, @name, @color, @position, @created_by, @created_at, @updated_at)
RETURNING *;

-- name: ImportCreateMilestone :one
INSERT INTO milestones (project_id, name, description, target_date, lead_id, sort_order, status_manual, created_by, created_at, updated_at)
VALUES (@project_id, @name, @description, @target_date, @lead_id, @sort_order, @status_manual::text, @created_by, @created_at, @updated_at)
RETURNING *;

-- name: ImportCreateIssue :one
INSERT INTO issues (project_id, number, title, description, state_id, priority, assignee_id, creator_id,
    parent_id, due_date, sort_order, created_at, updated_at, completed_at, canceled_at, milestone_id)
VALUES (@project_id, @number, @title, @description, @state_id, @priority, @assignee_id, @creator_id,
    @parent_id, @due_date, @sort_order, @created_at, @updated_at, @completed_at, @canceled_at, @milestone_id)
RETURNING *;

-- name: ImportCreateComment :one
INSERT INTO comments (issue_id, author_id, body, created_at, updated_at, edited_at)
VALUES (@issue_id, @author_id, @body, @created_at, @updated_at, @edited_at)
RETURNING *;

-- name: ImportCreateAttachment :one
INSERT INTO attachments (issue_id, uploader_id, blob_sha256, name, created_at)
VALUES (@issue_id::uuid, @uploader_id, @blob_sha256, @name, @created_at)
RETURNING *;

-- name: RaiseSequenceCounter :exec
-- Never lowers the counter, so the next new issue gets a number above every imported one.
UPDATE projects SET sequence_counter = GREATEST(sequence_counter, @number::int) WHERE id = @id;

-- name: IssueNumberTaken :one
SELECT EXISTS (SELECT 1 FROM issue_numbers WHERE project_id = @project_id AND number = @number::int);
