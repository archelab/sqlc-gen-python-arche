-- Tier-2 live-Postgres round-trip case. A single `ledger` table carrying the
-- type mix that exercises the runtime-construction paths a wrong escape /
-- forward-ref bug would pass pyright but fail on:
--   * created_at timestamptz  -> datetime.datetime (runtime field annotation)
--   * amount numeric          -> decimal.Decimal   (runtime field annotation)
--   * tags text[]             -> collections.abc.Sequence[str] (list field)
--   * note text               -> a plain scalar
-- plus a `label_row` table for the non-cast colon literal escape (better-python
-- #100), a `widget` table for the jsonb direct-bind (better-python #161), and a
-- `charge` table for the nullability-override (tighten / loosen) round-trips.
CREATE TABLE IF NOT EXISTS ledger
(
    ledger_id  bigint      NOT NULL,
    note       text        NOT NULL,
    amount     numeric     NOT NULL,
    tags       text[]      NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS label_row
(
    label_row_id bigint NOT NULL,
    label        text   NOT NULL
);

-- widget_kind is a PG ENUM emitted as `class WidgetKind(enum.StrEnum)`. The
-- `out-for-repair` value carries a hyphen so the OUT_FOR_REPAIR member exercises
-- value sanitization, and the round-trip proves a StrEnum member binds on write
-- and reads back as the same member (value-in == member-out) through the live
-- asyncpg/SQLAlchemy dialect — a runtime concern the golden + pyright tier cannot
-- see.
CREATE TYPE widget_kind AS ENUM ('active', 'archived', 'out-for-repair');

-- widget exercises a jsonb column overridden to a typed model (direct-bind on
-- write, read back), a nullable jsonb column (the typing.Any default, NULL
-- round-trip), and a PG ENUM column (the StrEnum member round-trip).
CREATE TABLE IF NOT EXISTS widget
(
    widget_id bigint      NOT NULL,
    meta      jsonb       NOT NULL,
    extra     jsonb,
    kind      widget_kind NOT NULL
);

-- charge exercises the nullability overrides: a COALESCE result column is
-- never NULL at runtime but sqlc infers Optional (tighten override makes it
-- non-Optional); the CASE-WHEN sibling is genuinely nullable and stays Optional.
CREATE TABLE IF NOT EXISTS charge
(
    charge_id      bigint  NOT NULL,
    penalty_amount numeric
);
