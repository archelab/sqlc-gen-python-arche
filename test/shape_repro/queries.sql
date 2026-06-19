-- VENDORED the reference-shape deletion-proof repro queries. Each block names the
-- the consuming project sed/perl rule it replaces; the generator (NullabilityOverrides +
-- inflection_exclude + bytea/jsonb defaults + file_header) must emit the
-- post-sed result with NO post-processing, so `sqlc diff` stays byte-empty.

-- kind/kinds collision (an external rename pass): selecting from BOTH the catalog
-- (order_kind -> OrderKind) and the junction (order_kinds ->
-- OrderKinds via inflection_exclude_table_names) proves the two models are
-- distinct, not a singularized redeclaration.
-- name: GetOrderKind :one
SELECT order_kind_id, label
FROM order_kind
WHERE order_kind_id = sqlc.arg(tid)::text;

-- name: ListOrderKinds :many
SELECT customer_id, order_kind_id
FROM order_kinds
WHERE customer_id = sqlc.arg(eid)::text;

-- bytea + jsonb defaults on a base-table model row: avatar -> bytes | None,
-- attributes -> typing.Any | None. No sed touches these once the generator
-- defaults are right (bytea built-in, jsonb -> Any).
-- name: GetCustomer :one
SELECT customer_id, full_name, avatar, attributes
FROM customer
WHERE customer_id = sqlc.arg(eid)::text;

-- LATERAL / correlated-aggregate / get_customer_primary_kind LOOSEN (an external post-processor):
-- sqlc infers the SUM/COUNT aggregates and the FuncCall result column as NOT
-- NULL; bare-name NullabilityOverrides loosen them to Optional. legal_form
-- is a LEFT JOIN LATERAL ColumnRef loosened the same way.
-- name: GetReconcileCandidate :one
SELECT
    e.customer_id,
    (SELECT SUM(f.amount_moved)
     FROM ledger_entry f
     WHERE f.transaction_id = e.customer_id) AS secondary_total_ledger_amount,
    (SELECT COUNT(*)
     FROM ledger_entry f
     WHERE f.transaction_id = e.customer_id) AS secondary_ledger_count,
    get_customer_primary_kind(e.customer_id) AS owner_kind,
    nat.document_number AS legal_form
FROM customer e
LEFT JOIN LATERAL (
    SELECT ef.document_number
    FROM customer_tax_id ef
    WHERE ef.customer_id = e.customer_id
      AND ef.tax_kind_id = 'LEGAL_FORM'
    ORDER BY ef.customer_tax_id_id DESC
    LIMIT 1
) nat ON TRUE
WHERE e.customer_id = sqlc.arg(eid)::text;

-- COALESCE TIGHTEN scoped to one row (an external post-processor): COALESCE over TWO nullable
-- columns is inferred NULLABLE by native sqlc (decimal.Decimal | None) -- so the
-- table-qualified override is genuinely LOAD-BEARING: it RETAINS its source Table
-- (ledger_charge) and the `ledger_charge.charge_*_value: nullable:false` override is
-- what flips it to non-Optional (strip the override and this row regresses to
-- Optional, perturbing the deletion-proof diff). The sibling CASE-WHEN result
-- column (LockSourceRows) has Table==nil, so the table-qualified override does NOT
-- reach it and it stays Optional. Aliases are singular-stable so the result
-- singularizer does not mangle them. This mirrors the reference ledger_source
-- COALESCE tighten end-to-end in the byte tier.
-- name: LockChargeRows :many
SELECT
    fb.ledger_charge_id,
    COALESCE(fb.penalty_amount, fb.interest_amount) AS charge_penalty_value,
    COALESCE(fb.interest_amount, fb.penalty_amount) AS charge_interest_value
FROM ledger_charge AS fb
WHERE fb.ledger_charge_id = ANY(sqlc.arg(ids)::integer[]);

-- name: LockSourceRows :many
SELECT
    fb.ledger_charge_id,
    CASE WHEN fb.ledger_charge_id IS NULL THEN NULL ELSE COALESCE(fb.penalty_amount, 0) END AS charge_penalty_value,
    CASE WHEN fb.ledger_charge_id IS NULL THEN NULL ELSE COALESCE(fb.interest_amount, 0) END AS charge_interest_value
FROM ledger_charge AS fb
WHERE fb.ledger_charge_id = ANY(sqlc.arg(ids)::integer[]);

-- multi-param bundle (> query_parameter_limit -> <Method>Params positional
-- class): five params force the bundle path the param-de-collision + bundling
-- reconciliation (C-#15) owns.
-- name: InsertLedgerEntry :exec
INSERT INTO ledger_entry (ledger_entry_id, internal_code, amount_moved, transaction_id)
VALUES (
    sqlc.arg(ledger_entry_id)::bigint,
    sqlc.arg(internal_code)::text,
    sqlc.arg(amount_moved)::numeric,
    sqlc.arg(transaction_id)::text
);

-- :many SELECT stream (native async generator -> AsyncIterator).
-- name: ListLedgerEntries :many
SELECT ledger_entry_id, internal_code, amount_moved, transaction_id
FROM ledger_entry
ORDER BY ledger_entry_id ASC;

-- :many over DML+RETURNING landmine (P-03): must materialize via
-- conn.execute(...).all() -> list[T], NOT conn.stream (Postgres rejects a
-- server-side cursor for DML).
-- name: BumpLedgerAmounts :many
UPDATE ledger_entry
SET amount_moved = amount_moved + sqlc.arg(incremento)::numeric
WHERE transaction_id = sqlc.arg(transaction_id)::text
RETURNING ledger_entry_id, internal_code, amount_moved, transaction_id;
