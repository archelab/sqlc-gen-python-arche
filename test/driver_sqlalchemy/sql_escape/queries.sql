-- MAJOR-1: the F1 body rewrite escapes EVERY colon, not just casts.
--  * `count(*)::bigint`            -> a `::` cast              -> `\\:\\:`
--  * the literal string 'a:b'      -> a NON-CAST literal colon -> 'a\\:b'
--  * `$1`                          -> the bind placeholder     -> `:p1`

-- name: CountByLiteralColon :one
SELECT count(*)::bigint AS c
FROM label_row
WHERE label = 'a:b'
  AND label_row_id = sqlc.arg(label_row_id)::bigint;
