-- name: AddProjectMember :exec
INSERT INTO project_members (project_id, user_id, created_at) VALUES (@project_id, @user_id, @now)
ON CONFLICT DO NOTHING;

-- name: RemoveProjectMember :exec
DELETE FROM project_members WHERE project_id = @project_id AND user_id = @user_id;

-- name: ListProjectMemberIDs :many
SELECT user_id FROM project_members WHERE project_id = $1 ORDER BY created_at, user_id;
