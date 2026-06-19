-- name: GetFileAttachmentByUploadId :one
SELECT file_attachment_id, upload_id, chat_id, extraction_id, user_id, created_at, expires_at
FROM file_attachment
WHERE upload_id = sqlc.arg(upload_id)::text;
