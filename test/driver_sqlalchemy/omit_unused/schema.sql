-- P-19 omit_unused_models: `used_table` is referenced by a query; `unused_table`
-- is referenced by NO query. With omit_unused_models:true the generated
-- models.py must contain class Used and must NOT contain class Unused.
CREATE TABLE IF NOT EXISTS used_table
(
    used_id integer NOT NULL,
    label   text    NOT NULL
);

CREATE TABLE IF NOT EXISTS unused_table
(
    unused_id integer NOT NULL,
    note      text    NOT NULL
);
