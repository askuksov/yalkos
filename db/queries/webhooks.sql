-- name: ListWebhooks :many
SELECT * FROM webhooks ORDER BY created_at DESC, id DESC;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE id = $1;

-- name: GetWebhookForUpdate :one
SELECT * FROM webhooks WHERE id = $1 FOR UPDATE;

-- name: CountWebhooks :one
SELECT count(*) FROM webhooks;

-- name: LockWorkspaceForWebhooks :exec
-- Serializes creations so the per-workspace limit holds under concurrency;
-- NO KEY UPDATE does not block rows that merely reference the workspace.
SELECT id FROM workspaces LIMIT 1 FOR NO KEY UPDATE;

-- name: CreateWebhook :one
INSERT INTO webhooks (workspace_id, name, url, secret, project_id, resources, enabled, created_by, created_at, updated_at)
VALUES ((SELECT id FROM workspaces LIMIT 1), @name, @url, @secret, @project_id, @resources::text[], @enabled, @created_by, @now, @now)
RETURNING *;

-- name: UpdateWebhook :one
UPDATE webhooks SET
    name = @name, url = @url, secret = @secret, project_id = @project_id, resources = @resources::text[],
    enabled = @enabled, disabled_reason = @disabled_reason, disabled_at = @disabled_at, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteWebhook :exec
DELETE FROM webhooks WHERE id = $1;
