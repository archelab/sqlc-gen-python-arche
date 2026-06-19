-- Un-overridden jsonb in a result row -> typing.Any (the P-09 default).
-- name: GetPayload :one
SELECT widget_id, payload
FROM widget
WHERE widget_id = sqlc.arg(wid)::integer;

-- The overridden jsonb column returned as a SCALAR :one -> the typed model
-- (shapes.WidgetConfig). The :one scalar return reads the value back unwrapped
-- (`return row[0]`), so no construction cast is emitted.
-- name: GetConfig :one
SELECT config
FROM widget
WHERE widget_id = sqlc.arg(wid)::integer;

-- The overridden jsonb column bound as a WRITE param (better-python #161): the
-- value binds DIRECTLY as `{"p2": config}` — NOT `typing.Any(config)`. The
-- SQLAlchemy dialect adapts the object to the jsonb column; a base-type
-- constructor wrap would be invalid Python (`typing.Any` is not callable).
-- name: InsertWidget :exec
INSERT INTO widget (widget_id, config)
VALUES (sqlc.arg(wid)::integer, sqlc.arg(cfg)::jsonb);
