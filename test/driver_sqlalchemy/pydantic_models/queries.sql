-- name: GetFileAttachment :one
SELECT *
FROM file_attachment
WHERE file_attachment_id = $1;

-- name: GetAuditLog :one
SELECT *
FROM audit_log
WHERE id = $1;
