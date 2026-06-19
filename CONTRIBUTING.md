# sqlc-gen-python-arche contribution guidelines

Thank you for investing your time trying to improve this plugin. We have some contribution guidelines
that you should follow to ensure that your contribution is at its best.

# Toolchain & tasks (mise)

This repo uses [mise](https://mise.jdx.dev) to pin the system toolchain (Go,
`uv`, `sqlc`, `changie`) and to run every task. Install mise, then:

```bash
mise install        # provision the pinned toolchain
mise tasks          # list every task
```

A running Docker daemon is the one prerequisite mise cannot provide (it is
needed for the live-Postgres tests).

# CI

CI runs the `nox` pipelines (Python) plus the Go tests; `mise run check`
reproduces the full gate locally. Under the hood `nox` is driven through `uv`,
so the Python dependency set is the frozen `uv.lock`.

Common tasks (run `mise tasks` for the full list):

| Task | What it does |
|------|--------------|
| `mise run build` | Build the wasm plugin and re-pin every `sqlc.yaml` sha256 |
| `mise run golden` | `sqlc diff` + pyright + ruff over every golden tree |
| `mise run lint` | pyright + ruff |
| `mise run format` | ruff format + import sort |
| `mise run go-test` | Go unit + architecture tests |
| `mise run test` | live-Postgres round-trip tests (needs `mise run db:up`) |
| `mise run check` | the full CI-equivalent gate (needs `mise run db:up`) |

The `test`/`check` tasks need a local Postgres. mise reads the URI from
`$POSTGRES_URI` (set in `mise.toml`); start and stop a matching instance with:

```bash
mise run db:up      # start Postgres (credentials match $POSTGRES_URI)
mise run db:down    # stop and remove it
```

<details>
    <summary>Running the pipelines directly (without mise)</summary>

```bash
uv sync --group dev          # install nox into the project venv
uv run nox                   # run all important pipelines
uv run nox -s sqlalchemy_check pytest
```

You will also need [sqlc](https://docs.sqlc.dev/en/latest/overview/install.html)
and Go 1.24.1 on your PATH, plus a local Postgres for `pytest`. The `pytest`
pipeline reads its connection URI from the `POSTGRES_URI` environment variable.
</details>

The generated-code pipelines build the WASM plugin from source first (see
`scripts/build/build.sh`), so you do not need to commit or pre-build the wasm —
each `nox` session that runs `sqlc generate`/`sqlc diff` rebuilds it from the Go
source under `plugin/` + `internal/`.

Useful sessions:

- `asyncpg`, `aiosqlite`, `sqlite3`, `sqlalchemy` — generate, type-check (pyright)
  and lint (ruff) each driver's output.
- `asyncpg_check`, `aiosqlite_check`, `sqlite3_check`, `sqlalchemy_check` — the
  same, but `sqlc diff` (assert the committed goldens are byte-current) instead of
  regenerating.
- `deletion_proof` — regenerate the consolidated shape repro under
  `test/shape_repro/` and assert it is byte-identical to the committed baseline.
- `pyright`, `ruff`, `pytest` — the cross-cutting type, lint, and live-Postgres
  round-trip checks.

Before committing we recommend you run `nox` to run all important pipelines and make sure they won't fail.

You may run a single pipeline with `nox -s name` or multiple pipelines with `nox -s name1 name3 name9`.

# Changelog fragments

We use [changie](https://changie.dev/) to manage changelog creation.

Every PR needs to have a changelog fragment for that to work.
Please refer to the [changie documentation](https://changie.dev/guide/installation/) for information about installing changie.

After installing changie you can run

```cmd
changie new
```

To create the needed changelog fragment. Changie will ask you for the following fields:

- Kind: The kind of changes, should be self explanatory
- Body: A short description about the made changes.
- PR: The number of the pull request associated to the changes.
- Github Name: The **username** of the github account that made the changes. This is used for giving credits to contributors in the changelog.
