-- Minimal repro of the reference file_attachment surface, exercising the verb
-- method shapes :one (scalar + struct), :exec, :execrows, and a multi-param
-- :one that bundles into a Params struct.
CREATE TABLE IF NOT EXISTS file_attachment
(
    file_attachment_id bigint,
    upload_id           text        NOT NULL,
    chat_id             text,
    extraction_id       text,
    user_id             text        NOT NULL,
    created_at          timestamptz NOT NULL,
    expires_at          timestamptz
);
