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
# Tier-2 PROOF (P-18, defaults-last column ordering): the generated
# AsyncQuerier.get_item executed against a REAL AsyncConnection. GetItem :one
# returns a Row that interleaves NOT NULL (item_id, quantity) and nullable
# (note, description) columns IN SELECT ORDER, and the generator builds the
# pydantic model positionally (item_id=row[0], note=row[1], quantity=row[2],
# description=row[3]). The static gates (sqlc diff + pyright + ruff) cannot
# catch a RUNTIME column-order regression: if row indices were misrouted
# (note<->description swapped, or quantity shifted), pyright still passes
# because every field stays `int`/`str | None`, but the wrong VALUE lands in
# the wrong field at execution. We seed ONE asymmetric row — note=NULL,
# description=a distinct string, quantity=a distinct int, item_id=a distinct
# int — so any field-order/row-index swap produces a visibly wrong result
# (a swap into the genuinely-NULL `note` would surface a non-None there, and a
# swap out of `description`/`quantity` would surface None / the wrong int).
# The case has no generated WRITER for `item`, so the row is seeded with raw
# SQL and read back through the GENERATED query. The None path (no matching
# iid -> the `if row is None: return None` branch) is covered too.
from __future__ import annotations

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.defaults_last.gen import models
from test.driver_sqlalchemy.defaults_last.gen import queries

# Distinct, asymmetric seed: item_id != quantity, note IS NULL while description
# carries a distinct string, so a misrouted row index between any field yields a
# visibly wrong value.
_ITEM_ID = 7
_QUANTITY = 42
_DESCRIPTION = "widget-alpha"
_MISSING_ITEM_ID = 9999


@pytest.mark.asyncio(loop_scope="session")
async def test_get_item_keeps_interleaved_columns_in_field_order(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # Seed note=NULL, a distinct description string, and a distinct quantity int,
    # so a misrouted row index between any of these fields yields a visibly wrong
    # value. item_id is a third distinct int so the WHERE bind + first field are
    # not confusable.
    await case_conn.execute(
        sqlalchemy.text("INSERT INTO item (item_id, note, quantity, description) VALUES (:iid, NULL, :qty, :descr)"),
        {"iid": _ITEM_ID, "qty": _QUANTITY, "descr": _DESCRIPTION},
    )

    querier = queries.AsyncQuerier(case_conn)
    got = await querier.get_item(iid=_ITEM_ID)
    assert got is not None
    assert isinstance(got, models.Item)
    # item_id round-trips into its own field (matches the WHERE bind, not a swap).
    assert got.item_id == _ITEM_ID
    # The NOT NULL int field carries the distinct seeded int (not item_id, not None).
    assert got.quantity == _QUANTITY
    # The nullable `note` is genuinely NULL: a swap from `description` would put
    # the string here, a swap from `quantity` would put the int here.
    assert got.note is None
    # The nullable `description` carries the distinct string: a swap with `note`
    # would null this out, a swap with `quantity` would put the int here.
    assert got.description == _DESCRIPTION


@pytest.mark.asyncio(loop_scope="session")
async def test_get_item_returns_none_for_missing_iid(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # The `-> models.Item | None` None path: an iid that matches no row hits the
    # `if row is None: return None` branch.
    querier = queries.AsyncQuerier(case_conn)
    assert await querier.get_item(iid=_MISSING_ITEM_ID) is None
