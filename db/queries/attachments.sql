-- name: InsertBlob :exec
INSERT INTO blobs (sha256, size, content_type, created_at) VALUES (@sha256, @size, @content_type, @now)
ON CONFLICT (sha256) DO NOTHING;

-- name: BlobExists :one
SELECT EXISTS (SELECT 1 FROM blobs WHERE sha256 = $1);

-- name: AnyBlob :one
SELECT EXISTS (SELECT 1 FROM blobs);

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

-- name: AttachmentsUsedInText :many
-- Whether the issue's description or one of its comments holds the
-- attachment's download path (what IDsInMarkdown links), in any case.
SELECT a.id,
  strpos(lower(i.description), '/api/attachments/' || a.id::text || '/') > 0
  OR EXISTS (SELECT 1 FROM comments c WHERE c.issue_id = a.issue_id
             AND strpos(lower(c.body), '/api/attachments/' || a.id::text || '/') > 0) AS used
FROM attachments a JOIN issues i ON i.id = a.issue_id
WHERE a.issue_id = ANY(@issue_ids::uuid[]);

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

-- name: DeleteOrphanAttachmentAudit :execrows
-- Events without a project of attachments that are gone: written for pending
-- uploads by earlier versions; a project delete misses those of uploads that
-- were never linked.
DELETE FROM audit_events e
WHERE e.entity_type = 'attachment' AND e.project_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM attachments a WHERE a.id = e.entity_id);
