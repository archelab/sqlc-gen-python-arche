-- Charge row: COALESCE(x, non-null-literal) — never NULL but sqlc infers
-- Optional. The COALESCE result column RETAINS its source Table (ledger_charge),
-- so a table-qualified override (ledger_charge.charge_penalty_value) TIGHTENS it to
-- non-Optional. Aliases (charge_penalty_value / charge_interest_value) are
-- chosen singular-stable so the field name is not mangled by the result-column
-- singularizer.
-- name: LockChargeRows :many
SELECT
    fb.ledger_charge_id,
    COALESCE(fb.penalty_amount, 0) AS charge_penalty_value,
    COALESCE(fb.interest_amount, 0) AS charge_interest_value
FROM ledger_charge AS fb
WHERE fb.ledger_charge_id = ANY(sqlc.arg(ids)::integer[]);

-- Source row: the CASE-WHEN variant is GENUINELY nullable (returns NULL when
-- the charge is absent). Same output-field name as the Charge row, but its
-- result column has Table==nil, so the table-qualified tighten override does
-- NOT match it — it stays Optional, exactly as the consuming project needs.
-- name: LockSourceRows :many
SELECT
    fb.ledger_charge_id,
    CASE WHEN fb.ledger_charge_id IS NULL THEN NULL ELSE COALESCE(fb.penalty_amount, 0) END AS charge_penalty_value,
    CASE WHEN fb.ledger_charge_id IS NULL THEN NULL ELSE COALESCE(fb.interest_amount, 0) END AS charge_interest_value
FROM ledger_charge AS fb
WHERE fb.ledger_charge_id = ANY(sqlc.arg(ids)::integer[]);
