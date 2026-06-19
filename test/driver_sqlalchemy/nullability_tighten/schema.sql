-- Minimal repro of the reference COALESCE-tighten sed (P-05), the
-- LockChargeRowsRow shape (analytics/sqlc-generate post-processor).
-- sqlc infers COALESCE(x, non-null-literal) as Optional even though it is never
-- NULL at runtime; an external analytics post-processor tightened
-- charge_penalty_amount / charge_interest_amount to non-Optional, but ONLY in the
-- Charge row — the sibling CASE-WHEN variants in the Source/Retry rows are
-- genuinely nullable and must STAY Optional. The NullabilityOverride mechanism
-- (ruling F2) reproduces this via a TABLE-QUALIFIED override: the COALESCE
-- result column retains its source Table, while the CASE-WHEN result column has
-- Table==nil, so a table-qualified override hits the COALESCE column ONLY.
CREATE TABLE ledger_charge (
    ledger_charge_id integer NOT NULL,
    penalty_amount     numeric,
    interest_amount     numeric
);
