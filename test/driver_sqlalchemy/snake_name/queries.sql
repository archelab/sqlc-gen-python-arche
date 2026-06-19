-- name: get_thing_by_id :one
SELECT thing_id, label FROM thing WHERE thing_id = sqlc.arg(thing_id)::bigint;

-- name: count_things :one
SELECT count(*)::bigint AS thing_count FROM thing;
