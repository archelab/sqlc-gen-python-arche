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
# Tier-2 PROOF for the reserved_kw case: the generated SQLAlchemy AsyncQuerier
# executed against a REAL AsyncConnection. The static gates (sqlc diff + pyright
# + ruff) cannot catch a runtime-only failure of the reserved-keyword field
# mapping. The generated row construction is positional —
# `models.Course(id=row[0], class_=row[1], from_=row[2], label=row[3])` — so a
# wrong row index, or a mangled field (`class_`/`from_`) wired to the wrong
# column, type-checks under pyright but mis-routes the value at execution. By
# seeding DISTINCT, asymmetric values for class / "from" / label via raw INSERT
# and reading them back through the generated mapping, this test fails loudly if
# any reserved-kw field carries another column's value. It also covers the
# None path (`if row is None: return None`) and the :many AsyncIterator surface.
from __future__ import annotations

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.reserved_kw.gen import models
from test.driver_sqlalchemy.reserved_kw.gen import queries

# Distinct, asymmetric seed values: id / class / "from" / label all differ, so a
# row-index swap or a class_<->from_<->label mis-wire produces a visibly wrong
# field. The bare strings are interned literals here; they must round-trip
# THROUGH Postgres and the generated mapping to satisfy the assertions below.
_COURSE_ID = 7
_COURSE_CLASS = "biology"
_COURSE_FROM = "harvard"
_COURSE_LABEL = "fall-2026"


async def _seed_course(
    conn: sqlalchemy.ext.asyncio.AsyncConnection,
    *,
    course_id: int,
    klass: str,
    frm: str,
    label: str,
) -> None:
    # No generated WRITER for `course`, so seed the prerequisite row with raw SQL.
    # `from` is a reserved word in raw SQL too and must stay double-quoted; the
    # values bind as parameters so the seed itself does not depend on quoting.
    await conn.execute(
        sqlalchemy.text('INSERT INTO course (id, class, "from", label) VALUES (:id, :class, :from, :label)'),
        {"id": course_id, "class": klass, "from": frm, "label": label},
    )


@pytest.mark.asyncio(loop_scope="session")
async def test_get_course_maps_reserved_kw_fields_to_own_columns(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # The core discriminator: each mangled field must hold ITS OWN column's value.
    # If `from_` were wired to row[1] (class) or row[3] (label), or `class_` to
    # the wrong index, the asymmetric seed makes the mismatch fail an assertion.
    await _seed_course(
        case_conn,
        course_id=_COURSE_ID,
        klass=_COURSE_CLASS,
        frm=_COURSE_FROM,
        label=_COURSE_LABEL,
    )

    course = await queries.AsyncQuerier(case_conn).get_course(id=_COURSE_ID)
    assert course is not None
    assert isinstance(course, models.Course)

    assert course.id == _COURSE_ID
    # `class_` (alias "class") carries the class column, not from/label.
    assert course.class_ == _COURSE_CLASS
    assert course.class_ != _COURSE_FROM
    assert course.class_ != _COURSE_LABEL
    # `from_` (alias "from") carries the from column, not class/label.
    assert course.from_ == _COURSE_FROM
    assert course.from_ != _COURSE_CLASS
    assert course.from_ != _COURSE_LABEL
    # `label` is a plain (non-keyword) field carrying its own column.
    assert course.label == _COURSE_LABEL


@pytest.mark.asyncio(loop_scope="session")
async def test_get_course_none_path(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # :one with no matching row exercises the `if row is None: return None` branch.
    assert await queries.AsyncQuerier(case_conn).get_course(id=9999) is None


@pytest.mark.asyncio(loop_scope="session")
async def test_list_courses_streams_reserved_kw_rows(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # :many SELECT -> conn.stream(...) -> AsyncIterator. Two rows with distinct
    # class/from/label per row prove ORDER BY id ASC ordering AND that the
    # reserved-kw mapping holds per row, not just on the :one path.
    seeded = (
        (_COURSE_ID, _COURSE_CLASS, _COURSE_FROM, _COURSE_LABEL),
        (_COURSE_ID + 1, "chemistry", "stanford", "spring-2027"),
    )
    for course_id, klass, frm, label in seeded:
        await _seed_course(
            case_conn,
            course_id=course_id,
            klass=klass,
            frm=frm,
            label=label,
        )

    rows: list[models.Course] = []
    async for course in queries.AsyncQuerier(case_conn).list_courses():
        assert isinstance(course, models.Course)
        rows.append(course)

    assert [(c.id, c.class_, c.from_, c.label) for c in rows] == list(seeded)
