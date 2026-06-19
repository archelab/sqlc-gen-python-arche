-- Minimal repro of a representative models.py shape (file_attachment / audit_log):
--  * a table WITH a comment      -> class docstring (raw, unconditional)
--  * columns WITH comments       -> `# ...` field comments
--  * nullable columns            -> `T | None`
--  * jsonb                       -> typing.Any (P-09 default; matches the reference
--                                   models.py Optional[Any]). A per-column
--                                   override can still map jsonb to a typed
--                                   pydantic model (see the jsonb_typed case).
--  * a table WITHOUT a comment   -> no class docstring
CREATE TABLE IF NOT EXISTS file_attachment
(
    file_attachment_id bigint,
    upload_id           text        NOT NULL,
    chat_id             text,
    metadata            jsonb,
    created_at          timestamptz NOT NULL
);

COMMENT ON TABLE file_attachment IS 'Authorization + retention metadata for files stored on the shared agent filesystem.';
COMMENT ON COLUMN file_attachment.upload_id IS 'Opaque hex identifier handed to clients/agents; the only lookup key exposed outside the module.';
COMMENT ON COLUMN file_attachment.chat_id IS 'Owning chat conversation when the file is a chat upload; NULL for extraction originals.';
COMMENT ON COLUMN file_attachment.metadata IS 'Typed JSONB metadata blob.';

CREATE TABLE IF NOT EXISTS audit_log
(
    id         bigint      NOT NULL,
    table_name text        NOT NULL,
    old_data   jsonb,
    created_at timestamptz
);
