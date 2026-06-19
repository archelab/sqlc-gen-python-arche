-- embed_validate: a sqlc.embed of `author` whose jsonb `settings` column carries
-- a `validate: true` override. Proves the validated-adapter declaration set
-- recurses into embed fields (a flat scan would emit an undeclared _adapter).
CREATE TABLE IF NOT EXISTS author
(
    author_id integer NOT NULL,
    settings  jsonb   NOT NULL
);

CREATE TABLE IF NOT EXISTS book
(
    book_id   integer NOT NULL,
    title     text    NOT NULL,
    author_id integer NOT NULL
);
