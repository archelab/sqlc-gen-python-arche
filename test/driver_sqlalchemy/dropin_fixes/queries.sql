-- name: GetEntityByTags :many
-- C: tag_ids result field AND the tags arg both render builtin list[str]
-- (emit_list_arrays:true). A plain SELECT :many STREAMS (AsyncIterator).
SELECT entity_id, tag_ids
FROM entity
WHERE tag_ids && sqlc.arg(tags)::text[];

-- name: ListEntityLinks :many
-- B: two result columns both named entity_id de-collide to
-- entity_id / entity_id_2 (not a duplicate keyword argument).
SELECT e.entity_id, l.entity_id
FROM entity AS e
JOIN link AS l ON l.other_id = e.entity_id;

-- name: LockEntitiesForUpdate :many
-- E: a WITH CTE containing FOR UPDATE is a read-only lock, so this :many must
-- STREAM (AsyncIterator + conn.stream), NOT materialize to list[T].
WITH locked AS (
    SELECT e.entity_id
    FROM entity AS e
    WHERE e.label = sqlc.arg(label)::text
    FOR UPDATE OF e
)
SELECT entity_id
FROM locked;
