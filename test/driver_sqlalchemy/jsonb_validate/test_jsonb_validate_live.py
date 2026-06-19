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
# Tier-2 PROOF for the `validate: true` override knob, executed against a REAL
# Postgres connection. The generated read path is a cached
# `pydantic.TypeAdapter(<type>).validate_python(row[N])`, so a stored jsonb that
# does NOT match the declared shape raises pydantic.ValidationError at READ time
# (fail-loud) — a runtime guarantee the static gates cannot prove. This module
# exercises ALL FOUR validate emit paths:
#   * scalar :one (get_config, get_payload, get_extra),
#   * a NULLABLE scalar (get_extra) — a SQL NULL must come back None, not raise,
#   * a struct :one (get_widget) — the struct-field branch, nullable + non-null,
#   * a :many SELECT stream (list_configs) — the yield branch,
#   * a :many-over-DML RETURNING (delete_widgets_returning_config) — the list-comp
#     branch,
# plus the discriminated UNION A | B | C[D] (incl. the generic member) and the
# fail-loud behavior on every path.
from __future__ import annotations

import datetime
import decimal
import json
import typing

import pydantic
import pytest

from test.driver_sqlalchemy.jsonb_validate.gen import models
from test.driver_sqlalchemy.jsonb_validate.gen import queries
from test.driver_sqlalchemy.jsonb_validate.shapes import Dimensions
from test.driver_sqlalchemy.jsonb_validate.shapes import Item
from test.driver_sqlalchemy.jsonb_validate.shapes import ListPayload
from test.driver_sqlalchemy.jsonb_validate.shapes import NumberPayload
from test.driver_sqlalchemy.jsonb_validate.shapes import TextPayload
from test.driver_sqlalchemy.jsonb_validate.shapes import WidgetConfig

if typing.TYPE_CHECKING:
    import sqlalchemy.ext.asyncio

_VALID_CONFIG = WidgetConfig(
    name="primary",
    dimensions=Dimensions(width=320, height=200),
    tags=["alpha", "beta"],
    ratio=decimal.Decimal("1.5"),
    created=datetime.datetime(2026, 6, 19, 12, 0, tzinfo=datetime.timezone.utc),
    note="hello",
)
_VALID_CONFIG_JSON = _VALID_CONFIG.model_dump_json()
_TEXT_PAYLOAD_JSON = TextPayload(kind="text", text="hi").model_dump_json()
_MALFORMED_CONFIG_JSON = json.dumps({"name": "broken", "tags": [], "unexpected_key": 1})


async def _insert(
    conn: sqlalchemy.ext.asyncio.AsyncConnection,
    *,
    wid: int,
    config_json: str = _VALID_CONFIG_JSON,
    payload_json: str = _TEXT_PAYLOAD_JSON,
    extra_json: str | None = None,
) -> None:
    # Write params are the jsonb default (typing.Any): seed arbitrary (incl.
    # malformed) JSON strings bound to `::jsonb`; validation happens on READ.
    await queries.AsyncQuerier(conn).insert_widget(wid=wid, config=config_json, payload=payload_json, extra=extra_json)


