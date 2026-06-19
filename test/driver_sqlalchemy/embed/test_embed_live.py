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
# Tier-2 PROOF (sqlc.embed parity): the generated AsyncQuerier executed against a
# REAL AsyncConnection. sqlc.embed(author) composes the `author` table into a
# nested Author sub-struct; the row layout is book_id=row[0], the embed consumes
# row[1]/row[2] (author_id/name), and title=row[3] BECAUSE the running index
# advanced by len(EmbedFields). RUNTIME BUG THIS DISCRIMINATES: a regression that
# re-uses the column's loop position would mis-scan `title` to row[2] (the
# author's NAME) instead of row[3]. That passes pyright and sqlc diff (the Row
# shape is unchanged) but returns the wrong string at execution. We seed an
# author and a book whose name, title, book_id and author_id are ALL distinct,
# call get_book_with_author, and assert each field is its OWN value — so a
# row[2]/row[3] confusion (title == author.name) fails the assertion. The None
# path (no matching book) is covered too.
from __future__ import annotations

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.embed.gen import models
from test.driver_sqlalchemy.embed.gen import queries

# Distinct, asymmetric seed values: author_id != book_id and author.name !=
# book.title, so a title<-row[2] mis-scan (the named runtime bug) or a book_id/
# author_id swap surfaces a visibly wrong field. Named constants keep the seed
# and the assertions reading from one source.
_AUTHOR_ID = 42
_AUTHOR_NAME = "Ursula Le Guin"
_BOOK_ID = 7
_BOOK_TITLE = "The Dispossessed"
_MISSING_BOOK_ID = 9999


@pytest.mark.asyncio(loop_scope="session")
async def test_embed_nested_author_and_post_embed_column_round_trip(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # No generated writer for author/book -> seed prerequisite rows with raw SQL.
    await case_conn.execute(
        sqlalchemy.text("INSERT INTO author (author_id, name) VALUES (:author_id, :name)"),
        {"author_id": _AUTHOR_ID, "name": _AUTHOR_NAME},
    )
    await case_conn.execute(
        sqlalchemy.text("INSERT INTO book (book_id, title, author_id) VALUES (:book_id, :title, :author_id)"),
        {"book_id": _BOOK_ID, "title": _BOOK_TITLE, "author_id": _AUTHOR_ID},
    )

    querier = queries.AsyncQuerier(case_conn)
    row = await querier.get_book_with_author(bid=_BOOK_ID)

    assert row is not None
    assert isinstance(row, queries.GetBookWithAuthorRow)

    # row[0] -> book_id: its own value, not the author_id.
    assert row.book_id == _BOOK_ID
    # The embed consumes row[1]/row[2] into the nested Author sub-struct.
    assert isinstance(row.author, models.Author)
    assert row.author.author_id == _AUTHOR_ID
    assert row.author.name == _AUTHOR_NAME
    # row[3] -> title, selected AFTER the embed. If the running-index advance
    # regressed and title were scanned at row[2], this would read the author's
    # name instead. Asserting the exact distinct title discriminates that bug.
    assert row.title == _BOOK_TITLE
    assert row.title != row.author.name


@pytest.mark.asyncio(loop_scope="session")
async def test_embed_none_path_for_unmatched_book(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # The :one signature is `-> GetBookWithAuthorRow | None`; a bid with no
    # matching book hits the `if row is None: return None` branch.
    querier = queries.AsyncQuerier(case_conn)
    assert await querier.get_book_with_author(bid=_MISSING_BOOK_ID) is None
