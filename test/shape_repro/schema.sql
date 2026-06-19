-- VENDORED the reference-shape deletion-proof repro (GOAL §6 STEP-B, fork-side).
--
-- This is self-contained and carries NO secrets. It is a small, hand-
-- vendored schema that reproduces EVERY risky shape two external sed/perl
-- post-processors (two external post-processing scripts)
-- patch, consolidated into one consolidated codegen config. The whole point:
-- the fork's NullabilityOverrides / inflection_exclude_table_names / bytea
-- default / file_header / jsonb-Any default do, IN THE GENERATOR, exactly what
-- those seds used to do AFTER generation. The committed gen/ tree here is the
-- STEP-A migration baseline; `sqlc diff` (the deletion_proof nox session)
-- regenerates with ZERO post-processing and asserts byte-empty against it.
--
-- Shapes covered (1:1 with the seds, enumerated in queries.sql):
--   * kind / kinds singularization collision -> distinct OrderKind /
--     OrderKinds models via inflection_exclude_table_names (an external rename pass).
--   * LATERAL / correlated-aggregate / get_customer_primary_kind loosen
--     (an external post-processor: secondary_*, legal_form, owner_kind -> Optional).
--   * COALESCE(nullable, nullable) tighten scoped to one row (an external post-processor):
--     native sqlc infers it Optional; the table-qualified override flips it
--     non-Optional (load-bearing), mirroring the reference ledger_source tighten.
--   * jsonb -> typing.Any default (db: Optional[Any]); typed override elsewhere.
--   * bytea -> bytes built-in default (replaces the db inline override + the
--     inert memoryview sed).
--   * a multi-param bundle (> query_parameter_limit -> <Method>Params class).
--   * a :many SELECT stream and the :many-over-DML+RETURNING landmine.
--   * file_header "# pyright: basic" on every file + emit_init_file (analytics F5).

-- order_kind (catalog) and order_kinds (junction) both singularize to
-- `OrderKind`; the perl rename in an external post-processor split them. The
-- fork keeps them distinct via inflection_exclude_table_names: [order_kinds].
CREATE TABLE order_kind (
    order_kind_id text NOT NULL,
    label            text NOT NULL
);

CREATE TABLE order_kinds (
    customer_id      text NOT NULL,
    order_kind_id text NOT NULL
);

CREATE TABLE customer (
    customer_id   text  NOT NULL,
    full_name text  NOT NULL,
    -- bytea column: the fork's built-in default maps it to `bytes` (replaces
    -- an inline override + the inert `: memoryview$` sed).
    avatar        bytea,
    -- jsonb column: defaults to typing.Any in the generated model (the reference
    -- Optional[Any]); never patched by a sed.
    attributes     jsonb
);

CREATE TABLE ledger_entry (
    ledger_entry_id          bigint  NOT NULL,
    internal_code    text    NOT NULL,
    amount_moved numeric NOT NULL,
    transaction_id    text    NOT NULL
);

CREATE TABLE customer_tax_id (
    customer_tax_id_id bigint NOT NULL,
    customer_id        text   NOT NULL,
    tax_kind_id     text   NOT NULL,
    document_number             text   NOT NULL
);

CREATE TABLE ledger_charge (
    ledger_charge_id integer NOT NULL,
    penalty_amount     numeric,
    interest_amount     numeric
);

-- STABLE function that may return NULL (mirrors get_customer_primary_kind).
CREATE FUNCTION get_customer_primary_kind(p_customer_id text) RETURNS text AS $$
    SELECT 'PJ'::text;
$$ LANGUAGE sql STABLE;
