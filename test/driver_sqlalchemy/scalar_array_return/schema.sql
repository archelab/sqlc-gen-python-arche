-- v0.5.3: a SINGLE-COLUMN (scalar, non-struct) result whose column is an array.
-- The struct path (a Row field) already wrapped arrays via pyFieldType, but the
-- scalar :one/:many return annotation took the bare inner type (query.Ret.Typ.Type)
-- and DROPPED the list wrapper — so an array-valued scalar return rendered `str`
-- instead of `list[str]`. arche hit this via a COALESCE(array_agg(id), ARRAY[]::
-- varchar[]) :one result column (is_array=true under the DB-backed analyzer); a
-- declared text[] column selected as a single column reaches the identical
-- renderer path and pins it hermetically (no DB needed).
CREATE TABLE IF NOT EXISTS entity
(
    entity_id bigint NOT NULL,
    tag_ids   text[] NOT NULL
);
