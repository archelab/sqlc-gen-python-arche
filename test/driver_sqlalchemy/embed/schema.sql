-- P-22 sqlc.embed parity: a query embedding a full table composes that table's
-- model as a nested sub-struct field on the result Row (rather than flattening
-- its columns). `author` is embedded into the GetBookWithAuthor Row.
CREATE TABLE IF NOT EXISTS author
(
    author_id integer NOT NULL,
    name      text    NOT NULL
);

CREATE TABLE IF NOT EXISTS book
(
    book_id   integer NOT NULL,
    title     text    NOT NULL,
    author_id integer NOT NULL
);
