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
# Tier-2 PROOF (v0.5.3): a SINGLE-COLUMN (scalar, non-struct) result whose column
# is an array executed against a REAL AsyncConnection. RUNTIME FAILURE CLASS this
# discriminates: before the fix the scalar :one/:many return annotation took the
# bare inner type (`str`) and dropped the list wrapper, so the GENERATED reader
# was typed `str | None` / `AsyncIterator[str]` while Postgres actually hands back
# a Python list. A consumer iterating the value as a string (or a `set(value)`
# over the chars) is wrong at runtime, not just at the type level. arche reached
# this exact renderer path via COALESCE(array_agg(id), ARRAY[]::varchar[]) :one; a
# declared text[] column selected as a single column is the same path and seeds
# deterministically without aggregation. Each row uses asymmetric tags so a
# row-index/bind swap yields a visibly wrong list.
from __future__ import annotations

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.scalar_array_return.gen import queries


async def _seed(conn: sqlalchemy.ext.asyncio.AsyncConnection, rows: list[tuple[int, list[str]]]) -> None:
    for entity_id, tag_ids in rows:
        await conn.execute(
            sqlalchemy.text("INSERT INTO entity (entity_id, tag_ids) VALUES (:eid, :tags)"),
            {"eid": entity_id, "tags": tag_ids},
        )


@pytest.mark.asyncio(loop_scope="session")
async def test_scalar_array_one_returns_list(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # :one scalar array column -> list[str] | None. The value is a real Python
    # list, NOT a string: assert membership + length so a `str`-typed regression
    # (which would make `len` count characters and iteration yield single chars)
    # is caught at runtime.
    await _seed(case_conn, [(1, ["alpha", "beta"]), (2, ["gamma"])])
    querier = queries.AsyncQuerier(case_conn)

    tags = await querier.get_entity_tags(entity_id=1)
    assert isinstance(tags, list)
    assert tags == ["alpha", "beta"]
    assert len(tags) == 2

    single = await querier.get_entity_tags(entity_id=2)
    assert single == ["gamma"]

    # None path: an absent id returns None (not [] and not a raise).
    assert await querier.get_entity_tags(entity_id=999) is None


@pytest.mark.asyncio(loop_scope="session")
async def test_scalar_array_many_streams_lists(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # :many scalar array column -> AsyncIterator[list[str]]: each yielded element
    # is itself a list[str], proving the per-row value is not flattened to a str.
    await _seed(case_conn, [(10, ["x", "y"]), (11, []), (12, ["z"])])
    querier = queries.AsyncQuerier(case_conn)

    collected = [tags async for tags in querier.list_entity_tags()]
    assert all(isinstance(tags, list) for tags in collected)
    # Empty-array row round-trips as an empty list, not None.
    assert sorted(collected, key=len) == [[], ["z"], ["x", "y"]]
