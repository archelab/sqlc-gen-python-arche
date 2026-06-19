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
# Tier-2 PROOF (GOAL §2/§3): the generated SQLAlchemy AsyncQuerier executed
# against a REAL sqlalchemy.ext.asyncio.AsyncConnection. The static gates
# (sqlc diff + pyright + ruff) cannot catch a runtime-only failure: a wrong
# `$N`->:pN / ::-escape / word-boundary-colon escape passes pyright but raises at
# execution (test_word_boundary_colon_literal_executes discriminates the
# colon-escape with a ` :b` literal SQLAlchemy WOULD parse as a bind), and a
# pydantic model with a datetime/decimal field that carries `from __future__
# import annotations` raises PydanticUserError when constructed. This module
# exercises both classes of failure end to end.
from __future__ import annotations

import datetime
import decimal

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.roundtrip.gen import models
from test.driver_sqlalchemy.roundtrip.gen import queries
from test.driver_sqlalchemy.roundtrip.gen.models import WidgetKind
from test.driver_sqlalchemy.roundtrip.shapes import WidgetMeta


def _params(ledger_id: int, note: str, amount: str, tags: list[str]) -> queries.InsertLedgerParams:
    return queries.InsertLedgerParams(
        ledger_id=ledger_id,
        note=note,
        amount=decimal.Decimal(amount),
        tag=tags,
        created_at=datetime.datetime(2026, 6, 18, 12, 0, 0, tzinfo=datetime.timezone.utc),
    )


