-- name: CountFileAttachmentsByExtractionId :one
SELECT count(*)::bigint AS attachment_count
FROM file_attachment
WHERE extraction_id = sqlc.arg(extraction_id)::text;

-- name: GetFileAttachmentByUploadId :one
SELECT file_attachment_id, upload_id, chat_id, extraction_id, user_id, created_at, expires_at
FROM file_attachment
WHERE upload_id = sqlc.arg(upload_id)::text;

-- name: DeleteFileAttachmentsByIds :execrows
DELETE FROM file_attachment
WHERE file_attachment_id = ANY(sqlc.arg(file_attachment_ids)::bigint[]);

-- name: TouchFileAttachment :exec
UPDATE file_attachment
SET expires_at = sqlc.arg(expires_at)::timestamptz
WHERE file_attachment_id = sqlc.arg(file_attachment_id)::bigint;

-- name: InsertFileAttachment :one
INSERT INTO file_attachment (
    upload_id,
    chat_id,
    extraction_id,
    user_id,
    created_at,
    expires_at
)
VALUES (
    sqlc.arg(upload_id)::text,
    sqlc.arg(chat_id)::text,
    sqlc.arg(extraction_id)::text,
    sqlc.arg(user_id)::text,
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(expires_at)::timestamptz
)
RETURNING file_attachment_id, upload_id, chat_id, extraction_id, user_id, created_at, expires_at;
