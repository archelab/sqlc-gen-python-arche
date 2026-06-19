"""Hand-written typed JSON shape imported by the generated jsonb_typed code.

Models a known JSONB payload shape so a jsonb column can be typed via a per-
column override (instead of the typing.Any default). The generated SQLAlchemy
code never CONSTRUCTS this type: a scalar :one read returns the value unwrapped
(`return row[0]`), and a write param binds it directly (`{"p2": config}`) for the
dialect to adapt — the override base-type cast was dropped for SQLAlchemy
(better-python #161; `typing.Any(...)` would not be callable). A pydantic
RootModel gives a valid runtime field-annotation type with no required
positional fields, matching how the raw decoded JSON flows through unconstructed.
"""

import pydantic


class WidgetConfig(pydantic.RootModel[dict[str, object]]):
    """Typed shape for widget.config jsonb (read unwrapped, bound directly)."""
