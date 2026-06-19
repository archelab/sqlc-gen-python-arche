# Copyright (c) 2025 Rayakame

# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:

# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.

# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.
from __future__ import annotations

import pathlib
import re
import typing

import pytest_asyncio
import sqlalchemy
import sqlalchemy.ext.asyncio

if typing.TYPE_CHECKING:
    import collections.abc

    import pytest

ROUNDTRIP_PATH = pathlib.Path(__file__).parent / "roundtrip"


def _asyncpg_dsn(raw: str) -> str:
    # The harness may pass `postgresql://...` (the local/canonical form) or the
    # legacy `postgres://...` alias (the CI Postgres service URL uses it).
    # SQLAlchemy dropped the bare `postgres` dialect, and its async engine needs
    # the explicit `postgresql+asyncpg://` dialect+driver scheme, so normalise
    # BOTH aliases here. A DSN that already names a driver (`postgresql+...`) is
    # passed through unchanged. Check the longer prefix first so `postgresql://`
    # is never partial-matched by the `postgres://` arm.
    for prefix in ("postgresql://", "postgres://"):
        if raw.startswith(prefix):
            return "postgresql+asyncpg://" + raw[len(prefix) :]
    return raw


def _strip_sql_comments(sql: str) -> str:
    # Drop full-line `--` comments so a multi-line comment block above a CREATE
    # is not glued onto the statement (asyncpg's simple-query path rejects a bare
    # leading comment as syntax).
    return "\n".join(line for line in sql.splitlines() if not line.lstrip().startswith("--"))


def _statements(sql: str) -> collections.abc.Iterator[str]:
    # Splitting on `;` is sufficient for this schema (no semicolons inside
    # literals/bodies).
    for statement in _strip_sql_comments(sql).split(";"):
        stripped = statement.strip()
        if stripped:
            yield stripped


_CREATE_TABLE_RE = re.compile(r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)", re.IGNORECASE)
_CREATE_TYPE_RE = re.compile(r"CREATE\s+TYPE\s+(\w+)", re.IGNORECASE)


def _drop_statements(sql: str) -> collections.abc.Iterator[str]:
    # Derive the teardown DROP list from schema.sql so setup and teardown share
    # one source of truth: any table/type the schema adds is dropped, none is
    # leaked, and the roundtrip case only touches its OWN objects (it must not
    # wipe the `public` schema the asyncpg/sqlite suites share). Tables first
    # (CASCADE clears dependents), then the enum types they referenced.
    body = _strip_sql_comments(sql)
    for table in _CREATE_TABLE_RE.findall(body):
        yield f"DROP TABLE IF EXISTS {table} CASCADE"
    for typ in _CREATE_TYPE_RE.findall(body):
        yield f"DROP TYPE IF EXISTS {typ} CASCADE"


async def _yield_conn_for_schema(
    dsn: str,
    schema_path: pathlib.Path,
) -> collections.abc.AsyncGenerator[sqlalchemy.ext.asyncio.AsyncConnection, typing.Any]:
    # Shared body for the live-PG fixtures. The schema at `schema_path` is
    # (re)created in a committed setup, then a connection is yielded inside one
    # open transaction whose own uncommitted writes are visible to later reads
    # and rolled back on teardown for per-test isolation.
    #
    # Setup drops exactly the tables/types the schema declares (derived from it,
    # not a hardcoded list — the old `DROP TABLE` pair leaked any newly-added
    # table or enum), then recreates them. It drops only its OWN objects, never
    # the whole `public` schema the asyncpg/sqlite suites share.
    engine = sqlalchemy.ext.asyncio.create_async_engine(dsn)
    schema = schema_path.read_text(encoding="utf-8")
    try:
        async with engine.begin() as setup:
            for drop in _drop_statements(schema):
                await setup.execute(sqlalchemy.text(drop))
            for statement in _statements(schema):
                await setup.execute(sqlalchemy.text(statement))
        async with engine.connect() as conn:
            yield conn
            await conn.rollback()
    finally:
        await engine.dispose()


@pytest_asyncio.fixture(loop_scope="session")
async def sqlalchemy_conn(
    request: pytest.FixtureRequest,
) -> collections.abc.AsyncGenerator[sqlalchemy.ext.asyncio.AsyncConnection, typing.Any]:
    # The roundtrip kitchen-sink case (explicit path). Yields the SAME
    # sqlalchemy.ext.asyncio.AsyncConnection the consuming app code passes to
    # AsyncQuerier(conn). Delegates to the shared body; the explicit `gen.aclose()`
    # guarantees the inner teardown (rollback + engine.dispose) runs when pytest
    # closes the fixture, which a bare `async for ... yield` would not promise.
    dsn = _asyncpg_dsn(typing.cast("str", request.config.getoption("--db")))
    gen = _yield_conn_for_schema(dsn, ROUNDTRIP_PATH / "schema.sql")
    try:
        async for conn in gen:
            yield conn
    finally:
        await gen.aclose()


@pytest_asyncio.fixture(loop_scope="session")
async def case_conn(
    request: pytest.FixtureRequest,
) -> collections.abc.AsyncGenerator[sqlalchemy.ext.asyncio.AsyncConnection, typing.Any]:
    # Per-case live-PG connection: derives schema.sql from the REQUESTING test
    # module's own directory, so every `test/driver_sqlalchemy/<case>/` test
    # round-trips that case's already-golden-verified `gen/` against real
    # Postgres with zero per-case fixture wiring. The invariant this enforces: a
    # runtime-behavioral case proves itself at the real boundary (a wrong row
    # index, bind-key, or reserved-kw field mapping raises at execution), not just
    # through `sqlc diff` + pyright, which cannot see a runtime-only failure.
    dsn = _asyncpg_dsn(typing.cast("str", request.config.getoption("--db")))
    schema_path = pathlib.Path(request.path).parent / "schema.sql"
    gen = _yield_conn_for_schema(dsn, schema_path)
    try:
        async for conn in gen:
            yield conn
    finally:
        await gen.aclose()
