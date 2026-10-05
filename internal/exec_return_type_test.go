package internal

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// execStatementQueries is one :exec per statement kind, keyed by the generated
// function name. sqlc passes the result columns of every statement, whatever
// the verb, so a SELECT or a RETURNING under :exec still carries columns here.
func execStatementQueries() map[string]*plugin.Query {
	col := func(name, typ string) *plugin.Column {
		return &plugin.Column{Name: name, NotNull: true, Type: &plugin.Identifier{Name: typ}}
	}
	return map[string]*plugin.Query{
		// arche's AcquirePaymentAuthorizationMutationLock: a SELECT of a void
		// function. Before the fix: `-> typing.Any` with no return.
		"lock_widgets": {Name: "LockWidgets", Cmd: metadata.CmdExec, Filename: "queries.sql",
			Text:    "SELECT lock_widgets()",
			Columns: []*plugin.Column{col("lock_widgets", "void")}},
		// A SELECT of a value. Before the fix: `-> int` with no return.
		"select_one": {Name: "SelectOne", Cmd: metadata.CmdExec, Filename: "queries.sql",
			Text:    "SELECT 1 AS one",
			Columns: []*plugin.Column{col("one", "integer")}},
		"insert_widget_returning": {Name: "InsertWidgetReturning", Cmd: metadata.CmdExec, Filename: "queries.sql",
			Text:    "INSERT INTO widgets (widget_id) VALUES ($1) RETURNING widget_id",
			Columns: []*plugin.Column{col("widget_id", "integer")},
			Params:  []*plugin.Parameter{{Number: 1, Column: col("widget_id", "integer")}}},
		"select_two_columns": {Name: "SelectTwoColumns", Cmd: metadata.CmdExec, Filename: "queries.sql",
			Text:    "SELECT widget_id, widget_id AS other FROM widgets",
			Columns: []*plugin.Column{col("widget_id", "integer"), col("other", "integer")}},
		"touch_widgets": {Name: "TouchWidgets", Cmd: metadata.CmdExec, Filename: "queries.sql",
			Text: "UPDATE widgets SET widget_id = widget_id"},
	}
}

// pyFunction returns the `def <name>(` line and the body lines of a generated
// function.
func pyFunction(t *testing.T, src, name string) (string, []string) {
	t.Helper()
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "def "+name+"(") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		var body []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " ")) <= indent {
				break
			}
			body = append(body, next)
		}
		return line, body
	}
	t.Fatalf("no function %s in:\n%s", name, src)
	return "", nil
}

// TestExecIsTypedNone pins that a :exec function is annotated `-> None` for
// every statement kind and every driver. The function discards the result, so
// a return type taken from the result columns is false: pyright accepts it for
// typing.Any, and a caller can read the None as a value.
func TestExecIsTypedNone(t *testing.T) {
	for _, tc := range []struct{ driver, engine, modelType string }{
		{"sqlalchemy", "postgresql", "pydantic"},
		{"asyncpg", "postgresql", "dataclass"},
		{"sqlite3", "sqlite", "dataclass"},
		{"aiosqlite", "sqlite", "dataclass"},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			queries := execStatementQueries()
			var reqQueries []*plugin.Query
			for _, q := range queries {
				reqQueries = append(reqQueries, q)
			}
			resp, err := Generate(context.Background(), &plugin.GenerateRequest{
				Catalog: &plugin.Catalog{
					DefaultSchema: "public",
					Schemas: []*plugin.Schema{{
						Name: "public",
						Tables: []*plugin.Table{{Rel: ident("widgets"), Columns: []*plugin.Column{
							{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "integer"}, Table: ident("widgets")},
						}}},
					}},
				},
				Queries:  reqQueries,
				Settings: &plugin.Settings{Engine: tc.engine},
				PluginOptions: []byte(fmt.Sprintf(
					`{"package": "m", "sql_driver": %q, "model_type": %q, "emit_classes": true, "emit_init_file": false}`,
					tc.driver, tc.modelType)),
			})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			var src string
			for _, f := range resp.Files {
				if f.Name == "queries.py" {
					src = string(f.Contents)
				}
			}
			for name := range queries {
				def, body := pyFunction(t, src, name)
				if !strings.HasSuffix(def, "-> None:") {
					t.Errorf("%s: want `-> None:`, got %q", name, strings.TrimSpace(def))
				}
				for _, line := range body {
					if strings.HasPrefix(strings.TrimSpace(line), "return ") {
						t.Errorf("%s: a :exec function returns nothing, got %q", name, strings.TrimSpace(line))
					}
				}
			}
			if strings.Contains(src, "class SelectTwoColumnsRow") {
				t.Error("a :exec emits no result Row class")
			}
		})
	}
}
