-- name: GetCourse :one
SELECT id, class, "from", label FROM course WHERE id = sqlc.arg(id)::bigint;

-- name: ListCourses :many
SELECT id, class, "from", label FROM course ORDER BY id ASC;
