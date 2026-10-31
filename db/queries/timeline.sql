-- name: TimelineForward :many
-- Comments and change events of one issue, oldest first, keyset on (at, id).
SELECT t.kind::text AS kind, t.id::uuid AS id, t.at::timestamptz AS at FROM (
    SELECT 'comment' AS kind, c.id, c.created_at AS at FROM comments c WHERE c.issue_id = @issue_id::uuid
    UNION ALL
    SELECT 'event', a.id, a.created_at FROM audit_events a WHERE a.entity_type = 'issue' AND a.entity_id = @issue_id::uuid
) t
WHERE sqlc.narg(after_at)::timestamptz IS NULL OR (t.at, t.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid)
ORDER BY t.at, t.id
LIMIT @lim;

-- name: TimelineBackward :many
SELECT t.kind::text AS kind, t.id::uuid AS id, t.at::timestamptz AS at FROM (
    SELECT 'comment' AS kind, c.id, c.created_at AS at FROM comments c WHERE c.issue_id = @issue_id::uuid
    UNION ALL
    SELECT 'event', a.id, a.created_at FROM audit_events a WHERE a.entity_type = 'issue' AND a.entity_id = @issue_id::uuid
) t
WHERE sqlc.narg(before_at)::timestamptz IS NULL OR (t.at, t.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid)
ORDER BY t.at DESC, t.id DESC
LIMIT @lim;