@pytest.mark.asyncio(loop_scope="session")
async def test_insert_one_constructs_datetime_decimal_list_model(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # :one INSERT...RETURNING -> .first() -> construct a pydantic model carrying
    # datetime + decimal + list columns. This is the CRITICAL path: a
    # future-annotations / TYPE_CHECKING-only datetime would raise
    # PydanticUserError right here, from a frame that does not hold the bare names.
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    row = await querier.insert_ledger(_params(1, "first", "12.50", ["a", "b"]))
    assert row is not None
    assert isinstance(row, models.Ledger)
    assert row.ledger_id == 1
    assert row.amount == decimal.Decimal("12.50")
    assert isinstance(row.amount, decimal.Decimal)
    assert isinstance(row.created_at, datetime.datetime)
    assert list(row.tags) == ["a", "b"]


@pytest.mark.asyncio(loop_scope="session")
async def test_one_struct_and_none_path(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    await querier.insert_ledger(_params(2, "got", "1.00", ["x"]))

    got = await querier.get_ledger(ledger_id=2)
    assert got is not None
    assert got.note == "got"

    # :one with no matching row returns None (the `if row is None: return None`
    # branch).
    missing = await querier.get_ledger(ledger_id=9999)
    assert missing is None


@pytest.mark.asyncio(loop_scope="session")
async def test_one_scalar(sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection) -> None:
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    assert await querier.count_ledgers() == 0
    inserted = [(3, "2.00"), (4, "3.00")]
    for ledger_id, amount in inserted:
        await querier.insert_ledger(_params(ledger_id, "c", amount, []))
    assert await querier.count_ledgers() == len(inserted)


@pytest.mark.asyncio(loop_scope="session")
async def test_many_select_struct_streams_async_iterator(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # :many SELECT -> conn.stream(...) -> async generator. Consumed with
    # `async for`, proving the AsyncIterator surface and per-row model build.
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    await querier.insert_ledger(_params(5, "s1", "1.00", ["t"]))
    await querier.insert_ledger(_params(6, "s2", "2.00", []))

    seen: list[int] = []
    async for ledger in querier.list_ledgers():
        assert isinstance(ledger, models.Ledger)
        assert isinstance(ledger.amount, decimal.Decimal)
        seen.append(ledger.ledger_id)
    assert seen == [5, 6]


@pytest.mark.asyncio(loop_scope="session")
async def test_many_select_scalar_streams_bare_values(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    await querier.insert_ledger(_params(7, "n1", "1.00", []))
    await querier.insert_ledger(_params(8, "n2", "1.00", []))

    notes = [note async for note in querier.list_ledger_notes()]
    assert notes == ["n1", "n2"]


@pytest.mark.asyncio(loop_scope="session")
async def test_exec_returns_none(sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection) -> None:
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    await querier.insert_ledger(_params(9, "before", "1.00", []))
    result = await querier.touch_ledger(note="after", ledger_id=9)
    assert result is None
    got = await querier.get_ledger(ledger_id=9)
    assert got is not None
    assert got.note == "after"


@pytest.mark.asyncio(loop_scope="session")
async def test_execrows_returns_rowcount(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    dup_ids = [10, 11]
    keep_ids = [12]
    for ledger_id in [*dup_ids, *keep_ids]:
        note = "dup" if ledger_id in dup_ids else "keep"
        await querier.insert_ledger(_params(ledger_id, note, "1.00", []))

    deleted = await querier.delete_ledgers_by_note(note="dup")
    assert deleted == len(dup_ids)
    assert await querier.count_ledgers() == len(keep_ids)


@pytest.mark.asyncio(loop_scope="session")
async def test_p03_many_over_dml_uses_all_not_stream(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # P-03: a :many over UPDATE...RETURNING materializes via result.all() and
    # returns list[T]. Postgres rejects a server-side cursor (conn.stream) for a
    # DML statement, so a wrong .stream() emission would raise here at runtime.
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    await querier.insert_ledger(_params(13, "bump", "10.00", []))
    await querier.insert_ledger(_params(14, "bump", "20.00", []))

    bumped = await querier.bump_ledger_amounts(delta=decimal.Decimal("5.00"), note="bump")
    assert isinstance(bumped, list)
    assert {row.ledger_id: row.amount for row in bumped} == {
        13: decimal.Decimal("15.00"),
        14: decimal.Decimal("25.00"),
    }


@pytest.mark.asyncio(loop_scope="session")
async def test_word_boundary_colon_literal_executes(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # MAJOR-1 runtime proof: the body carries a WORD-BOUNDARY non-cast literal
    # colon ` :b` (escaped ` \:b`) alongside the `\:\:bigint` cast and the `:p1`
    # bind. SQLAlchemy's BIND_PARAMS regex `(?<![:\w\$\x5c]):([\w\$]+)(?![:\w\$])`
    # WOULD read an UNESCAPED ` :b` as a `:b` bind (the colon is preceded by a
    # space, not a word char), so a generator that fails to escape the literal —
    # or that wrongly escapes the `:p1` bind — passes pyright but raises here ("A
    # value is required for bind parameter b" / no value for p1). Only the correct
    # ` \:b` escape (backslash before colon — the regex lookbehind excludes `\x5c`)
    # binds exactly p1 and returns the literal ` :b` string. We seed a matching
    # row and a non-matching row to exercise the bound predicate too.
    # The seed itself must escape the colon (` \:b`): a raw `' :b'` in this
    # hand-written text() would be parsed by SQLAlchemy's BIND_PARAMS regex as a
    # `:b` bind and raise here too — the very failure the generator's escape
    # prevents. The escaped seed inserts the literal ` :b` string into the row.
    await sqlalchemy_conn.execute(
        sqlalchemy.text(r"INSERT INTO label_row (label_row_id, label) VALUES (1, ' \:b'), (2, 'other')")
    )
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    assert await querier.select_colon_literal(label_row_id=1) == " :b"
    assert await querier.select_colon_literal(label_row_id=2) is None


@pytest.mark.asyncio(loop_scope="session")
async def test_jsonb_override_direct_bind_round_trip(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # Runtime proof for the jsonb direct-bind (better-python #161). The overridden
    # `meta` jsonb param binds DIRECTLY in the generated code (`{"p2": meta}`,
    # never the non-callable typing.Any(meta) wrap); a wrong wrap passes pyright
    # but raises at runtime. The value is bound as a JSON string (asyncpg's native
    # jsonb form) and the dialect decodes the column back to a dict.
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    meta = WidgetMeta({"color": "red", "size": 3})
    await querier.insert_widget(
        widget_id=1,
        meta=meta.model_dump_json(),
        extra=None,
        kind=WidgetKind.ACTIVE,
    )

    # The jsonb value round-trips: the directly-bound payload reads back decoded.
    got_meta = await querier.get_widget_meta(widget_id=1)
    assert got_meta == {"color": "red", "size": 3}

    # The nullable jsonb round-trips NULL.
    assert await querier.get_widget_extra(widget_id=1) is None


@pytest.mark.asyncio(loop_scope="session")
async def test_str_enum_member_round_trip(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # Runtime proof for the enum.StrEnum emission. A StrEnum member is bound on
    # the write path ({"p4": kind}) and read back from the PG ENUM column: the
    # generator emits the column as `kind: WidgetKind`, and the golden + pyright
    # tier cannot prove the value actually round-trips value-in == member-out
    # against the live asyncpg/SQLAlchemy dialect. WidgetKind subclasses
    # enum.StrEnum, so the bound member adapts to the enum text on write; the
    # scalar read (`return row[0]`) returns that text, which equals the member.
    # The hyphenated `out-for-repair` value exercises a sanitized member name
    # (OUT_FOR_REPAIR) so a value-vs-member mismatch would surface here.
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    meta = WidgetMeta({})
    for widget_id, kind in ((10, WidgetKind.ARCHIVED), (11, WidgetKind.OUT_FOR_REPAIR)):
        await querier.insert_widget(
            widget_id=widget_id,
            meta=meta.model_dump_json(),
            extra=None,
            kind=kind,
        )

    got = await querier.get_widget_kind(widget_id=10)
    assert got == WidgetKind.ARCHIVED
    assert got == "archived"

    # The sanitized-member value round-trips to the correct member, not a sibling.
    repair = await querier.get_widget_kind(widget_id=11)
    assert repair == WidgetKind.OUT_FOR_REPAIR
    assert repair == "out-for-repair"

    # Missing row -> the `if row is None: return None` branch.
    assert await querier.get_widget_kind(widget_id=9999) is None


@pytest.mark.asyncio(loop_scope="session")
async def test_nullability_override_tighten_and_loosen_round_trip(
    sqlalchemy_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # Runtime proof for the nullability overrides. A TIGHTEN override
    # (nullable:false on the COALESCE result column) is a promise to pydantic
    # that the value is non-NULL; the type/golden tier cannot prove real Postgres
    # returns non-NULL. Seed a row where the COALESCE source is NULL — COALESCE
    # still yields 0, so the field must construct as a bare decimal.Decimal (not
    # Optional). The CASE-WHEN sibling is genuinely NULL there and stays None.
    await sqlalchemy_conn.execute(
        sqlalchemy.text("INSERT INTO charge (charge_id, penalty_amount) VALUES (1, NULL)")
    )
    querier = queries.AsyncQuerier(sqlalchemy_conn)
    row = await querier.get_charge_amounts(charge_id=1)
    assert row is not None
    # Tighten: COALESCE(NULL, 0) -> 0, constructed as a non-Optional Decimal.
    assert isinstance(row.charge_penalty_value, decimal.Decimal)
    assert row.charge_penalty_value == decimal.Decimal(0)
    # Loosen: the CASE-WHEN sibling is genuinely NULL and stays None.
    assert row.source_penalty_value is None

    # And a row where the source IS non-NULL: both reflect the real value.
    await sqlalchemy_conn.execute(
        sqlalchemy.text("INSERT INTO charge (charge_id, penalty_amount) VALUES (2, 7.50)")
    )
    row2 = await querier.get_charge_amounts(charge_id=2)
    assert row2 is not None
    assert row2.charge_penalty_value == decimal.Decimal("7.50")
    assert row2.source_penalty_value == decimal.Decimal("7.50")
