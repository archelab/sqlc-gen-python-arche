-- Minimal repro of the reference LATERAL / LEFT-JOIN / get_customer_primary_kind
-- nullability-LOOSEN seds (P-04). sqlc infers the SUM/COUNT aggregate and the
-- FuncCall result columns as NOT NULL even though a LEFT JOIN / LIMIT-0 LATERAL
-- can leave them NULL; an external db/sqlc-generate post-processor rewrote each to
-- Optional by output-field name. The NullabilityOverride mechanism (ruling F2)
-- replaces those by-name seds. Every loosen-target here is UNIQUELY named (no
-- base-table model carries the same field), so a bare-name override hits
-- exactly the intended result column WITHOUT a query scope — the over-match
-- audit condition. (The scope_kind/scope_owner_kind pair is NOT reproduced
-- here: those collide with the base view model and are documented as an
-- unresolvable-by-bare-name case in the README + the over-match audit.)
CREATE TABLE customer (
    customer_id   text NOT NULL,
    full_name text NOT NULL
);

CREATE TABLE ledger_entry (
    ledger_entry_id         bigint NOT NULL,
    internal_code   text   NOT NULL,
    amount_moved numeric NOT NULL,
    transaction_id   text   NOT NULL
);

CREATE TABLE customer_tax_id (
    customer_tax_id_id bigint NOT NULL,
    customer_id        text   NOT NULL,
    tax_kind_id     text   NOT NULL,
    document_number             text   NOT NULL
);

-- STABLE function that may return NULL (mirrors get_customer_primary_kind).
CREATE FUNCTION get_customer_primary_kind(p_customer_id text) RETURNS text AS $$
    SELECT 'PJ'::text;
$$ LANGUAGE sql STABLE;
