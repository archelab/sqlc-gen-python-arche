-- Only used_table is referenced; unused_table is never queried, so with
-- omit_unused_models:true its model class must be dropped from models.py.
-- name: GetUsed :one
SELECT used_id, label
FROM used_table
WHERE used_id = sqlc.arg(uid)::integer;
