-- :one INSERT...RETURNING struct, bundled Params (5 params > qpl 4): exercises
-- runtime construction of a model with datetime + decimal + list columns.
-- name: InsertLedger :one
INSERT INTO ledger (ledger_id, note, amount, tags, created_at)
VALUES (
    sqlc.arg(ledger_id)::bigint,
    sqlc.arg(note)::text,
    sqlc.arg(amount)::numeric,
    sqlc.arg(tags)::text[],
    sqlc.arg(created_at)::timestamptz
)
RETURNING ledger_id, note, amount, tags, created_at;

-- :one struct (flat keyword-only) returning a row, plus the None path when no
-- row matches.
-- name: GetLedger :one
SELECT ledger_id, note, amount, tags, created_at
FROM ledger
WHERE ledger_id = sqlc.arg(ledger_id)::bigint;

-- :one scalar.
-- name: CountLedgers :one
SELECT count(*)::bigint AS ledger_count
FROM ledger;

-- :many SELECT struct -> conn.stream + async for/yield -> AsyncIterator[Row].
-- name: ListLedgers :many
SELECT ledger_id, note, amount, tags, created_at
FROM ledger
ORDER BY ledger_id ASC;

-- :many SELECT scalar -> yields a bare value.
-- name: ListLedgerNotes :many
SELECT note
FROM ledger
ORDER BY ledger_id ASC;

-- :exec.
-- name: TouchLedger :exec
UPDATE ledger
SET note = sqlc.arg(note)::text
WHERE ledger_id = sqlc.arg(ledger_id)::bigint;

-- :execrows -> result.rowcount.
-- name: DeleteLedgersByNote :execrows
DELETE FROM ledger
WHERE note = sqlc.arg(note)::text;

-- P-03: :many over DML+RETURNING -> result.all() -> list[Row], NOT conn.stream
-- (Postgres rejects a server-side cursor for a DML statement).
-- name: BumpLedgerAmounts :many
UPDATE ledger
SET amount = amount + sqlc.arg(delta)::numeric
WHERE note = sqlc.arg(note)::text
RETURNING ledger_id, note, amount, tags, created_at;

-- MAJOR-1: a WORD-BOUNDARY non-cast literal colon (' :b' — a colon preceded by a
-- non-word char that SQLAlchemy's BIND_PARAMS regex
-- `(?<![:\w\$\x5c]):([\w\$]+)(?![:\w\$])` WOULD read as a `:b` bind) must be
-- escaped ' \:b' in the body, while the `::bigint` cast becomes `\:\:bigint` and
-- `$1` becomes `:p1`. UNESCAPED, SQLAlchemy parses ' :b' as a bind and raises "A
-- value is required for bind parameter b" at execution — a runtime-only failure
-- that passes pyright. (An 'a:b' colon, by contrast, is glued to the word char
-- 'a', the lookbehind rejects it, so it is NEVER a bind whether escaped or not —
-- which would make this proof inert.) The query returns the literal ' :b' string
-- so the test asserts the escape both executed AND preserved the literal.
-- name: SelectColonLiteral :one
SELECT label::text AS colon_literal
FROM label_row
WHERE label = ' :b'
  AND label_row_id = sqlc.arg(label_row_id)::bigint;

-- A jsonb write param overridden to a typed model binds DIRECTLY (`{"pN": meta}`,
-- NOT a non-callable typing.Any(...) wrap, better-python #161). A nullable jsonb
-- (`extra`) write param round-trips NULL too. The `kind` param is a StrEnum
-- member written to a PG ENUM column (member round-trip on read).
-- name: InsertWidget :exec
INSERT INTO widget (widget_id, meta, extra, kind)
VALUES (
    sqlc.arg(widget_id)::bigint,
    sqlc.arg(meta)::jsonb,
    sqlc.narg(extra)::jsonb,
    sqlc.arg(kind)::widget_kind
);

-- The overridden jsonb column read back as a SCALAR :one returns the typed value
-- unwrapped (`return row[0]`), so no construction import is needed in the query
-- file — the value round-trips through the dialect.
-- name: GetWidgetMeta :one
SELECT meta
FROM widget
WHERE widget_id = sqlc.arg(widget_id)::bigint;

-- The nullable jsonb read back as a SCALAR :one round-trips NULL.
-- name: GetWidgetExtra :one
SELECT extra
FROM widget
WHERE widget_id = sqlc.arg(widget_id)::bigint;

-- The PG ENUM column read back as a SCALAR :one returns the StrEnum member
-- (`return row[0]`): the dialect decodes the enum text to the WidgetKind member,
-- so value-in == member-out round-trips.
-- name: GetWidgetKind :one
SELECT kind
FROM widget
WHERE widget_id = sqlc.arg(widget_id)::bigint;

-- Tighten / loosen nullability round-trip: the COALESCE result column retains
-- its source table (charge), so a table-qualified nullable:false override makes
-- it non-Optional; the CASE-WHEN sibling has Table==nil, so the override does
-- NOT match it and it stays Optional — genuinely NULL when penalty is absent.
-- name: GetChargeAmounts :one
SELECT
    fb.charge_id,
    COALESCE(fb.penalty_amount, 0) AS charge_penalty_value,
    CASE WHEN fb.penalty_amount IS NULL THEN NULL ELSE fb.penalty_amount END AS source_penalty_value
FROM charge AS fb
WHERE fb.charge_id = sqlc.arg(charge_id)::bigint;
