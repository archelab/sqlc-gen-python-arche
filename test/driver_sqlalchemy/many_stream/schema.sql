-- Minimal repro of the reference file_attachment surface for the :many SELECT
-- shape (C-#12). The native SQLAlchemy :many is an async generator:
-- `async def f(...) -> collections.abc.AsyncIterator[Row]:` with
-- `result = await self._conn.stream(...)` + `async for row in result: yield
-- Row(...)`, byte-modelled on the reference list_expired_file_attachments query
-- (file_attachment.py). Includes a scalar :many to prove the
-- bare-value yield path too.
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
