-- Pins the three drop-in-from-upstream fixes (v0.5.2):
--   B: duplicate result-column de-collision (entity_id / entity_id_2)
--   C: emit_list_arrays:true -> array columns render builtin list[str]
--   E: a :many over WITH ... FOR UPDATE STREAMS (AsyncIterator), not list —
--      FOR UPDATE is a locking clause, not a data-modifying statement.
CREATE TABLE IF NOT EXISTS entity
(
    entity_id bigint NOT NULL,
    tag_ids   text[] NOT NULL,
    label     text   NOT NULL
);

CREATE TABLE IF NOT EXISTS link
(
    link_id   bigint NOT NULL,
    entity_id bigint NOT NULL,
    other_id  bigint NOT NULL
);
