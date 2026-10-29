-- name: CreateInvite :one
INSERT INTO invites (token_hash, role, created_by, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetInviteForUpdate :one
SELECT * FROM invites WHERE token_hash = $1 FOR UPDATE;

-- name: MarkInviteUsed :exec
UPDATE invites SET used_at = sqlc.arg(now)::timestamptz, used_by = $2 WHERE id = $1;

-- name: ListActiveInvites :many
SELECT * FROM invites
WHERE used_at IS NULL AND revoked_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
ORDER BY created_at DESC;

-- name: RevokeInvite :execrows
UPDATE invites SET revoked_at = now() WHERE id = $1 AND used_at IS NULL AND revoked_at IS NULL;
