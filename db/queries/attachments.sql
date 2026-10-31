-- name: InsertBlob :exec
INSERT INTO blobs (sha256, size, content_type, created_at) VALUES (@sha256, @size, @content_type, @now)
ON CONFLICT (sha256) DO NOTHING;

-- name: BlobExists :one
SELECT EXISTS (SELECT 1 FROM blobs WHERE sha256 = $1);

-- name: CreateAttachment :one
INSERT INTO attachments (issue_id, uploader_id, blob_sha256, name, created_at)
VALUES (@issue_id, @uploader_id, @blob_sha256, @name, @now)
RETURNING *;

-- name: GetAttachment :one
SELECT sqlc.embed(attachments), b.size, b.content_type
FROM attachments JOIN blobs b ON b.sha256 = attachments.blob_sha256
WHERE attachments.id = $1;

-- name: GetAttachmentForUpdate :one
SELECT * FROM attachments WHERE id = $1 FOR UPDATE;

-- name: ListIssueAttachments :many
SELECT sqlc.embed(attachments), b.size, b.content_type
FROM attachments JOIN blobs b ON b.sha256 = attachments.blob_sha256
WHERE attachments.issue_id = ANY(@issue_ids::uuid[])
ORDER BY attachments.created_at, attachments.id;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = $1;

-- name: LinkPendingAttachments :many
-- Only the actor's own pending uploads; anything else stays untouched.
UPDATE attachments a SET issue_id = @issue_id
FROM blobs b
WHERE b.sha256 = a.blob_sha256 AND a.id = ANY(@ids::uuid[]) AND a.issue_id IS NULL AND a.uploader_id = @uploader_id
RETURNING a.id, a.name, b.size;

-- name: DeleteOldPendingAttachments :many
DELETE FROM attachments WHERE issue_id IS NULL AND created_at < @before RETURNING id;

-- name: UnreferencedBlobs :many
SELECT b.sha256 FROM blobs b
WHERE b.created_at < @before AND NOT EXISTS (SELECT 1 FROM attachments a WHERE a.blob_sha256 = b.sha256);

-- name: DeleteBlobIfUnreferenced :execrows
DELETE FROM blobs b WHERE b.sha256 = @sha256
  AND NOT EXISTS (SELECT 1 FROM attachments a WHERE a.blob_sha256 = b.sha256);
