-- name: OneParam :exec
-- 1 param, below qpl(2): keyword-only `(self, *, a)`.
UPDATE thing SET a = sqlc.arg(a)::text WHERE thing_id = 1;

-- name: AtLimitTwoParams :exec
-- 2 params == qpl(2): still keyword-only `(self, *, a, b)`, NOT bundled.
UPDATE thing SET a = sqlc.arg(a)::text, b = sqlc.arg(b)::text WHERE thing_id = 1;

-- name: AboveLimitThreeParams :exec
-- 3 params > qpl(2): bundles into AboveLimitThreeParamsParams passed
-- positionally as `arg`; body reads arg.field.
UPDATE thing SET a = sqlc.arg(a)::text, b = sqlc.arg(b)::text, c = sqlc.arg(c)::text WHERE thing_id = 1;

-- name: DuplicateNames :exec
-- Two DISTINCT positional params ($1, $2) both resolve to column `status`, so
-- ParamName yields the base name `status` for both -> de-collided to `status`
-- / `status_2` (P-16 flat-arg de-collision, the SAME rule columnsToStruct
-- uses). Each is a distinct placeholder, so the bind dict keys on the real
-- numbers ({"p1": status, "p2": status_2}).
UPDATE thing SET status = $1 WHERE status = $2;
