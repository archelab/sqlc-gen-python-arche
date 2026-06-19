-- name: GetOrder :one
SELECT order_id, status FROM orders WHERE order_id = sqlc.arg(order_id)::bigint;
