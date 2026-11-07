-- name: ListWebhooks :many
SELECT * FROM webhooks ORDER BY created_at DESC, id DESC;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE id = $1;

-- name: GetWebhookForUpdate :one
-- For deleting: conflicts with the key share a delivery insert takes.
SELECT * FROM webhooks WHERE id = $1 FOR UPDATE;

-- name: GetWebhookForNoKeyUpdate :one
-- For edits, which never change the key. Unlike FOR UPDATE it does not wait for
-- (or block) the key share a delivery insert takes on the row, so an edit and a
-- domain write that emits an event cannot deadlock.
SELECT * FROM webhooks WHERE id = $1 FOR NO KEY UPDATE;

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

-- name: GetAttempt :one
-- The delivery with what an attempt needs from its webhook, read at attempt time.
SELECT d.id, d.status, d.payload, w.url, w.secret, w.enabled, (d.payload ->> 'type')::text AS event_type
FROM webhook_deliveries d
JOIN webhooks w ON w.id = d.webhook_id
WHERE d.id = @id;

-- name: CancelDelivery :exec
UPDATE webhook_deliveries
SET status = 'canceled', last_error = @reason, next_attempt_at = NULL, finished_at = @now
WHERE id = @id AND status IN ('pending', 'retrying');

-- name: RecordAttempt :one
-- Stores the outcome of an attempt on a delivery that is still unfinished.
UPDATE webhook_deliveries
SET status = @status, attempts = @attempts, response_status = @response_status, last_error = @last_error,
    duration_ms = @duration_ms, last_attempt_at = @last_attempt_at, next_attempt_at = @next_attempt_at,
    finished_at = @finished_at
WHERE id = @id AND status IN ('pending', 'retrying')
RETURNING webhook_id, resource, created_at;

-- name: DisableWebhookForFailures :execrows
-- Turns an enabled webhook off after a failed delivery created at @since: no
-- event delivery succeeded at or after that time. One statement, so a
-- concurrent edit of the row is seen as it is.
UPDATE webhooks SET enabled = false, disabled_reason = 'failures', disabled_at = @now, updated_at = @now
WHERE webhooks.id = @webhook_id AND enabled
  AND NOT EXISTS (
      SELECT 1 FROM webhook_deliveries
      WHERE webhook_id = @webhook_id AND status = 'succeeded' AND resource <> 'test' AND finished_at >= @since
  );

-- name: InsertTestDelivery :exec
INSERT INTO webhook_deliveries (id, webhook_id, resource, action, payload, status, attempts, response_status,
                                last_error, duration_ms, created_at, last_attempt_at, finished_at)
VALUES (@id, @webhook_id, 'test', 'test', @payload, @status, 1, @response_status, @last_error, @duration_ms,
        @created_at, @last_attempt_at, @finished_at);

-- name: CleanupDeliveries :execrows
-- Keeps the newest 50 finished deliveries of every webhook; unfinished ones stay.
DELETE FROM webhook_deliveries
WHERE id IN (
    SELECT ranked.id FROM (
        SELECT id, row_number() OVER (PARTITION BY webhook_id ORDER BY created_at DESC, id DESC) AS n
        FROM webhook_deliveries
        WHERE status IN ('succeeded', 'failed', 'canceled')
    ) ranked
    WHERE n > 50
);
