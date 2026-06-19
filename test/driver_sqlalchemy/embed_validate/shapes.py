"""Typed JSON shape for the embedded author.settings jsonb column (validated)."""

from __future__ import annotations

import pydantic


class AuthorSettings(pydantic.BaseModel):
    """A small validated shape carried by an embedded column."""

    model_config = pydantic.ConfigDict(extra="forbid")

    theme: str
    notifications: bool = True
