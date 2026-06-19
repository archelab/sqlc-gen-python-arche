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
# Tier-2 PROOF (P-17): the generated GET_ACCOUNT constant is a triple-quoted
# `typing.Final[str]` that SPANS MULTIPLE LINES and embeds a `/* ... */` block
# comment INSIDE the SELECT. sqlc diff + pyright + ruff cannot see a runtime-only
# failure here: a broken triple-quote (premature close), a dropped line, or a
# block comment that the asyncpg/SQLAlchemy parser chokes on all pass the static
# gates yet raise the moment `text(GET_ACCOUNT)` is executed against real PG.
# This module round-trips the case's OWN generated `get_account` against a real
# AsyncConnection: it seeds a row with DISTINCT, asymmetric account_id + label
# (so a row-index or field-name swap in the generated Account(...) build produces
# a visibly wrong value), asserts the row reads back through the multi-line
# commented statement, and covers the `-> Account | None` None path.
from __future__ import annotations

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.multiline_sql.gen import models
from test.driver_sqlalchemy.multiline_sql.gen import queries

# Distinct, asymmetric seed: account_id != label string, and the two rows differ,
# so a row[0]/row[1] swap or a fixed/first-row read surfaces a wrong value.
_ACCOUNT_ID = 7
_ACCOUNT_LABEL = "ledger-alpha"
_SIBLING_ID = 8
_SIBLING_LABEL = "ledger-beta"
_MISSING_ID = 9999


@pytest.mark.asyncio(loop_scope="session")
async def test_get_account_round_trips_multiline_commented_sql(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # Seed two rows with DISTINCT account_id AND DISTINCT label. There is no
    # generated writer for `account`, so the prerequisite rows are inserted with
    # raw SQL; the GENERATED reader (the multi-line, block-commented GET_ACCOUNT)
    # is the thing under test.
    await case_conn.execute(
        sqlalchemy.text("INSERT INTO account (account_id, label) VALUES (:aid, :label), (:sib_id, :sib_label)"),
        {"aid": _ACCOUNT_ID, "label": _ACCOUNT_LABEL, "sib_id": _SIBLING_ID, "sib_label": _SIBLING_LABEL},
    )
    querier = queries.AsyncQuerier(case_conn)

    # Executing GET_ACCOUNT proves the multi-line text() with the embedded
    # `/* ... */` block comment actually runs against real Postgres: a broken
    # triple-quoted constant or a comment the driver rejects would raise here.
    row = await querier.get_account(aid=_ACCOUNT_ID)
    assert row is not None
    assert isinstance(row, models.Account)
    # Discriminating: the WHERE bound :p1 selected this id (not the sibling), and
    # the fields map row[0]->account_id, row[1]->label. A swapped row index or
    # field name would fail one of these.
    assert row.account_id == _ACCOUNT_ID
    assert row.label == _ACCOUNT_LABEL

    # The sibling row binds the OTHER id and returns the OTHER label, proving the
    # :p1 bind actually filters (not a fixed/first-row read).
    other = await querier.get_account(aid=_SIBLING_ID)
    assert other is not None
    assert other.account_id == _SIBLING_ID
    assert other.label == _SIBLING_LABEL


@pytest.mark.asyncio(loop_scope="session")
async def test_get_account_none_path(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # `-> Account | None`: a no-match execution still runs the multi-line
    # commented statement and exercises the `if row is None: return None` branch.
    querier = queries.AsyncQuerier(case_conn)
    assert await querier.get_account(aid=_MISSING_ID) is None
