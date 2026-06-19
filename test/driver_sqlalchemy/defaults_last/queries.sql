-- A :one returning a Row that interleaves NOT NULL (item_id, quantity) and
-- nullable (note, description) columns. The generated Row must keep them in
-- SELECT/column order: item_id, note, quantity, description — with the nullable
-- ones as plain `T | None` and NO `= None` default appended.
-- name: GetItem :one
SELECT item_id,
       note,
       quantity,
       description
FROM item
WHERE item_id = sqlc.arg(iid)::integer;
