-- The Row composes the embedded `author` table as a nested Author sub-struct
-- field alongside the book's own columns. A plain column (`book.title`) is
-- selected AFTER `sqlc.embed(author)` so its row index can only be produced by
-- the running-index advance (rowIdx += len(EmbedFields)) the driver applies for
-- an embed: book_id=row[0], the embed consumes row[1]/row[2], title=row[3]. A
-- regression that re-used the column's loop position would mis-scan `title` to
-- row[2] and `sqlc diff` would catch it. (`SELECT col, embed, col2` — the exact
-- shape the row-index comment in sqlalchemyRowKwargs guards.)
-- name: GetBookWithAuthor :one
SELECT book.book_id,
       sqlc.embed(author),
       book.title
FROM book
JOIN author ON author.author_id = book.author_id
WHERE book.book_id = sqlc.arg(bid)::integer;
