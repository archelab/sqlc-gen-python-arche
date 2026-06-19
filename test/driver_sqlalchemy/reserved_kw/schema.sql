-- Proves P-14 (C-#14): a column named a TRUE Python keyword (`class`) is
-- escaped to `class_` and carries the alias via the ANNOTATED pattern
-- (`class_: typing.Annotated[str, pydantic.Field(alias="class")]`) + a
-- class-level `model_config = pydantic.ConfigDict(populate_by_name=True)`, so
-- the row maps to the real `class` column AND the generated row construction
-- `models.Course(class_=row[1])` type-checks under pyright (a bare
-- `= pydantic.Field(alias=...)` would make the type checker expect the ALIAS
-- name in __init__ and reject `class_=`). A column named `id` is NOT a keyword
-- (it is a
-- valid identifier and a mere builtin) and stays RAW `id: int` — byte-faithful
-- to the reference models.py, which has raw `id` columns and ZERO aliases.
-- `from_` similarly proves the keyword set beyond `class`.
--
-- It ALSO proves the flat-param escape uses the SAME keyword-only predicate:
-- `get_course(self, *, id: int)` with bind `{"p1": id}` (raw `id`, NOT `id_`),
-- byte-matching the reference get_account_balance_sheet_by_id(self, *, id: str). A flat
-- `id` param and the model FIELD named `id` must agree — escaping the param to
-- `id_` while the field stays `id` was an internal inconsistency.
CREATE TABLE IF NOT EXISTS course
(
    id     bigint NOT NULL,
    class  text   NOT NULL,
    "from" text   NOT NULL,
    label  text   NOT NULL
);
