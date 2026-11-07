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

-- name: MatchWebhooks :many
-- Enabled webhooks subscribed to the resource whose scope covers the event:
-- all projects, one of the given projects, or any scope (workspace-wide events).
SELECT * FROM webhooks
WHERE enabled
  AND @resource::text = ANY (resources)
  AND (project_id IS NULL OR @any_scope::boolean OR project_id = ANY (@project_ids::uuid[]))
ORDER BY created_at, id;

-- name: InsertDelivery :one
INSERT INTO webhook_deliveries (webhook_id, resource, action, entity_id, payload, status, created_at)
VALUES (@webhook_id, @resource, @action, @entity_id, @payload, 'pending', @created_at)
RETURNING id;

-- name: ListDeliveries :many
SELECT id, webhook_id, resource, action, entity_id, status, attempts, response_status, last_error, duration_ms,
       created_at, last_attempt_at, next_attempt_at, finished_at
FROM webhook_deliveries
WHERE webhook_id = @webhook_id
ORDER BY created_at DESC, id DESC
LIMIT @max_rows;
