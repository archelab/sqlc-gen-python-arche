-- name: ListExpiredFileAttachments :many
SELECT file_attachment_id, upload_id, chat_id, extraction_id, user_id, created_at, expires_at
FROM file_attachment
WHERE expires_at IS NOT NULL
  AND expires_at < sqlc.arg(now)::timestamptz
ORDER BY expires_at ASC
LIMIT sqlc.arg(limit_count)::integer;

-- name: ListUploadIds :many
SELECT upload_id
FROM file_attachment
ORDER BY upload_id ASC;
