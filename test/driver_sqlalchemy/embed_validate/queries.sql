-- The embedded `author` composes a nested Author sub-struct whose `settings`
-- field is a validated jsonb override: the generated Row build validates it via
-- `_AuthorSettings_adapter.validate_python(row[N])`, so the adapter must be
-- declared at module level (the bug this case pins) and `import pydantic` added.
-- name: GetBookWithAuthor :one
SELECT book.book_id,
       sqlc.embed(author),
       book.title
FROM book
JOIN author ON author.author_id = book.author_id
WHERE book.book_id = sqlc.arg(bid)::integer;
