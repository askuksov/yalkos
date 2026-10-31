-- name: CreateComment :one
INSERT INTO comments (issue_id, author_id, body, created_at, updated_at)
VALUES (@issue_id, @author_id, @body, @now, @now)
RETURNING *;

-- name: GetComment :one
SELECT * FROM comments WHERE id = $1;

-- name: GetCommentForUpdate :one
SELECT * FROM comments WHERE id = $1 FOR UPDATE;

-- name: UpdateComment :one
UPDATE comments SET body = @body, edited_at = @now::timestamptz, updated_at = @now::timestamptz WHERE id = @id RETURNING *;

-- name: DeleteComment :exec
DELETE FROM comments WHERE id = $1;

-- name: ListCommentsByIDs :many
SELECT * FROM comments WHERE id = ANY(@ids::uuid[]);

-- name: ListCommentsForward :many
SELECT * FROM comments
WHERE issue_id = @issue_id
  AND (sqlc.narg(after_at)::timestamptz IS NULL OR (created_at, id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at, id
LIMIT @lim;

-- name: ListCommentsBackward :many
SELECT * FROM comments
WHERE issue_id = @issue_id
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: CommentCounts :many
SELECT issue_id, count(*)::int AS n FROM comments WHERE issue_id = ANY(@issue_ids::uuid[]) GROUP BY issue_id;
