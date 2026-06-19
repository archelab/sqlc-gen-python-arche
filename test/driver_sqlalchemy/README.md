# `driver_sqlalchemy` test corpus

This directory is the SQLAlchemy-async driver proof corpus (PROOFS layer, GOAL
§2 / §5). The driver, the `pydantic` model emitter, and the golden cases are
LIVE. The `nox -s sqlalchemy` session regenerates every per-case `gen/` tree and
runs `pyright` + `ruff` over it; `nox -s sqlalchemy_check` runs `sqlc diff`
against the committed goldens then the same lint/type gate. Both are wired into
`options.sessions`. `sqlalchemy` and `pydantic` are project dependencies so the
type errors are real (not missing-stub noise); the `str_enum` case is
type-checked at Python 3.11 (its `enum.StrEnum` floor).

## Cases (Tier 1 — byte goldens via real `sqlc diff`)
~18 minimal testdata cases, one per risky shape, each running `sqlc generate`
then `sqlc diff` against a committed golden `python/` tree, plus `pyright` +
`ruff`. Each case gets its own `sqlc.yaml` (`sql_driver: sqlalchemy`,
`model_type: pydantic`, `emit_classes: true`), a `schema.sql`, a `queries.sql`,
and a committed golden output tree. Cases to pin:

- the four verb shapes (`:one`, `:many` SELECT, `:many` DML+RETURNING, `:exec`,
  `:execrows`);
- the `\:<verb>` header escape and the `:pN` + `\:\:` body escape (incl. a
  **non-cast** literal colon, per REVIEW-CORRECTIONS MAJOR-1);
- keyword-only vs `Params`-bundling, including the 2-param and at-limit
  boundary;
- scalar `:one`;
- the nullability loosen/tighten cases (minimal repros of the four reference files:
  `account_balance`, `crud_customer_picker`, `account_extrato_com_previsao`,
  `ledger_source`) driving `nullability_overrides`;
- `order_kind`/`order_kinds` collision (non-zero exit, both names in
  stderr) AND a collision + `omit_unused_models` case;
- `bytea` -> `bytes`, `jsonb` -> typed override, `StrEnum`, defaults-last,
  reserved-`id` alias;
- the `file_header` knob across `models.py` / a query file / `__init__.py`
  (header on line 1) vs off;
- the synthetic `:many`-over-DML -> `.all()` case;
- the unknown-option-key rejection (a stray `bogus_knob` errors, key named);
- a fork-internal docstring-keeps-`$N` guard (labeled NOT correctness-critical).

### C-#20 remaining-correctness cases (P-15/17/18/19/21/22)

- `multiline_sql` (P-17) — a multi-line SQL body + an embedded block comment stay
  intact inside the `typing.Final[str]` constant (no broken triple-quote), with the
  F1 colon-escape applied across every line.
- `defaults_last` (P-18) — a pydantic Row mixing NOT NULL and nullable columns keeps
  SELECT/column order with NO `= None` field default (the reference pydantic models carry
  zero defaults).
- `omit_unused` (P-19) — `omit_unused_models: true` drops an unreferenced table model
  from `models.py`.
- `embed` (P-22) — `sqlc.embed(author)` composes a nested `author: models.Author`
  sub-struct field, constructed `author=models.Author(author_id=row[2], name=row[3])`
  with the running row index advancing past the embedded columns (mirrors
  asyncpg.go's embed scan; the SQLAlchemy row-construction helper now handles embed
  expansion in one shared `sqlalchemyRowKwargs`).
- `multi_schema` (P-21) — `schema` is a LIST of two files and `queries` is a directory
  glob; a JOIN resolves across both schema files (mirrors app_billing listing
  app_db migrations by path).
- P-15 (empty Params/Row class emits `pass`) is pinned by a Go unit test
  (`internal/codegen/tables_empty_test.go`) rather than a golden — sqlc does not emit
  an empty class for any natural input, so the guard is exercised directly at
  the `buildPydanticTable` boundary.

## Nullability over-match audit (ruling F2)

The `NullabilityOverride` chokepoint (`applyNullabilityOverride` wrapping
`makePythonType`) receives only a `*plugin.Column` — no query name and no
owning-struct name. So an override keyed by `[schema.][table.]column` can only
disambiguate by the column's OUTPUT field name (`col.Name`, the SELECT alias)
and its source `Table`. Re-audited against the REAL loosen/tighten target
set, per column:

| target | shape | resolvable by | golden |
| --- | --- | --- | --- |
| `secondary_*`/`tertiary_*`/`quaternary_*` (17, account_balance), `legal_form` | unique output-field name, no base-model collision | **bare-name** | `nullability_loosen` |
| `charge_penalty_amount` / `charge_interest_amount` TIGHTEN | COALESCE (Table-retained) in `LockChargeRowsRow` vs CASE-WHEN (Table==nil) siblings that must STAY Optional | **table-qualified** (`ledger_charge.charge_penalty_amount`) — hits only the Table-retaining COALESCE column | `nullability_tighten` |
| `scope_kind` / `scope_owner_kind` LOOSEN | query row (loosen) vs base view-model `VwAccountStatementForecast` (must stay non-null); SAME view table; FuncCall has no table | **NEITHER** bare-name nor table-qualified — no key separates them | pinned in `internal/nullability_spike_test.go` |

The `scope` pair is the genuine collision: a bare-name loosen ALSO flips the
base view model. It is HARMLESS for the consuming project today (that base model is unused by app
code) and does NOT break the cutover deletion proof (the override is on both
STEP-A and STEP-B baselines, so STEP-B stays byte-empty), but it IS a semantic
divergence from the legacy committed `models.py`. The strictly-correct fix
is the query/struct-scoped in-generator post-pass fallback (ruling F2), which is
OUT OF SCOPE here and **flagged as a follow-up for the
two scope loosen entries**.

Mechanism fix landed this phase: `NullabilityOverride.Matches` keys on the
OUTPUT alias `col.Name`, NOT `col.OriginalName` (the pre-alias source column) —
otherwise `legal_form` (alias of `nat.document_number`) would miss its target AND
over-match every `document_number`-derived column.

### Tier 2 — live-Postgres pytest round-trip (planned)
A conftest fixture yielding a real `sqlalchemy.ext.asyncio.AsyncConnection`
(`create_async_engine` + `begin()`), executing the generated `AsyncQuerier`
against the CI Postgres service: `:one`/`.first()`+None, `:many`/`.stream()`
`AsyncIterator`, `:exec`, `:execrows`/`.rowcount`, and the P-03
`:many`-over-DML proving `.all()` runs where `.stream()` would raise. This is
non-negotiable for the `\:<verb>` / `$N`->`:pN` / `::`-escape transforms (a
wrong escape passes pyright but fails at runtime).
