-- P-17: the query SQL body spans multiple lines AND carries a multi-line SQL
-- comment INSIDE the statement. The generated `typing.Final[str] = """..."""`
-- constant must keep every line intact (no broken triple-quoted string, no
-- premature close), and the F1 colon-escape must apply across all lines.
-- name: GetAccount :one
SELECT account_id,
       label
FROM account
/* multi-line
   block comment
   inside the SQL */
WHERE account_id = sqlc.arg(aid)::integer;
