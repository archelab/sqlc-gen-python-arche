"""Typed JSON shapes for the jsonb_validate case.

Two jsonb columns are overridden with `validate: true`, so the generated READ
path fails loud — `pydantic.TypeAdapter(<type>).validate_python(row[0])` — instead
of returning the decoded value unwrapped:

  * `config`  -> WidgetConfig, a NESTED pydantic model with edge-case fields
                 (a sub-model, a list, decimal, datetime, a default, an optional)
                 and `extra="forbid"`, so a stored row with the wrong shape raises.
  * `payload` -> WidgetPayload, a discriminated UNION `A | B | C[D]`
                 (TextPayload | NumberPayload | ListPayload[Item]) — proving a jsonb
                 field can be typed as several alternatives, including a GENERIC
                 member, and validated against all of them at read time.
"""

from __future__ import annotations

import datetime
import decimal
import typing

import pydantic


class Dimensions(pydantic.BaseModel):
    """A nested sub-model embedded inside WidgetConfig."""

    model_config = pydantic.ConfigDict(extra="forbid")

    width: int
    height: int


class WidgetConfig(pydantic.BaseModel):
    """Nested config shape with edge-case field types. `extra="forbid"` makes an
    unexpected key in the stored jsonb fail validation (fail-loud)."""

    model_config = pydantic.ConfigDict(extra="forbid")

    name: str
    dimensions: Dimensions
    tags: list[str]
    ratio: decimal.Decimal
    created: datetime.datetime
    enabled: bool = True
    note: str | None = None


# --- discriminated UNION: TypeA | TypeB | TypeC[InnerD] -----------------------
_T = typing.TypeVar("_T")


class TextPayload(pydantic.BaseModel):
    model_config = pydantic.ConfigDict(extra="forbid")
    kind: typing.Literal["text"]
    text: str


class NumberPayload(pydantic.BaseModel):
    model_config = pydantic.ConfigDict(extra="forbid")
    kind: typing.Literal["number"]
    value: decimal.Decimal


class Item(pydantic.BaseModel):
    """The InnerD of the generic member ListPayload[Item]."""

    model_config = pydantic.ConfigDict(extra="forbid")
    id: int
    label: str


class ListPayload(pydantic.BaseModel, typing.Generic[_T]):
    """The generic member TypeC[InnerD] — a list of an inner typed shape."""

    model_config = pydantic.ConfigDict(extra="forbid")
    kind: typing.Literal["list"]
    items: list[_T]


# WidgetPayload = A | B | C[D], tagged on `kind`. pydantic selects the member by
# the Literal discriminator; a payload whose `kind` matches no member, or whose
# body is wrong for its kind, raises pydantic.ValidationError at read time.
WidgetPayload: typing.TypeAlias = typing.Annotated[
    TextPayload | NumberPayload | ListPayload[Item],
    pydantic.Field(discriminator="kind"),
]
