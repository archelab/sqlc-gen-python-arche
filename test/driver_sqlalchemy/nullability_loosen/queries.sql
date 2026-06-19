-- (a) Correlated-subquery SUM/COUNT aggregate result columns, Table==nil and
-- IsFuncCall, that sqlc infers NOT NULL. Mirrors account_balance's
-- secondary_total_ledger_amount (SUM) / secondary_ledger_count (COUNT). The
-- bare-name override loosens both to Optional.
-- (b) A direct get_customer_primary_kind FuncCall result column (Table==nil),
-- mirroring scope_owner_kind. Uniquely named here (owner_kind) so the
-- bare-name override is unambiguous.
-- name: GetReconcileCandidate :one
SELECT
    e.customer_id,
    (SELECT SUM(f.amount_moved)
     FROM ledger_entry f
     WHERE f.transaction_id = e.customer_id) AS secondary_total_ledger_amount,
    (SELECT COUNT(*)
     FROM ledger_entry f
     WHERE f.transaction_id = e.customer_id) AS secondary_ledger_count,
    get_customer_primary_kind(e.customer_id) AS owner_kind
FROM customer e
WHERE e.customer_id = sqlc.arg(eid)::text;

-- (c) A LEFT JOIN LATERAL ColumnRef that sqlc infers NOT NULL but is nullable
-- because the LATERAL can match zero rows, mirroring legal_form.
-- name: GetCustomerLegalForm :one
SELECT
    e.customer_id,
    e.full_name,
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
