-- name: InsertAuditEvent :exec
INSERT INTO audit_events (entity_type, entity_id, project_id, actor_id, action, changes, created_at)
VALUES (@entity_type, @entity_id, @project_id, @actor_id, @action, @changes, @created_at);

-- name: ListAuditEventsForward :many
-- Oldest first; after_at/after_id is the keyset cursor (both NULL on the first page).
SELECT * FROM audit_events
WHERE entity_id = @entity_id
  AND (sqlc.narg(after_at)::timestamptz IS NULL OR (created_at, id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at, id
LIMIT @lim;

-- name: ListAuditEventsBackward :many
SELECT * FROM audit_events
WHERE entity_id = @entity_id
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: DeleteProjectAudit :exec
-- Events of the project, its states and milestones, the issues currently in
-- it and their comments; events of comments already deleted while in this
-- project go too.
DELETE FROM audit_events
WHERE entity_id IN (SELECT i.id FROM issues i WHERE i.project_id = @project_id::uuid)
   OR (entity_type IN ('project', 'state', 'milestone') AND audit_events.project_id = @project_id::uuid)
   OR (entity_type = 'comment' AND (
        entity_id IN (SELECT c.id FROM comments c JOIN issues i ON i.id = c.issue_id WHERE i.project_id = @project_id::uuid)
     OR (audit_events.project_id = @project_id::uuid AND NOT EXISTS (SELECT 1 FROM comments c WHERE c.id = audit_events.entity_id))));
