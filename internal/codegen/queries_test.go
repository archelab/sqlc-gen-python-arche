package codegen

import (
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
)

// TestBuildQueryHeaderKeepsEveryBackslash pins the constant site for every
// driver: each backslash of the SQL is doubled in the Python source, so the
// run-time string holds it once. For SQLAlchemy the colon escape is the
// rewrite's own `\:`, which the same encoding writes `\\:`. Doubling the
// backslashes AFTER a rewrite that already wrote `\\:` gives `\\\\:` and breaks
// every cast; no doubling at all leaves `\n` a newline.
func TestBuildQueryHeaderKeepsEveryBackslash(t *testing.T) {
	sql := `SELECT E'\n'::text AS nl, '^(4|5|6)\.' AS re, '"""' AS q, $1::text AS p`
	cases := map[core.SQLDriverType]string{
		core.SQLDriverSQLAlchemy: `Q: typing.Final[str] = """-- name: Q \\:one
SELECT E'\\n'\\:\\:text AS nl, '^(4|5|6)\\.' AS re, '""\"' AS q, :p1\\:\\:text AS p
"""
`,
		core.SQLDriverAsyncpg: `Q: typing.Final[str] = """-- name: Q :one
SELECT E'\\n'::text AS nl, '^(4|5|6)\\.' AS re, '""\"' AS q, $1::text AS p
"""
`,
		core.SQLDriverSQLite: `Q: typing.Final[str] = """-- name: Q :one
SELECT E'\\n'::text AS nl, '^(4|5|6)\\.' AS re, '""\"' AS q, $1::text AS p
"""
`,
		core.SQLDriverAioSQLite: `Q: typing.Final[str] = """-- name: Q :one
SELECT E'\\n'::text AS nl, '^(4|5|6)\\.' AS re, '""\"' AS q, $1::text AS p
"""
`,
	}
	for driverType, want := range cases {
		t.Run(driverType.String(), func(t *testing.T) {
			dr, err := NewDriver(&core.Config{SqlDriver: driverType})
			if err != nil {
				t.Fatal(err)
			}
			body := builders.NewIndentStringBuilder("    ", 1)
			dr.buildQueryHeader(&core.Query{ConstantName: "Q", MethodName: "Q", Cmd: ":one", SQL: sql}, body)
			if got := body.String(); got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}
