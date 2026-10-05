package builders

import (
	"strings"
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/metadata"
)

// TestQueryDocstringSQLEchoKeepsEveryBackslash pins the second site that copies
// SQL into Python source: the docstring echo is a `"""` string too, so a
// backslash must be doubled and a `"""` escaped there as well.
func TestQueryDocstringSQLEchoKeepsEveryBackslash(t *testing.T) {
	convention, emitSQL := core.DocstringConventionGoogle, true
	SetDocstringConfig(&convention, &emitSQL, core.SQLDriverAsyncpg)
	b := NewIndentStringBuilder("    ", 1)
	query := &core.Query{MethodName: "Q", Cmd: metadata.CmdExec, SQL: "SELECT E'\\n', '\"\"\"'\nWHERE x ~ '\\.'\nORDER BY 1 COLLATE \"C\""}
	b.WriteQueryFunctionDocstring(1, query, "", nil, core.PyType{})
	got := b.String()
	for _, want := range []string{`    SELECT E'\\n', '""\"'` + "\n", `    WHERE x ~ '\\.'` + "\n", `    ORDER BY 1 COLLATE "C"` + "\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
