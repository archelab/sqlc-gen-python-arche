-- name: UpdateWidgetsByStatus :many
UPDATE widget
SET status = sqlc.arg(new_status)::text
WHERE status = sqlc.arg(old_status)::text
RETURNING widget_id, name, status;

-- name: DeleteWidgetIds :many
DELETE FROM widget
WHERE status = sqlc.arg(status)::text
RETURNING widget_id;

-- name: InsertWidgetReturning :many
INSERT INTO widget (widget_id, name, status)
VALUES (sqlc.arg(widget_id)::bigint, sqlc.arg(name)::text, sqlc.arg(status)::text)
RETURNING widget_id, name, status;

-- name: DeleteWidgetsViaCTE :many
WITH del AS (
    DELETE FROM widget
    WHERE status = sqlc.arg(status)::text
    RETURNING widget_id, name, status
)
SELECT widget_id, name, status FROM del;
