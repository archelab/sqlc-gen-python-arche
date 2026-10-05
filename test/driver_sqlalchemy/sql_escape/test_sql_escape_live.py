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
#
# Tier-2 PROOF: every backslash of a query reaches Postgres as written. The
# query constant is a Python string that is not raw, so a backslash the
# generator does not double is a Python escape: `\n` and `\u001f` change the
# text, `\'` ends `ESCAPE '\'` early, `\1` becomes chr(1), and `\\` becomes one
# backslash. Each of those queries still runs, so only a comparison of values
# shows the fault. Each test runs the GENERATED method and the same SQL from
# queries.sql directly on the asyncpg connection (no SQLAlchemy text(), no
# Python escape), and both must return the expected row. Every query carries a
# `::` cast, so a backslash escape applied after the colon escape (`\\\\:`)
# fails here as a syntax error.
from __future__ import annotations

import pathlib
import re
import typing

import pydantic
import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.sql_escape.gen import models
from test.driver_sqlalchemy.sql_escape.gen import queries

if typing.TYPE_CHECKING:
    import asyncpg

_QUERIES_SQL = pathlib.Path(__file__).parent / "queries.sql"
_HEADER = re.compile(r"-- name: (\w+) :\w+$")

_EXPECTED: dict[str, tuple[object, ...]] = {
    "NewlineEscapes": ("a\nb", "a\\nb"),
    "UnitSeparatorEscapes": ("a\x1fb", "a\\u001fb"),
    "RegexDot": (True, False, True, False),
    "LikeEscapedPercent": (True, False),
    "Backreference": ("aabc",),
    "BackslashBeforeColon": ("a\\:b",),
    "TripleQuote": ('"""', '\\"""'),
}


def _source_sql() -> dict[str, str]:
    # The SQL of each `-- name:` block as written: the full-line comments sqlc
    # drops (a `--` at column 0) and the final `;` dropped, every backslash kept.
    blocks: dict[str, list[str]] = {}
    lines: list[str] | None = None
    for line in _QUERIES_SQL.read_text(encoding="utf-8").splitlines():
        if header := _HEADER.match(line):
            lines = blocks.setdefault(header.group(1), [])
        elif lines is not None and not line.startswith("--"):
            lines.append(line)
    return {name: "\n".join(body).strip().removesuffix(";") for name, body in blocks.items()}


def _method_name(query_name: str) -> str:
    return re.sub(r"(?<!^)(?=[A-Z])", "_", query_name).lower()


def _values(result: object) -> tuple[object, ...]:
    if isinstance(result, pydantic.BaseModel):
        return tuple(result.model_dump().values())
    return (result,)


def test_every_expected_query_is_in_the_source() -> None:
    assert set(_EXPECTED) <= set(_source_sql())


@pytest.mark.parametrize("query_name", sorted(_EXPECTED))
@pytest.mark.asyncio(loop_scope="session")
async def test_generated_query_returns_what_its_source_sql_returns(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
    query_name: str,
) -> None:
    querier = queries.AsyncQuerier(case_conn)
    generated = _values(await getattr(querier, _method_name(query_name))())

    raw = await case_conn.get_raw_connection()
    driver = typing.cast("asyncpg.Connection[asyncpg.Record]", raw.driver_connection)
    record = await driver.fetchrow(_source_sql()[query_name])
    assert record is not None
    direct = tuple(record.values())

    assert direct == _EXPECTED[query_name]
    assert generated == direct


@pytest.mark.asyncio(loop_scope="session")
async def test_literal_colon_and_bind_round_trip(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # The escaped literal colon 'a:b' and the `:p1` bind next to a cast.
    await case_conn.execute(
        sqlalchemy.text("INSERT INTO label_row (label_row_id, label) VALUES (1, 'a\\:b'), (2, 'a\\:c')"),
    )
    querier = queries.AsyncQuerier(case_conn)
    assert await querier.count_by_literal_colon(label_row_id=1) == 1
    assert await querier.count_by_literal_colon(label_row_id=2) == 0


@pytest.mark.asyncio(loop_scope="session")
async def test_schema_text_reaches_python_as_written(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # The enum labels, the type and table comments (docstrings) and the
    # two-line column comment (a # block) come from schema.sql. The labels must
    # equal what Postgres returns, or a read of a stored label fails.
    raw = await case_conn.get_raw_connection()
    driver = typing.cast("asyncpg.Connection[asyncpg.Record]", raw.driver_connection)
    direct = await driver.fetchval("SELECT array_agg(m::text) FROM unnest(enum_range(NULL::escape_mood)) AS m")
    expected = ["plain", "back\\slash", 'say "hi"']
    assert list(direct) == expected
    assert [member.value for member in models.EscapeMood] == expected

    assert models.EscapeMood.__doc__ == 'a label can hold "quotes" and \\ backslashes'
    assert models.LabelRow.__doc__ == 'rows with a "quoted" end"'