@pytest.mark.asyncio(loop_scope="session")
async def test_nested_config_validates_and_round_trips(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    await _insert(case_conn, wid=1)
    got = await queries.AsyncQuerier(case_conn).get_config(wid=1)

    assert isinstance(got, WidgetConfig)
    assert got == _VALID_CONFIG
    assert isinstance(got.dimensions, Dimensions)
    assert got.dimensions == Dimensions(width=320, height=200)
    assert got.ratio == decimal.Decimal("1.5")
    assert got.created == _VALID_CONFIG.created
    assert got.tags == ["alpha", "beta"]
    assert got.enabled is True  # default applied by validation


@pytest.mark.asyncio(loop_scope="session")
async def test_malformed_config_raises_on_read(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # THE HEADLINE: store jsonb that violates WidgetConfig (missing the required
    # `dimensions` sub-model + an extra key under extra="forbid"). validate_python
    # raises; the old unwrapped `return row[0]` would SILENTLY return the bad dict.
    await _insert(case_conn, wid=2, config_json=_MALFORMED_CONFIG_JSON)
    with pytest.raises(pydantic.ValidationError):
        await queries.AsyncQuerier(case_conn).get_config(wid=2)


@pytest.mark.asyncio(loop_scope="session")
async def test_union_members_each_validate(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(case_conn)

    # TypeA — TextPayload
    await _insert(case_conn, wid=10, payload_json=TextPayload(kind="text", text="note").model_dump_json())
    a = await querier.get_payload(wid=10)
    assert isinstance(a, TextPayload)
    assert a.text == "note"

    # TypeB — NumberPayload (decimal inside the union)
    await _insert(
        case_conn,
        wid=11,
        payload_json=NumberPayload(kind="number", value=decimal.Decimal("3.14")).model_dump_json(),
    )
    b = await querier.get_payload(wid=11)
    assert isinstance(b, NumberPayload)
    assert b.value == decimal.Decimal("3.14")

    # TypeC[InnerD] — the GENERIC member ListPayload[Item]
    list_payload = ListPayload[Item](kind="list", items=[Item(id=1, label="x"), Item(id=2, label="y")])
    await _insert(case_conn, wid=12, payload_json=list_payload.model_dump_json())
    c = await querier.get_payload(wid=12)
    assert isinstance(c, ListPayload)
    assert [item.id for item in c.items] == [1, 2]
    assert c.items[0] == Item(id=1, label="x")


@pytest.mark.asyncio(loop_scope="session")
async def test_union_non_member_and_bad_body_raise(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(case_conn)

    await _insert(case_conn, wid=20, payload_json=json.dumps({"kind": "bogus", "x": 1}))
    with pytest.raises(pydantic.ValidationError):
        await querier.get_payload(wid=20)

    await _insert(case_conn, wid=21, payload_json=json.dumps({"kind": "list", "items": [{"id": 1}]}))
    with pytest.raises(pydantic.ValidationError):
        await querier.get_payload(wid=21)


@pytest.mark.asyncio(loop_scope="session")
async def test_nullable_extra_null_returns_none_value_validates_and_bad_raises(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(case_conn)

    # NULL-GUARD: extra omitted -> SQL NULL -> the read must return None, NOT raise
    # ValidationError against the non-Optional WidgetConfig. This is the path the
    # struct-field branch already guarded but the scalar/many paths did not.
    await _insert(case_conn, wid=30, extra_json=None)
    assert await querier.get_extra(wid=30) is None

    # A non-NULL value still validates to the typed model.
    await _insert(case_conn, wid=31, extra_json=_VALID_CONFIG_JSON)
    got = await querier.get_extra(wid=31)
    assert isinstance(got, WidgetConfig)
    assert got == _VALID_CONFIG

    # A non-NULL MALFORMED value still fails loud (the column being nullable does
    # not weaken validation of a present value).
    await _insert(case_conn, wid=32, extra_json=_MALFORMED_CONFIG_JSON)
    with pytest.raises(pydantic.ValidationError):
        await querier.get_extra(wid=32)


@pytest.mark.asyncio(loop_scope="session")
async def test_struct_read_validates_each_field(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(case_conn)

    wid = 40
    await _insert(case_conn, wid=wid, extra_json=_VALID_CONFIG_JSON)
    row = await querier.get_widget(wid=wid)
    assert isinstance(row, models.Widget)
    assert row.widget_id == wid
    # Each validated struct field carries its OWN validated value (row index map).
    assert isinstance(row.config, WidgetConfig)
    assert row.config == _VALID_CONFIG
    assert isinstance(row.payload, TextPayload)
    assert row.payload.text == "hi"
    assert isinstance(row.extra, WidgetConfig)
    assert row.extra == _VALID_CONFIG

    # The nullable struct field: extra NULL -> None inside the Row (guarded).
    await _insert(case_conn, wid=41, extra_json=None)
    row2 = await querier.get_widget(wid=41)
    assert isinstance(row2, models.Widget)
    assert row2.extra is None


@pytest.mark.asyncio(loop_scope="session")
async def test_list_configs_streams_validated_and_bad_raises(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(case_conn)

    wids = (50, 51)
    for wid in wids:
        await _insert(case_conn, wid=wid)
    streamed = [c async for c in querier.list_configs()]
    assert len(streamed) == len(wids)
    assert all(isinstance(c, WidgetConfig) for c in streamed)
    assert all(c == _VALID_CONFIG for c in streamed)

    # A malformed stored config makes the stream raise when it reaches that row.
    await _insert(case_conn, wid=52, config_json=_MALFORMED_CONFIG_JSON)
    with pytest.raises(pydantic.ValidationError):
        _ = [c async for c in querier.list_configs()]


@pytest.mark.asyncio(loop_scope="session")
async def test_delete_returning_many_validates_list(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(case_conn)

    await _insert(case_conn, wid=60)
    deleted = await querier.delete_widgets_returning_config(wid=60)
    assert isinstance(deleted, list)
    assert len(deleted) == 1
    assert isinstance(deleted[0], WidgetConfig)
    assert deleted[0] == _VALID_CONFIG
    # The DELETE persisted: the row is gone.
    assert await querier.get_config(wid=60) is None

    # A malformed config makes the RETURNING validation raise (fail-loud on the
    # list-comprehension path too).
    await _insert(case_conn, wid=61, config_json=_MALFORMED_CONFIG_JSON)
    with pytest.raises(pydantic.ValidationError):
        await querier.delete_widgets_returning_config(wid=61)
