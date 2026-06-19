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
# Tier-2 PROOF: a `validate: true` override on a jsonb column that lives INSIDE a
# sqlc.embed. The embedded Author's `settings` is read via
# `_AuthorSettings_adapter.validate_python(row[N])`, so the adapter must be
# declared at module level AND `AuthorSettings` imported into the query file even
# though it is never a query signature type (it surfaces only as a field of the
# embedded model). This case pins both: the generated code imports + runs against
# a real Postgres, validating the embedded jsonb and failing loud on a mismatch.
from __future__ import annotations

import json

import pydantic
import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.embed_validate.gen import models
from test.driver_sqlalchemy.embed_validate.gen import queries
from test.driver_sqlalchemy.embed_validate.shapes import AuthorSettings

_SETTINGS = AuthorSettings(theme="dark", notifications=False)
_SETTINGS_JSON = _SETTINGS.model_dump_json()


async def _seed(
    conn: sqlalchemy.ext.asyncio.AsyncConnection,
    *,
    author_id: int,
    settings_json: str,
    book_id: int,
    title: str,
) -> None:
    await conn.execute(
        sqlalchemy.text("INSERT INTO author (author_id, settings) VALUES (:aid, :settings)"),
        {"aid": author_id, "settings": settings_json},
    )
    await conn.execute(
        sqlalchemy.text("INSERT INTO book (book_id, title, author_id) VALUES (:bid, :title, :aid)"),
        {"bid": book_id, "title": title, "aid": author_id},
    )


@pytest.mark.asyncio(loop_scope="session")
async def test_embedded_jsonb_column_validates(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    book_id = 10
    await _seed(case_conn, author_id=1, settings_json=_SETTINGS_JSON, book_id=book_id, title="The Dispossessed")
    row = await queries.AsyncQuerier(case_conn).get_book_with_author(bid=book_id)

    assert row is not None
    assert isinstance(row, queries.GetBookWithAuthorRow)
    assert row.book_id == book_id
    assert row.title == "The Dispossessed"
    # The embedded sub-struct's validated jsonb field is a real AuthorSettings,
    # not a raw dict — proving the embedded `_AuthorSettings_adapter` was declared,
    # imported, and executed.
    assert isinstance(row.author, models.Author)
    assert row.author.author_id == 1
    assert isinstance(row.author.settings, AuthorSettings)
    assert row.author.settings == _SETTINGS


@pytest.mark.asyncio(loop_scope="session")
async def test_embedded_jsonb_mismatch_raises(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # The stored embedded settings violate AuthorSettings (missing `theme`, plus
    # an extra key under extra="forbid"). Reading the embed validates it and
    # raises — the fail-loud guarantee holds inside a sqlc.embed too.
    bad = json.dumps({"notifications": True, "unexpected": 1})
    await _seed(case_conn, author_id=2, settings_json=bad, book_id=11, title="x")
    with pytest.raises(pydantic.ValidationError):
        await queries.AsyncQuerier(case_conn).get_book_with_author(bid=11)
