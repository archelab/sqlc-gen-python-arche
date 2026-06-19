# `test/shape_repro/` — post-processor-deletion proof

A self-contained proof that the generator emits correct code with **zero
post-processing**. It consolidates, into one codegen config, every result shape
that a downstream project might otherwise patch by hand (or with a `sed`/`perl`
pass) after `sqlc generate` — and shows the generator already produces exactly
that, so no post-processing step is needed.

## What it proves

The proof is a two-baseline diff:

- **Baseline** — the committed `gen/` tree, produced by the generator with the
  full set of config options (`nullability_overrides`, `file_header`,
  `inflection_exclude_table_names`, …) and **no** post-processing.
- **Check** — regenerate from the same config and assert the output is
  **byte-identical** to the committed baseline.

The `deletion_proof` nox session runs `sqlc diff` and asserts byte-empty. Because
every shape below is produced by a generator option rather than a post-pass, a
project can drop any hand-rolled post-generation patching entirely and rely on
the config alone.

## Shapes covered

| Shape a post-pass would otherwise patch | How the GENERATOR reproduces it | Proof in this tree |
| --- | --- | --- |
| Singularization collision: two tables that would both singularize to `OrderKind` | `inflection_exclude_table_names: [order_kinds]` | `models.py` has distinct `OrderKind` AND `OrderKinds` |
| LATERAL / correlated-aggregate / scalar-function result columns loosened to `Optional` | bare-name `nullability_overrides` (uniquely-named targets) | `GetReconcileCandidateRow.{secondary_total_ledger_amount,secondary_ledger_count,owner_kind,legal_form}: … \| None` |
| `COALESCE` over two nullable columns tightened to non-`Optional`, scoped to one table | table-qualified `nullability_overrides` (`ledger_charge.charge_*_value`) | `COALESCE(fb.penalty_amount, fb.interest_amount)` (both nullable) is inferred Optional by native sqlc; the override is **load-bearing** — it flips `LockChargeRowsRow.charge_penalty_value` to `decimal.Decimal` (strip it → `decimal.Decimal \| None`, perturbing the diff). The CASE-WHEN sibling `LockSourceRowsRow.charge_penalty_value: typing.Any \| None` (Table==nil) is NOT reached by the table-qualified override and stays Optional |
| `jsonb` → `Optional[Any]` (generator default) | jsonb default = `typing.Any` | `Customer.attributes: typing.Any \| None` |
| `bytea` → `bytes` (generator default) | bytea built-in default = `bytes` | `Customer.avatar: bytes \| None` |
| A file header on every file, including `__init__.py` | `file_header: "# pyright: basic"` + `emit_init_file: true` | line 1 of `models.py`, `queries.py`, AND `__init__.py` |
| Multi-param bundle, `:many` stream, `:many`-over-DML | generator emitters | `InsertLedgerEntryParams` / `BumpLedgerAmountsParams` bundles; `list_*` AsyncIterator; `bump_ledger_amounts` → `list[…]` via `.execute(...).all()` not `.stream()` |

## Running

```
nox -s deletion_proof          # sqlc diff (byte-empty proof) + pyright + ruff over gen/
nox -s deletion_proof_generate # regenerate the baseline in place after an intentional generator change
```

The `gen/` tree is committed as the baseline; the wasm is built fresh from source
by the nox session before each diff (it is gitignored, never committed).
