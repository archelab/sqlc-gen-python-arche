CREATE TABLE IF NOT EXISTS label_row
(
    label_row_id bigint NOT NULL,
    label        text   NOT NULL
);

-- Schema text reaches Python source too: the enum labels become "..."
-- literals, the type and table comments become docstrings, and the column
-- comment becomes a # block.
CREATE TYPE escape_mood AS ENUM ('plain', 'back\slash', 'say "hi"');

COMMENT ON TYPE escape_mood IS 'a label can hold "quotes" and \ backslashes';

COMMENT ON TABLE label_row IS 'rows with a "quoted" end"';

COMMENT ON COLUMN label_row.label IS E'first line\nsecond line';
