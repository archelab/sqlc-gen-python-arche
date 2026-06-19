# sqlc-gen-python-arche

A [sqlc](https://github.com/sqlc-dev/sqlc) WASM plugin that generates type-safe
Python from your SQL — Pydantic (or dataclass / attrs / msgspec) models and a
SQLAlchemy-async querier, with opt-in fail-loud runtime validation of typed
`jsonb` columns.

## Lineage / credits

This is a fork of
[rayakame/sqlc-gen-better-python](https://github.com/rayakame/sqlc-gen-better-python)
(MIT), which itself builds on [sqlc-dev/sqlc](https://github.com/sqlc-dev/sqlc)
and the official
[sqlc-gen-python](https://github.com/sqlc-dev/sqlc-gen-python) /
[sqlc-gen-kotlin](https://github.com/sqlc-dev/sqlc-gen-kotlin) generators. The
model and query emitters, the driver abstraction, the inflection and type
machinery — most of the generator — come from that lineage. The MIT copyright
(`Copyright (c) 2025 Rayakame`) and attribution are preserved verbatim in
[LICENSE](LICENSE) and [NOTICE](NOTICE).

This fork adds a SQLAlchemy-async + Pydantic code path, the `validate` override
knob, a live-Postgres test tier, and a set of correctness fixes — so the
generated code needs no post-processing pass.

## What this fork adds

### 1. Fail-loud `jsonb` validation — `validate: true` (headline)

A typed override on a `jsonb` (or any) column can be marked `validate: true`.
The generated read path then validates the stored value against the declared
shape with a cached `pydantic.TypeAdapter`, so a row that does **not** match the
type raises `pydantic.ValidationError` **at read time** — instead of returning
the decoded value unwrapped. The shape can be a nested model, a **discriminated
union `A | B | C[D]`** (including a generic member), and the column may be
nullable (a SQL `NULL` returns `None`, it does not raise). See
[The `validate` override](#the-validate-override) below.

### 2. SQLAlchemy-async driver — `sql_driver: sqlalchemy`

Generates an `AsyncQuerier` over a SQLAlchemy
`sqlalchemy.ext.asyncio.AsyncConnection`. The generated code is DBAPI-agnostic —
it binds parameters through `sqlalchemy.text()` with `:pN` named placeholders, so
it works with whichever async driver your engine uses (the live-Postgres tests
exercise it over `asyncpg`). `:one`,
`:many`, `:exec`, `:execrows`, and `:execresult` are supported. `:many` over a
`SELECT` is a native async generator backed by `conn.stream(...)`; `:many` over
a `DML ... RETURNING` materializes to `list[T]` via `result.all()`. See
[The SQLAlchemy async driver](#the-sqlalchemy-async-driver).

> This is the new `sqlalchemy` driver. It is distinct from the inherited
> `asyncpg` driver — don't conflate the two.

### 3. Live-Postgres (Tier-2) test coverage

The generated SQLAlchemy code is executed against a real Postgres in
round-trip tests: `sqlc.embed` row indexing, reserved-keyword field mapping,
param bind keys, `:many` DML `RETURNING`, multi-line/commented SQL, interleaved
nullable column ordering, and the `jsonb` validation above — catching
runtime-only bugs the static gates miss.

### 4. Generator and type-checker fixes

- `:many` over `DML ... RETURNING` materializes to `list[T]` instead of emitting
  a broken async generator (those rows cannot be streamed).
- StrEnum is emitted as `enum.StrEnum` with a correct class name, sanitized
  member names, and quoted values; the type-checker floor is pinned to Python
  3.11 (below which pyright collapses `StrEnum` members).
- Postgres `bytea` maps to `bytes` by default instead of `memoryview`.
- An empty params class emits a `pass` body instead of an invalid empty class.
- Two query columns resolving to the same name are de-collided rather than
  silently shadowing each other.
- A `sqlc.embed` row is constructed with a correct running row index.
- `model_type` is a typed enum, not a bare string; unknown keys in the plugin
  `options` block fail loudly and name the offending key.
- A gen-time guard reports struct-to-struct name collisions instead of emitting
  two classes with the same name.

### Inherited from the upstream plugin

The `asyncpg` / `aiosqlite` / `sqlite3` drivers; the `dataclass` / `attrs` /
`msgspec` model types; modern `T | None` / `list[T]` typing; type overrides;
`omit_unused_models`; `sqlc.embed`; the asyncpg `:copyfrom` path; docstring
generation; and the overall sqlc plugin plumbing. See the
[upstream README](https://github.com/rayakame/sqlc-gen-better-python) for the
history of those features.

## Installation

The plugin is distributed as a single WASM module, pinned by `sha256` in your
`sqlc.yaml`. Download `sqlc-gen-python-arche.wasm` from the
[latest release](https://github.com/archelab/sqlc-gen-python-arche/releases/latest)
(or build it from source — see [Development](#development); the wasm is never
committed, only the source and the pins are), then reference it:

```yaml
# filename: sqlc.yaml
version: "2"
plugins:
  - name: python
    wasm:
      url: https://github.com/archelab/sqlc-gen-python-arche/releases/download/v0.5.0/sqlc-gen-python-arche.wasm
      sha256: ae82dcd410956538f871680a271dd9c1f4def9365fe47232df2b227c6fafccb6
sql:
  - schema: schema.sql
    queries: queries.sql
    engine: postgresql
    codegen:
      - out: gen
        plugin: python
        options:
          package: app.db.gen
          sql_driver: sqlalchemy
          model_type: pydantic
          emit_classes: true
          emit_init_file: false
```

The `build.sh` script (see [Development](#development)) rewrites the `sha256`
pin to the freshly computed hash of your built wasm, so the value above is the
one this repo's own build produces. More options at the
[`sqlc` config reference](https://docs.sqlc.dev/en/stable/reference/config.html).

## The SQLAlchemy async driver

With `sql_driver: sqlalchemy` and `emit_classes: true`, query functions become
methods on an `AsyncQuerier` that wraps a SQLAlchemy `AsyncConnection`. Given:

```sql
-- name: GetConfig :one
SELECT config FROM widget WHERE widget_id = sqlc.arg(wid)::integer;

-- name: ListConfigs :many
SELECT config FROM widget ORDER BY widget_id ASC;

-- name: DeleteWidgetsReturningConfig :many
DELETE FROM widget WHERE widget_id = sqlc.arg(wid)::integer RETURNING config;
```

the generated querier is:

```python
class AsyncQuerier:
    def __init__(self, conn: sqlalchemy.ext.asyncio.AsyncConnection):
        self._conn = conn

    # :one -> .first(), None when no row
    async def get_config(self, *, wid: int) -> WidgetConfig | None:
        row = (await self._conn.execute(sqlalchemy.text(GET_CONFIG), {"p1": wid})).first()
        if row is None:
            return None
        return ...

    # :many SELECT -> native async generator backed by conn.stream
    async def list_configs(self) -> collections.abc.AsyncIterator[WidgetConfig]:
        result = await self._conn.stream(sqlalchemy.text(LIST_CONFIGS))
        async for row in result:
            yield ...

    # :many over DELETE ... RETURNING -> materialized list via result.all()
    async def delete_widgets_returning_config(self, *, wid: int) -> list[WidgetConfig]:
        result = await self._conn.execute(
            sqlalchemy.text(DELETE_WIDGETS_RETURNING_CONFIG), {"p1": wid}
        )
        return [... for row in result.all()]
```

Positional `$N` parameters are rewritten to `:pN` named binds and passed as a
dict (`{"p1": wid}`); every literal `:` in the SQL is escaped so casts like
`::integer` survive `sqlalchemy.text()`.

## The `validate` override

A standard sqlc type override points a column at a Python type. Marking it
`validate: true` additionally makes the **read path fail loud**: the generated
code validates the stored value against the declared shape, so a value that
does not match raises rather than being returned unwrapped. The knob is only
valid with `sql_driver: sqlalchemy` (it is rejected at config time on any other
driver), and it is byte-neutral when off.

### Config

```yaml
options:
  sql_driver: sqlalchemy
  model_type: pydantic
  overrides:
    # NESTED model column.
    - column: widget.config
      py_type:
        import: app.db.shapes
        package: WidgetConfig
        type: WidgetConfig
      validate: true
    # DISCRIMINATED UNION column: A | B | C[D] (incl. a generic member).
    - column: widget.payload
      py_type:
        import: app.db.shapes
        package: WidgetPayload
        type: WidgetPayload
      validate: true
    # NULLABLE jsonb -> NULL-guard path (a SQL NULL returns None, never raises).
    - column: widget.extra
      py_type:
        import: app.db.shapes
        package: WidgetConfig
        type: WidgetConfig
      validate: true
```

Where the shapes are ordinary Pydantic types — a nested model and a
discriminator-tagged union with a generic member:

```python
class WidgetConfig(pydantic.BaseModel):
    model_config = pydantic.ConfigDict(extra="forbid")
    name: str
    dimensions: Dimensions          # a nested sub-model
    tags: list[str]
    ratio: decimal.Decimal
    created: datetime.datetime
    enabled: bool = True
    note: str | None = None

# A | B | C[D], tagged on `kind`; ListPayload[Item] is the generic member C[D].
WidgetPayload: typing.TypeAlias = typing.Annotated[
    TextPayload | NumberPayload | ListPayload[Item],
    pydantic.Field(discriminator="kind"),
]
```

### Generated code

The plugin emits one cached module-level `TypeAdapter` per distinct validated
type, and the read path calls `validate_python` on it:

```python
_WidgetConfig_adapter: pydantic.TypeAdapter[WidgetConfig] = pydantic.TypeAdapter(WidgetConfig)
_WidgetPayload_adapter: pydantic.TypeAdapter[WidgetPayload] = pydantic.TypeAdapter(WidgetPayload)


class AsyncQuerier:
    def __init__(self, conn: sqlalchemy.ext.asyncio.AsyncConnection):
        self._conn = conn

    # non-null scalar :one -> fail-loud validate, no NULL guard
    async def get_config(self, *, wid: int) -> WidgetConfig | None:
        row = (await self._conn.execute(sqlalchemy.text(GET_CONFIG), {"p1": wid})).first()
        if row is None:
            return None
        return _WidgetConfig_adapter.validate_python(row[0])

    # NULLABLE scalar :one -> NULL guard: a SQL NULL returns None, never raises
    async def get_extra(self, *, wid: int) -> WidgetConfig | None:
        row = (await self._conn.execute(sqlalchemy.text(GET_EXTRA), {"p1": wid})).first()
        if row is None:
            return None
        return _WidgetConfig_adapter.validate_python(row[0]) if row[0] is not None else None

    # discriminated union member, validated against A | B | C[D]
    async def get_payload(self, *, wid: int) -> WidgetPayload | None:
        row = (await self._conn.execute(sqlalchemy.text(GET_PAYLOAD), {"p1": wid})).first()
        if row is None:
            return None
        return _WidgetPayload_adapter.validate_python(row[0])

    # :many SELECT -> native async generator, each row validated as it streams
    async def list_configs(self) -> collections.abc.AsyncIterator[WidgetConfig]:
        result = await self._conn.stream(sqlalchemy.text(LIST_CONFIGS))
        async for row in result:
            yield _WidgetConfig_adapter.validate_python(row[0])

    # :many over DELETE ... RETURNING -> materialized list, each element validated
    async def delete_widgets_returning_config(self, *, wid: int) -> list[WidgetConfig]:
        result = await self._conn.execute(sqlalchemy.text(DELETE_WIDGETS_RETURNING_CONFIG), {"p1": wid})
        return [_WidgetConfig_adapter.validate_python(row[0]) for row in result.all()]
```

A validated column inside a struct row is validated per field, with the same
NULL guard for nullable fields:

```python
async def get_widget(self, *, wid: int) -> models.Widget | None:
    row = (await self._conn.execute(sqlalchemy.text(GET_WIDGET), {"p1": wid})).first()
    if row is None:
        return None
    return models.Widget(
        widget_id=row[0],
        config=_WidgetConfig_adapter.validate_python(row[1]),
        payload=_WidgetPayload_adapter.validate_python(row[2]),
        extra=_WidgetConfig_adapter.validate_python(row[3]) if row[3] is not None else None,
    )
```

### Fail-loud behavior

A stored value that violates the declared shape raises on read, on every path
(scalar `:one`, struct `:one`, streamed `:many`, and `:many`-over-DML). For the
union, a payload whose `kind` matches no member — or whose body is wrong for its
`kind` — raises; a SQL `NULL` in a nullable validated column returns `None`
rather than raising. This is exercised end-to-end against a real Postgres in
[`test/driver_sqlalchemy/jsonb_validate/`](test/driver_sqlalchemy/jsonb_validate/).

### Notes

- **`TypeAdapter`, not `Model.model_validate`.** The override type may be a
  union, a generic, or a nested alias — not necessarily a single
  `BaseModel` — so the plugin uses `pydantic.TypeAdapter[T]`, whose explicit
  annotation also gives the method a known return type.
- **Collision-guarded.** Two validated overrides whose type *name* comes from
  different modules are rejected at config time (the cached adapter is named
  from the type name); rename one to disambiguate.
- **Byte-neutral when off.** Files with no validated override generate
  identically to before; the validation block only fires under the SQLAlchemy
  driver when at least one validated type is present.

## Configuration options

| Name                             | Type                      | Required | Description                                                                                                                                |
|----------------------------------|---------------------------|----------|------------------------------------------------------------------------------------------------------------------------------------------|
| `package`                        | string                    | yes      | The package name for the generated files.                                                                                                  |
| `emit_init_file`                 | bool                      | yes      | When `false`, no `__init__.py` is created. Only set this if you already have one.                                                          |
| `sql_driver`                     | string                    | no       | The SQL driver: `aiosqlite` (default), `sqlite3`, `asyncpg`, or `sqlalchemy`. See [feature support](#feature-support).                     |
| `model_type`                     | string                    | no       | `dataclass` (default), `msgspec`, `attrs`, or `pydantic`.                                                                                   |
| `file_header`                    | string                    | no       | A header prepended verbatim to every emitted file (including `__init__.py`).                                                                |
| `nullability_overrides`          | list[NullabilityOverride] | no       | Loosen/tighten inferred nullability per result column. See [nullability overrides](#nullability-overrides).                                 |
| `initialisms`                    | list[string]              | no       | [Initialisms](https://google.github.io/styleguide/go/decisions.html#initialisms) to upper-case (e.g. `app_id` → `AppID`). Defaults to `["id"]`. |
| `emit_exact_table_names`         | bool                      | no       | When `true`, model names mirror table names; otherwise sqlc singularizes plural table names.                                                |
| `emit_classes`                   | bool                      | no       | When `true`, query functions are methods on a `Querier` / `AsyncQuerier` class; otherwise standalone functions.                             |
| `inflection_exclude_table_names` | list[string]              | no       | Table names not to singularize. Only applies when `emit_exact_table_names` is `false`.                                                      |
| `omit_unused_models`             | bool                      | no       | When `true`, tables not referenced by any query are not emitted as models.                                                                  |
| `omit_typechecking_block`        | bool                      | no       | When `true`, non-builtin types are not wrapped in a `typing.TYPE_CHECKING` block. Defaults to `false`.                                      |
| `docstrings`                     | string                    | no       | Docstring format: `google`, `numpy`, `pep257`, or `none`.                                                                                   |
| `docstrings_emit_sql`            | bool                      | no       | When `false`, the SQL is not included in docstrings. Defaults to `true`; unused when `docstrings` is unset or `none`.                       |
| `omit_kwargs_limit`              | integer                   | no       | Queries with at most this many parameters do not require keyword arguments. Defaults to `0` (every query uses kwargs).                      |
| `query_parameter_limit`          | integer                   | no       | Parameter count at or above which a query's parameters bundle into a `<Method>Params` object instead of separate arguments.                  |
| `speedups`                       | bool                      | no       | Use faster third-party libraries for type conversion (extra deps; currently `sqlite3` & `aiosqlite`, via `ciso8601`).                       |
| `overrides`                      | list[Override]            | no       | [Type overrides](#type-overrides), each optionally `validate: true` (see [The `validate` override](#the-validate-override)).                |
| `debug`                          | bool                      | no       | When `true`, write debug logs to `log.txt` during `sqlc generate`. Defaults to `false`.                                                     |

### Type overrides

Like `sqlc-gen-go`, you can override the Python type for every column of a given
SQL type, or for a specific column. Add `validate: true` to make the read path
fail loud (SQLAlchemy driver only — see
[The `validate` override](#the-validate-override)):

```yaml
options:
  overrides:
    - db_type: text
      py_type:
        import: collections
        package: UserString
        type: UserString
    - column: table_name.text_column
      py_type:
        import: collections
        type: collections.UserString
```

### Nullability overrides

sqlc infers result-column nullability statically, but some shapes are wider than
it can see: a correlated `SUM`/`COUNT` is always nullable, a `LEFT JOIN LATERAL`
column is nullable, and a `COALESCE` over nullable columns is non-null in
practice. Override the inference per column, by bare name or by `table.column`
to scope the override to a single source table:

```yaml
options:
  nullability_overrides:
    # Loosen: a correlated aggregate result column to Optional.
    - column: secondary_total_ledger_amount
      nullable: true
    # Tighten: a COALESCE over two nullable columns, scoped to one table.
    - column: ledger_charge.charge_penalty_value
      nullable: false
```

A bare-name override matches the result column by the name the caller sees (the
alias), across every query. A `table.column` override matches only columns that
retain that source table, which is how you tighten a `COALESCE` column without
also touching a same-named `CASE WHEN` sibling that has no source table.

## Feature support

Every [sqlc macro](https://docs.sqlc.dev/en/latest/reference/macros.html) is
supported. The supported
[query commands](https://docs.sqlc.dev/en/latest/reference/query-annotations.html)
depend on the driver:

|            | `:exec` | `:execresult` | `:execrows` | `:execlastid` | `:many` | `:one` | `:copyfrom` |
|------------|---------|---------------|-------------|---------------|---------|--------|-------------|
| aiosqlite  | yes     | yes           | yes         | yes           | yes     | yes    | no          |
| sqlite3    | yes     | yes           | yes         | yes           | yes     | yes    | no          |
| asyncpg    | yes     | yes           | yes         | no            | yes     | yes    | yes         |
| sqlalchemy | yes     | yes           | yes         | no            | yes     | yes    | no          |
| psycopg2   | no      | no            | no          | no            | no      | no     | no          |
| mysql      | no      | no            | no          | no            | no      | no     | no          |

> `:batch*` commands are not supported. Prepared queries are not planned.

## Development

The plugin is Go, compiled to a single WASM module; the test and lint pipelines
run under [`nox`](https://nox.thea.codes/) (driven by
[`uv`](https://docs.astral.sh/uv/)). [mise](https://mise.jdx.dev) pins the
toolchain (Go, `uv`, `sqlc`, `changie`) and wraps every task:

```bash
mise install     # provision the pinned toolchain
mise tasks       # list every task
mise run build   # build the wasm + re-pin every sqlc.yaml sha256
mise run check   # the full CI-equivalent gate (needs `mise run db:up`)
```

A running Docker daemon is the only prerequisite mise cannot provide (the
live-Postgres tests need it). See [CONTRIBUTING.md](CONTRIBUTING.md) for running
the pipelines directly, without mise.

### Build the wasm

```bash
mise run build       # or: bash scripts/build/build.sh
```

The build is reproducible: it pins the Go toolchain (`GOTOOLCHAIN`, kept in
lockstep with the `go` line in `go.mod`) and builds with `-trimpath`, so two
builds of the same source on different machines are byte-identical. It writes
`sqlc-gen-python-arche.wasm` at the repo root, copies it beside every
`sqlc.yaml`, and patches each `sha256` pin to the freshly computed hash. The
wasm is never committed; only the source and the pins are. Every `nox` session
that runs `sqlc generate` / `sqlc diff` rebuilds the wasm from source first, so
the golden trees are always checked against a fresh build.

### Pipelines

```bash
mise run golden      # sqlc diff + pyright + ruff over every golden tree
mise run lint        # pyright + ruff
mise run go-test     # Go unit + architecture tests
mise run test        # live-Postgres round-trip tests (needs db:up)
```

Each wraps a `nox` session (`uv run nox -s <session>`); run `mise tasks` for the
full list. Per-driver sessions (`sqlite3`, `aiosqlite`, `asyncpg`, `sqlalchemy`)
regenerate that driver's test tree, then run `pyright` and `ruff` on it; the
`*_check` variants run `sqlc diff` to fail on any byte drift in the golden output
against sqlc v1.31.1.

### Live-Postgres tests

`mise run test` (the `pytest` session) is the Tier-2 tier: it executes the
generated SQLAlchemy code against a real Postgres, reading the connection URI
from `$POSTGRES_URI`. Start and stop a matching local instance with:

```bash
mise run db:up
mise run db:down
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full list of tasks and the
connection details.

## License

MIT — see [LICENSE](LICENSE). This fork preserves the upstream copyright and
license; attribution and lineage are recorded in [NOTICE](NOTICE).

## Credits

This plugin builds on:

- [rayakame/sqlc-gen-better-python](https://github.com/rayakame/sqlc-gen-better-python) — the base this fork extends
- [sqlc-dev/sqlc](https://github.com/sqlc-dev/sqlc) — the SQL engine and plugin SDK
- [sqlc-gen-python](https://github.com/sqlc-dev/sqlc-gen-python) and [sqlc-gen-kotlin](https://github.com/sqlc-dev/sqlc-gen-kotlin) — prior-art generators
- [sqlc-gen-go](https://github.com/sqlc-dev/sqlc-gen-go) and [sqlc-gen-java](https://github.com/tandemdude/sqlc-gen-java) — references for plugin design
- [jinzhu/inflection](https://github.com/jinzhu/inflection) — singularization
