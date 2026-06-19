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
# Tier-2 PROOF (P-03 / C-#13): the four :many-over-DML methods executed against a
# REAL AsyncConnection. RUNTIME FAILURE CLASS this discriminates: Postgres
# rejects a server-side cursor (conn.stream) on a DML statement, so a :many over
# UPDATE/DELETE/INSERT...RETURNING MUST materialize via result.all(). A
# regression that re-emits these as conn.stream(...) passes sqlc diff + pyright
# (the SQL text and types are unchanged) but raises at execution here. The
# CTE-wrapped DELETE is the sharpest case: its top level is a SELECT (which a
# naive "is this a SELECT?" check would route to .stream) yet it contains a
# DELETE, so PG still refuses the cursor. Each test seeds DISTINCT, asymmetric
# widget rows (unique ids/names/statuses) so a row-index or bind-key swap yields
# a visibly wrong list, then asserts the returned list's contents AND that the
# DML actually mutated/removed/added the rows.
from __future__ import annotations

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.many_dml.gen import models
from test.driver_sqlalchemy.many_dml.gen import queries


async def _seed(conn: sqlalchemy.ext.asyncio.AsyncConnection, rows: list[tuple[int, str, str]]) -> None:
    # Seed prerequisite widget rows with raw SQL (distinct id/name/status per row)
    # so the GENERATED readers/DML below have asymmetric data to act on. The case
    # has a generated INSERT, but the update/delete/CTE tests seed directly to stay
    # independent of that one method.
    for widget_id, name, status in rows:
        await conn.execute(
            sqlalchemy.text("INSERT INTO widget (widget_id, name, status) VALUES (:wid, :n, :s)"),
            {"wid": widget_id, "n": name, "s": status},
        )


async def _statuses(conn: sqlalchemy.ext.asyncio.AsyncConnection) -> dict[int, str]:
    # Read the live table back through the raw boundary so post-DML assertions
    # observe the REAL persisted state, not the RETURNING projection alone.
    result = await conn.execute(sqlalchemy.text("SELECT widget_id, status FROM widget ORDER BY widget_id"))
    return {row[0]: row[1] for row in result.all()}


@pytest.mark.asyncio(loop_scope="session")
async def test_update_returning_many_materializes_and_mutates(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # UPDATE...RETURNING :many -> list[models.Widget]. A regression to conn.stream
    # would raise (server-side cursor on DML). Seed two "draft" rows + one "live"
    # decoy; the update must touch only the two drafts and return exactly them.
    await _seed(
        case_conn,
        [(1, "alpha", "draft"), (2, "beta", "draft"), (3, "gamma", "live")],
    )
    querier = queries.AsyncQuerier(case_conn)

    bumped = await querier.update_widgets_by_status(new_status="published", old_status="draft")
    assert isinstance(bumped, list)
    assert all(isinstance(w, models.Widget) for w in bumped)
    # Asymmetric ids->names prove no row-index / bind swap (status is the new
    # value, name pins each row to its identity).
    assert {w.widget_id: (w.name, w.status) for w in bumped} == {
        1: ("alpha", "published"),
        2: ("beta", "published"),
    }
    # The DML actually persisted: drafts became published, the live decoy is
    # untouched.
    assert await _statuses(case_conn) == {1: "published", 2: "published", 3: "live"}

    # None/empty path: an UPDATE matching nothing returns an empty list, not a
    # coroutine/iterator and not a raise.
    empty = await querier.update_widgets_by_status(new_status="x", old_status="nonexistent")
    assert empty == []


@pytest.mark.asyncio(loop_scope="session")
async def test_delete_returning_scalar_many(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # DELETE...RETURNING widget_id :many -> list[int] (scalar projection: row[0]).
    # Seed three rows across two statuses; delete one status and assert exactly
    # those ids come back AND the rows are gone.
    await _seed(
        case_conn,
        [(10, "ten", "stale"), (11, "eleven", "stale"), (12, "twelve", "fresh")],
    )
    querier = queries.AsyncQuerier(case_conn)

    deleted = await querier.delete_widget_ids(status="stale")
    assert isinstance(deleted, list)
    assert all(isinstance(i, int) for i in deleted)
    assert sorted(deleted) == [10, 11]
    # The DELETE persisted: only the "fresh" decoy survives.
    assert await _statuses(case_conn) == {12: "fresh"}

    # None/empty path: deleting an absent status returns [].
    assert await querier.delete_widget_ids(status="ghost") == []


@pytest.mark.asyncio(loop_scope="session")
async def test_insert_returning_many(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # INSERT...RETURNING :many -> list[models.Widget]. The single VALUES row
    # returns a one-element list; the inserted row must read back from the table.
    querier = queries.AsyncQuerier(case_conn)

    returned = await querier.insert_widget_returning(widget_id=20, name="delta", status="queued")
    assert isinstance(returned, list)
    assert len(returned) == 1
    row = returned[0]
    assert isinstance(row, models.Widget)
    assert (row.widget_id, row.name, row.status) == (20, "delta", "queued")
    # The RETURNING projection matches the persisted row (not just the bound args
    # echoed back): read the table independently.
    assert await _statuses(case_conn) == {20: "queued"}


@pytest.mark.asyncio(loop_scope="session")
async def test_cte_wrapped_delete_many_struct(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # WITH del AS (DELETE...RETURNING) SELECT ... :many -> list[DeleteWidgetsViaCTERow].
    # SHARPEST stream-regression catch: the statement's top level is a SELECT, so a
    # generator that routes "SELECT" to conn.stream would emit a server-side cursor
    # — which PG STILL rejects because the CTE contains a DELETE. result.all() is
    # mandatory. Seed asymmetric rows; delete one status and assert the struct list
    # AND that the rows are gone.
    await _seed(
        case_conn,
        [(30, "thirty", "expired"), (31, "thirtyone", "expired"), (32, "thirtytwo", "active")],
    )
    querier = queries.AsyncQuerier(case_conn)

    removed = await querier.delete_widgets_via_c_t_e(status="expired")
    assert isinstance(removed, list)
    assert all(isinstance(r, queries.DeleteWidgetsViaCTERow) for r in removed)
    assert {r.widget_id: (r.name, r.status) for r in removed} == {
        30: ("thirty", "expired"),
        31: ("thirtyone", "expired"),
    }
    # The CTE DELETE persisted: only the "active" decoy remains.
    assert await _statuses(case_conn) == {32: "active"}

    # None/empty path: no matching status -> empty list.
    assert await querier.delete_widgets_via_c_t_e(status="missing") == []
