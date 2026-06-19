-- name: GetGadget :one
SELECT gadget_id, label FROM gadget WHERE gadget_id = sqlc.arg(gadget_id)::bigint;
