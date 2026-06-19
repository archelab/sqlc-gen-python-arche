-- P-18 (defaults-last ordering regression). pydantic Row/Params fields carry NO
-- `= default` (the reference models.py has zero field defaults — nullable
-- columns are plain `T | None`). So a Row mixing NOT NULL and nullable columns
-- must emit every field in COLUMN ORDER with no reordering and no `= None`
-- trailer; this golden pins that the defaults-last machinery never injects a
-- default into the pydantic emitter (which would break a downstream Row built by
-- positional/keyword construction).
CREATE TABLE IF NOT EXISTS item
(
    item_id     integer NOT NULL,
    note        text,
    quantity    integer NOT NULL,
    description  text
);
