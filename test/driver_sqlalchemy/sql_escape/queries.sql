-- MAJOR-1: the F1 body rewrite escapes EVERY colon, not just casts.
--  * `count(*)::bigint`            -> a `::` cast              -> `\\:\\:`
--  * the literal string 'a:b'      -> a NON-CAST literal colon -> 'a\\:b'
--  * `$1`                          -> the bind placeholder     -> `:p1`

-- name: CountByLiteralColon :one
SELECT count(*)::bigint AS c
FROM label_row
WHERE label = 'a:b'
  AND label_row_id = sqlc.arg(label_row_id)::bigint;

-- Every backslash below must reach Postgres as written. The query constant is
-- a Python string that is not raw, so the generator writes each backslash as
-- `\\`. Without that, Python reads `\n`, `\u001f`, `\'` and `\1` as escapes and
-- `\\` as one backslash, and each query below returns another value (the live
-- test compares each one with the same SQL run directly). Every query also
-- carries a `::` cast: a backslash escape applied after the colon escape writes
-- `\\\\:` and breaks the cast.

-- name: NewlineEscapes :one
SELECT E'a\nb'::text AS e_string,
       'a\nb'::text  AS standard_string;

-- name: UnitSeparatorEscapes :one
SELECT E'a\u001fb'::text AS e_string,
       'a\u001fb'::text  AS standard_string;

-- name: RegexDot :one
SELECT ('4.' ~ '^(4|5|6)\.')::bool    AS escaped_dot_matches_dot,
       ('4x' ~ '^(4|5|6)\.')::bool    AS escaped_dot_rejects_other,
       ('4\.' ~ '^(4|5|6)\\.')::bool  AS escaped_backslash_matches_backslash,
       ('4.' ~ '^(4|5|6)\\.')::bool   AS escaped_backslash_rejects_dot;

-- name: LikeEscapedPercent :one
SELECT ('a%b' LIKE 'a\%b' ESCAPE '\')::bool AS escaped_percent_matches_percent,
       ('axb' LIKE 'a\%b' ESCAPE '\')::bool AS escaped_percent_rejects_other;

-- name: Backreference :one
SELECT regexp_replace('abc', '(a)', '\1\1')::text AS doubled;

-- SQLAlchemy unescapes only the backslash directly before a colon, and the
-- colon escape puts its own backslash there, so the SQL's backslash stays.
-- name: BackslashBeforeColon :one
SELECT 'a\:b'::text AS backslash_colon;

-- A `"""` in the SQL would end the Python string: the generator escapes it.
-- name: TripleQuote :one
SELECT '"""'::text AS triple_quote,
       '\"""'::text AS backslash_triple_quote;
