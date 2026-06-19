"""Hand-written typed JSON shape imported by the generated roundtrip code.

Models a known jsonb payload so the `widget.meta` column can be typed via a
per-column override instead of the typing.Any default. The generated SQLAlchemy
code never CONSTRUCTS this type on the write path: the override base-type cast
was dropped for SQLAlchemy (better-python #161; `typing.Any(...)` is not
callable), so the value binds directly for the dialect to adapt. A pydantic
RootModel gives a valid runtime field-annotation type with no required
positional fields.
"""

import pydantic


class WidgetMeta(pydantic.RootModel[dict[str, object]]):
    """Typed shape for widget.meta jsonb (bound directly, read back as the model)."""
