-- name: CreateAPIKey :one
INSERT INTO api_keys (user_id, name, key_hash, display_prefix)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAPIKeyUser :one
SELECT sqlc.embed(k), sqlc.embed(u)
FROM api_keys k
JOIN users u ON u.id = k.user_id
WHERE k.key_hash = $1 AND k.revoked_at IS NULL AND NOT u.disabled;

-- name: GetAPIKey :one
SELECT * FROM api_keys WHERE id = $1;

-- name: ListAPIKeys :many
SELECT * FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC;

-- name: RevokeAPIKey :execrows
UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = now()
WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '60 seconds');
