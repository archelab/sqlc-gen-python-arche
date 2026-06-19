-- Pins the query-constant header MethodName byte-exactness (PRIMITIVES-A
-- review minor-1): buildQueryHeader emits query.MethodName VERBATIM in the
-- `-- name: <MethodName> :<verb>` line, and the constant is
-- UpperSnakeCase(name). many projects use snake_case query names (e.g.
-- `count_file_attachments_by_extraction_id`), so this case uses snake_case
-- names — the other verb cases use PascalCase and do NOT exercise the verbatim
-- snake_case header + the UPPER_SNAKE constant transform.
CREATE TABLE IF NOT EXISTS thing
(
    thing_id bigint NOT NULL,
    label    text   NOT NULL
);
