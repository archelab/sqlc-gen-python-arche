-- A query joining across the two schema files (customer from file 1, order from
-- file 2), proving the combined catalog resolves both tables.
-- name: GetCustomerOrder :one
SELECT customer.name,
       customer_order.total
FROM customer_order
JOIN customer ON customer.customer_id = customer_order.customer_id
WHERE customer_order.order_id = sqlc.arg(oid)::integer;
