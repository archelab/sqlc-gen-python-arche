-- name: GetEntityTags :one
-- A scalar (single-column) :one whose column is an array must render
-- `list[str] | None` (emit_list_arrays:true), NOT `str | None`.
SELECT tag_ids
FROM entity
WHERE entity_id = sqlc.arg(entity_id)::bigint;

-- name: ListEntityTags :many
-- A scalar (single-column) :many SELECT whose column is an array streams
-- `collections.abc.AsyncIterator[list[str]]`, NOT `AsyncIterator[str]`.
SELECT tag_ids
FROM entity;
