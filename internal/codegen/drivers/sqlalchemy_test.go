package drivers

import (
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
)

// TestSQLAlchemyRewriteSQL pins the escape rule: escape EVERY colon to `\\:`
// first (cast AND non-cast literal colons), THEN rewrite `$N` -> `:pN`. The
// order is load-bearing — escaping first ensures the inserted `:pN` bind
// markers are not themselves escaped. The emitted text carries two backslash
// characters before each escaped colon (Go raw string `\\:`).
func TestSQLAlchemyRewriteSQL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "cast becomes double-escaped",
			in:   "SELECT count(*)::bigint",
			want: `SELECT count(*)\\:\\:bigint`,
		},
		{
			name: "non-cast literal colon also escapes",
			in:   "WHERE label = 'a:b'",
			want: `WHERE label = 'a\\:b'`,
		},
		{
			name: "placeholder becomes bind, inserted colon NOT escaped",
			in:   "WHERE id = $1",
			want: `WHERE id = :p1`,
		},
		{
			name: "placeholder with cast: bind unescaped, cast escaped",
			in:   "WHERE extraction_id = $1::text",
			want: `WHERE extraction_id = :p1\\:\\:text`,
		},
		{
			name: "multi placeholders",
			in:   "VALUES ($1::text, $2::bigint)",
			want: `VALUES (:p1\\:\\:text, :p2\\:\\:bigint)`,
		},
		{
			name: "no colons, no placeholders is identity",
			in:   "SELECT 1",
			want: "SELECT 1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SQLAlchemyRewriteSQL(tc.in); got != tc.want {
				t.Fatalf("SQLAlchemyRewriteSQL(%q)\n got = %q\nwant = %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSQLAlchemyRowKwargsEmbedRunningIndex is the fast Go guard for the
// embed row-index advance — the regression the sqlalchemyRowKwargs docstring
// describes ("using the column's loop position as the row index would mis-scan
// every column AFTER an embed"). The fixture is the `SELECT col, embed, col2`
// shape the embed golden also exercises (book_id, sqlc.embed(author), title): a
// plain column BEFORE the embed, the embed's two inner columns, and a plain
// column AFTER it. Only the running-index advance (rowIdx += len(EmbedFields))
// puts the trailing `title` at row[3]; the buggy loop-position code would emit
// row[2] for it and row[1]/row[1] for the embed's inner columns. The
// embed-last golden alone could not catch this (the advance never changes
// output when no plain column follows the embed).
func TestSQLAlchemyRowKwargsEmbedRunningIndex(t *testing.T) {
	plain := func(name string) core.Column {
		return core.Column{Name: name, Type: core.PyType{Type: "int"}}
	}
	table := &core.Table{
		Columns: []core.Column{
			plain("book_id"),
			{
				Name: "author",
				Type: core.PyType{Type: "models.Author"},
				EmbedFields: []core.Column{
					plain("author_id"),
					plain("name"),
				},
			},
			plain("title"),
		},
	}
	want := []string{
		"book_id=row[0]",
		"author=models.Author(author_id=row[1], name=row[2])",
		"title=row[3]",
	}
	got := sqlalchemyRowKwargs(table)
	if len(got) != len(want) {
		t.Fatalf("sqlalchemyRowKwargs len = %d, want %d\n got = %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sqlalchemyRowKwargs[%d]\n got = %q\nwant = %q", i, got[i], want[i])
		}
	}
}

// TestSQLAlchemyRewriteHeaderVerb pins the second emission site: the verb
// colon on the `-- name: <fn> :<verb>` line escapes to `\\:<verb>`.
func TestSQLAlchemyRewriteHeaderVerb(t *testing.T) {
	cases := map[string]string{
		":one":      `\\:one`,
		":many":     `\\:many`,
		":exec":     `\\:exec`,
		":execrows": `\\:execrows`,
	}
	for in, want := range cases {
		if got := SQLAlchemyRewriteHeaderVerb(in); got != want {
			t.Fatalf("SQLAlchemyRewriteHeaderVerb(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSQLAlchemyAcceptedCMDsAllDispatch keeps the accepted-command list and the
// emitter switch in lockstep: every Cmd advertised by SQLAlchemyAcceptedDriverCMDs
// must be handled by SQLAlchemyBuildPyQueryFunc (none returns the unsupported-
// command error). Adding a Cmd to the list without a switch case — or vice
// versa — would otherwise drift silently behind the switch's fail-loud default.
func TestSQLAlchemyAcceptedCMDsAllDispatch(t *testing.T) {
	none := core.DocstringConventionNone
	emitSQL := false
	builders.SetDocstringConfig(&none, &emitSQL, core.SQLDriverSQLAlchemy)

	conf := &core.Config{
		SqlDriver:           core.SQLDriverSQLAlchemy,
		EmitClasses:         true,
		ModelType:           core.ModelTypePydantic,
		IndentChar:          " ",
		CharsPerIndentLevel: 4,
	}
	for _, cmd := range SQLAlchemyAcceptedDriverCMDs() {
		t.Run(cmd, func(t *testing.T) {
			query := &core.Query{
				FuncName:     "do_thing",
				MethodName:   "DoThing",
				Cmd:          cmd,
				ConstantName: "DO_THING",
				SQL:          "SELECT 1",
				Ret:          core.QueryValue{Typ: core.PyType{Type: "int"}},
			}
			body := builders.NewIndentStringBuilder(conf.IndentChar, conf.CharsPerIndentLevel)
			if err := SQLAlchemyBuildPyQueryFunc(query, body, nil, core.PyType{Type: "int"}, conf); err != nil {
				t.Fatalf("accepted command %q must dispatch, got error: %v", cmd, err)
			}
		})
	}
}
