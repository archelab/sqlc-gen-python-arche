-- Write all columns. The write params are the jsonb default (typing.Any): the
-- override + validate apply to the READ path, so the test seeds arbitrary (incl.
-- deliberately malformed) JSON and validation happens on read. `extra` is a
-- nargs (nullable) param.
-- name: InsertWidget :exec
INSERT INTO widget (widget_id, config, payload, extra)
VALUES (
    sqlc.arg(wid)::integer,
    sqlc.arg(config)::jsonb,
    sqlc.arg(payload)::jsonb,
    sqlc.narg(extra)::jsonb
);

-- Scalar :one read of the nested-model column -> validated WidgetConfig.
-- name: GetConfig :one
SELECT config
FROM widget
WHERE widget_id = sqlc.arg(wid)::integer;

-- Scalar :one read of the union column -> validated WidgetPayload (A | B | C[D]).
-- name: GetPayload :one
SELECT payload
FROM widget
WHERE widget_id = sqlc.arg(wid)::integer;

-- Scalar :one read of the NULLABLE validated column: a SQL NULL must come back as
-- None (the NULL-guard path), NOT raise ValidationError against the non-Optional
-- WidgetConfig.
-- name: GetExtra :one
SELECT extra
FROM widget
WHERE widget_id = sqlc.arg(wid)::integer;

-- Struct :one read: a Row whose fields include the validated `config`/`payload`
-- (non-nullable) and the validated `extra` (nullable) — exercises the
-- struct-field validate branch, both forms.
-- name: GetWidget :one
SELECT widget_id, config, payload, extra
FROM widget
WHERE widget_id = sqlc.arg(wid)::integer;

-- :many SELECT of a validated scalar -> conn.stream + async-for/yield: the
-- stream-yield validate branch.
-- name: ListConfigs :many
SELECT config
FROM widget
ORDER BY widget_id ASC;

-- :many over DML + RETURNING a validated scalar -> result.all() list
-- comprehension: the list-comprehension validate branch.
-- name: DeleteWidgetsReturningConfig :many
DELETE FROM widget
WHERE widget_id = sqlc.arg(wid)::integer
RETURNING config;
