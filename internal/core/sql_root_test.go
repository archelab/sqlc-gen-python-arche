package core

import "testing"

// TestSQLRootIsDML pins the P-03 detection: a :many over INSERT/UPDATE/DELETE
// is DML (must materialize via .all()); a SELECT (incl. CTE/leading comments)
// is not (must stream).
func TestSQLRootIsDML(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want bool
	}{
		{"select", "SELECT a FROM t", false},
		{"insert returning", "INSERT INTO t (a) VALUES ($1) RETURNING a", true},
		{"update returning", "UPDATE t SET a = $1 RETURNING a", true},
		{"delete returning", "DELETE FROM t WHERE a = $1 RETURNING a", true},
		{"lowercase update", "update t set a = 1 returning a", true},
		{"leading whitespace + newline", "\n\t  DELETE FROM t RETURNING a", true},
		{"leading line comment", "-- name: X\nUPDATE t SET a = 1 RETURNING a", true},
		{"leading block comment", "/* c */ INSERT INTO t (a) VALUES (1) RETURNING a", true},
		{"select after comment", "-- c\nSELECT a FROM t", false},
		{"identifier starting with keyword is not DML", "SELECT updates FROM t", false},
		{"with cte select", "WITH x AS (SELECT 1) SELECT * FROM x", false},
		{"writable cte delete", "WITH del AS (DELETE FROM t WHERE a = $1 RETURNING a) SELECT a FROM del", true},
		{"writable cte insert", "WITH ins AS (INSERT INTO t (a) VALUES ($1) RETURNING a) SELECT a FROM ins", true},
		{"writable cte update", "WITH upd AS (UPDATE t SET a = $1 RETURNING a) SELECT a FROM upd", true},
		{"read-only cte with deleted_at column", "WITH x AS (SELECT deleted_at FROM t) SELECT * FROM x", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SQLRootIsDML(tc.sql); got != tc.want {
				t.Fatalf("SQLRootIsDML(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}
