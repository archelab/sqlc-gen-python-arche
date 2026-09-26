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
# Tier-2 PROOF for the two :many SELECT fetch shapes against a REAL
# AsyncConnection: the default buffered `conn.execute` + `for row in result`
# generator, and the `-- @stream` query that keeps `conn.stream` + `async for`.
# Both must yield the seeded rows with the right row-index mapping, and a
# consumer must be able to run another query on the same connection while it
# iterates the buffered generator (the rows are already fetched, so the
# connection is free between yields).
from __future__ import annotations

import datetime

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.many_stream.gen import models
from test.driver_sqlalchemy.many_stream.gen import queries

_T0 = datetime.datetime(2026, 1, 1, tzinfo=datetime.UTC)


async def _seed(conn: sqlalchemy.ext.asyncio.AsyncConnection) -> None:
    # Three asymmetric rows: two expired at different times, one without expiry.
    for attachment_id, upload_id, expires_in_days in ((1, "up-b", 2), (2, "up-a", 1), (3, "up-c", None)):
        await conn.execute(
            sqlalchemy.text(
                "INSERT INTO file_attachment (file_attachment_id, upload_id, user_id, created_at, expires_at) VALUES (:fid, :uid, 'user', :created, :expires)"
            ),
            {
                "fid": attachment_id,
                "uid": upload_id,
                "created": _T0,
                "expires": None if expires_in_days is None else _T0 + datetime.timedelta(days=expires_in_days),
            },
        )


@pytest.mark.asyncio(loop_scope="session")
async def test_buffered_many_yields_struct_rows_in_order(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    await _seed(case_conn)
    querier = queries.AsyncQuerier(case_conn)

    rows = [
        r async for r in querier.list_expired_file_attachments(now=_T0 + datetime.timedelta(days=10), limit_count=10)
    ]

    assert all(isinstance(r, models.FileAttachment) for r in rows)
    assert [(r.file_attachment_id, r.upload_id, r.expires_at) for r in rows] == [
        (2, "up-a", _T0 + datetime.timedelta(days=1)),
        (1, "up-b", _T0 + datetime.timedelta(days=2)),
    ]


@pytest.mark.asyncio(loop_scope="session")
async def test_buffered_many_allows_a_query_inside_the_loop(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    await _seed(case_conn)
    querier = queries.AsyncQuerier(case_conn)

    seen: list[tuple[str, int]] = []
    async for upload_id in querier.list_upload_ids():
        # A second statement on the SAME connection while the generator is
        # suspended between yields.
        count = (
            await case_conn.execute(
                sqlalchemy.text("SELECT count(*) FROM file_attachment WHERE upload_id <= :u"), {"u": upload_id}
            )
        ).scalar_one()
        seen.append((upload_id, count))

    assert seen == [("up-a", 1), ("up-b", 2), ("up-c", 3)]


@pytest.mark.asyncio(loop_scope="session")
async def test_stream_marked_many_yields_every_row(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    await _seed(case_conn)
    querier = queries.AsyncQuerier(case_conn)

    assert sorted([u async for u in querier.list_all_upload_ids()]) == ["up-a", "up-b", "up-c"]
