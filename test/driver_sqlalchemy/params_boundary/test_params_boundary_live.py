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
# Tier-2 PROOF: the generated AsyncQuerier executed against a REAL
# sqlalchemy.ext.asyncio.AsyncConnection. The keyword-only-vs-bundle param
# boundary (qpl=2) and the P-16 flat-arg de-collision are byte-verified by sqlc
# diff + pyright, but those gates cannot see a runtime-only bind-key mismatch.
# duplicate_names compiles `UPDATE thing SET status=:p1 WHERE status=:p2` with
# {"p1": status, "p2": status_2}; a swapped/wrong bind-key passes pyright but at
# execution updates the wrong row (or nothing). test_duplicate_names_binds_set_vs_where
# discriminates that by reading the row back: only $1->SET and $2->WHERE turns the
# OLD row into NEW while leaving a sibling row untouched. The remaining tests
# smoke one_param / at_limit_two_params / above_limit_three_params (the three
# arity rungs of the qpl boundary) executing at the real boundary without raising.
from __future__ import annotations

import pytest
import sqlalchemy
import sqlalchemy.ext.asyncio

from test.driver_sqlalchemy.params_boundary.gen import queries


async def _seed_thing(
    conn: sqlalchemy.ext.asyncio.AsyncConnection,
    *,
    thing_id: int,
    a: str,
    b: str,
    c: str,
    status: str,
) -> None:
    # No generated WRITER for `thing`; seed prerequisite rows with raw SQL, then
    # exercise the GENERATED updaters. Distinct, asymmetric values per column so a
    # bind-key swap (p1<->p2, or a wrong field read on the bundled param) produces
    # a visibly wrong row.
    await conn.execute(
        sqlalchemy.text("INSERT INTO thing (thing_id, a, b, c, status) VALUES (:thing_id, :a, :b, :c, :status)"),
        {"thing_id": thing_id, "a": a, "b": b, "c": c, "status": status},
    )


async def _read_thing(conn: sqlalchemy.ext.asyncio.AsyncConnection, *, thing_id: int) -> tuple[str, str, str, str]:
    row = (
        await conn.execute(
            sqlalchemy.text("SELECT a, b, c, status FROM thing WHERE thing_id = :thing_id"),
            {"thing_id": thing_id},
        )
    ).first()
    assert row is not None
    return (row[0], row[1], row[2], row[3])


@pytest.mark.asyncio(loop_scope="session")
async def test_duplicate_names_binds_set_vs_where(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # CRITICAL discriminator for the P-16 de-collided binds. Seed a row at
    # status='OLD' (the SET target) and a sibling at status='KEEP' (must not be
    # touched). duplicate_names(status='NEW', status_2='OLD') must compile
    # `SET status=:p1('NEW') WHERE status=:p2('OLD')`. A p1<->p2 swap would write
    # `SET status='OLD' WHERE status='NEW'`, match nothing, and leave the row at
    # 'OLD' -> the first assertion fails. The KEEP sibling proves the WHERE filter
    # ($2) actually scoped the update instead of touching every row.
    await _seed_thing(case_conn, thing_id=1, a="a1", b="b1", c="c1", status="OLD")
    await _seed_thing(case_conn, thing_id=2, a="a2", b="b2", c="c2", status="KEEP")

    result = await queries.AsyncQuerier(case_conn).duplicate_names(status="NEW", status_2="OLD")
    assert result is None  # :exec returns None

    _, _, _, status_target = await _read_thing(case_conn, thing_id=1)
    assert status_target == "NEW"  # $1 bound the SET value
    _, _, _, status_sibling = await _read_thing(case_conn, thing_id=2)
    assert status_sibling == "KEEP"  # $2 scoped the WHERE; sibling untouched


@pytest.mark.asyncio(loop_scope="session")
async def test_one_param_keyword_only_executes(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # 1 param < qpl(2): keyword-only (self, *, a). Updates a on thing_id=1.
    await _seed_thing(case_conn, thing_id=1, a="a-old", b="b1", c="c1", status="s1")
    result = await queries.AsyncQuerier(case_conn).one_param(a="a-new")
    assert result is None
    a, b, c, _ = await _read_thing(case_conn, thing_id=1)
    assert a == "a-new"  # :p1 bound the SET value
    assert (b, c) == ("b1", "c1")  # only column a moved


@pytest.mark.asyncio(loop_scope="session")
async def test_at_limit_two_params_keyword_only_executes(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # 2 params == qpl(2): still keyword-only (self, *, a, b), NOT bundled. Seed
    # distinct old values so a p1<->p2 swap would land 'a-new' in column b.
    await _seed_thing(case_conn, thing_id=1, a="a-old", b="b-old", c="c1", status="s1")
    result = await queries.AsyncQuerier(case_conn).at_limit_two_params(a="a-new", b="b-new")
    assert result is None
    a, b, c, _ = await _read_thing(case_conn, thing_id=1)
    assert (a, b) == ("a-new", "b-new")  # :p1->a, :p2->b, not swapped
    assert c == "c1"  # column c untouched


@pytest.mark.asyncio(loop_scope="session")
async def test_above_limit_three_params_bundled_executes(
    case_conn: sqlalchemy.ext.asyncio.AsyncConnection,
) -> None:
    # 3 params > qpl(2): bundles into AboveLimitThreeParamsParams passed
    # positionally; body reads arg.a/arg.b/arg.c into :p1/:p2/:p3. Distinct old
    # values so any field/bind misread lands the wrong value in the wrong column.
    await _seed_thing(case_conn, thing_id=1, a="a-old", b="b-old", c="c-old", status="s1")
    params = queries.AboveLimitThreeParamsParams(a="a-new", b="b-new", c="c-new")
    result = await queries.AsyncQuerier(case_conn).above_limit_three_params(params)
    assert result is None
    a, b, c, _ = await _read_thing(case_conn, thing_id=1)
    assert (a, b, c) == ("a-new", "b-new", "c-new")
