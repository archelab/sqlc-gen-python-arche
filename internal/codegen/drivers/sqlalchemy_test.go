package drivers

import (
	"strings"
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/metadata"
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

// rowConstructorQuery is a struct-returning query whose result carries a
// validated jsonb column and a nullable override column, the two shapes that
// wrap `row[N]` in more than a plain keyword.
func rowConstructorQuery(cmd, sql string, comments []string) *core.Query {
	validated := core.Column{Name: "config", Type: core.PyType{
		Type:       "WidgetConfig",
		IsOverride: true,
		Override:   &core.Override{Validate: true},
	}}
	token := core.Column{Name: "lease", Type: core.PyType{
		Type:       "LeaseToken",
		IsNullable: true,
		IsOverride: true,
		Override:   &core.Override{},
	}}
	return &core.Query{
		FuncName:     "get_widget",
		MethodName:   "GetWidget",
		Cmd:          cmd,
		Comments:     comments,
		ConstantName: "GET_WIDGET",
		SQL:          sql,
		Ret: core.QueryValue{Table: &core.Table{
			Name:    "GetWidgetRow",
			Columns: []core.Column{{Name: "widget_id", Type: core.PyType{Type: "int"}}, validated, token},
		}},
	}
}

// TestSQLAlchemyRowConstructorIsTheOneRowBuilder pins the single per-row
// builder: every row-returning method shape (:one, buffered :many, `-- @stream`
// :many, :many over DML) calls the module-level `<func>_row(row)` constructor
// and builds no row itself, and the constructor holds the whole construction.
// A consumer that must build the same row outside the querier (a dispatcher
// that streams the query constant) calls the constructor, so the two can not
// drift.
func TestSQLAlchemyRowConstructorIsTheOneRowBuilder(t *testing.T) {
	none := core.DocstringConventionNone
	emitSQL := false
	builders.SetDocstringConfig(&none, &emitSQL, core.SQLDriverSQLAlchemy)
	conf := &core.Config{SqlDriver: core.SQLDriverSQLAlchemy, EmitClasses: true, ModelType: core.ModelTypePydantic, IndentChar: " ", CharsPerIndentLevel: 4}
	retType := core.PyType{Type: "GetWidgetRow"}

	shapes := []struct {
		name     string
		query    *core.Query
		wantCall string
	}{
		{"one", rowConstructorQuery(metadata.CmdOne, "SELECT 1", nil), "        return get_widget_row(row)\n"},
		{"many select", rowConstructorQuery(metadata.CmdMany, "SELECT 1", nil), "            yield get_widget_row(row)\n"},
		{"many stream", rowConstructorQuery(metadata.CmdMany, "SELECT 1", []string{" @stream too large"}), "            yield get_widget_row(row)\n"},
		{"many dml", rowConstructorQuery(metadata.CmdMany, "DELETE FROM widget RETURNING *", nil), "        return [get_widget_row(row) for row in result.all()]\n"},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			body := builders.NewIndentStringBuilder(conf.IndentChar, conf.CharsPerIndentLevel)
			if err := SQLAlchemyBuildPyQueryFunc(shape.query, body, nil, retType, conf); err != nil {
				t.Fatal(err)
			}
			method := body.String()
			if !strings.Contains(method, shape.wantCall) {
				t.Fatalf("method does not call the row constructor with %q:\n%s", shape.wantCall, method)
			}
			if strings.Contains(method, "row[") || strings.Contains(method, "GetWidgetRow(") {
				t.Fatalf("method builds a row itself instead of calling the constructor:\n%s", method)
			}
		})
	}

	body := builders.NewIndentStringBuilder(conf.IndentChar, conf.CharsPerIndentLevel)
	SQLAlchemyBuildRowConstructor(shapes[0].query, body, retType)
	want := "def get_widget_row(row: sqlalchemy.Row[typing.Any]) -> GetWidgetRow:\n" +
		"    return GetWidgetRow(\n" +
		"        widget_id=row[0],\n" +
		"        config=_WidgetConfig_adapter.validate_python(row[1]),\n" +
		"        lease=LeaseToken(row[2]) if row[2] is not None else None,\n" +
		"    )\n"
	if got := body.String(); got != want {
		t.Fatalf("row constructor\n got = %q\nwant = %q", got, want)
	}
}

// TestSQLAlchemyRowConstructorScalar pins the scalar constructor: a validated
// scalar reads through its adapter, a plain scalar returns row[0].
func TestSQLAlchemyRowConstructorScalar(t *testing.T) {
	validated := &core.Query{FuncName: "get_config", Cmd: metadata.CmdOne, Ret: core.QueryValue{Typ: core.PyType{
		Type:       "WidgetConfig",
		IsNullable: true,
		IsOverride: true,
		Override:   &core.Override{Validate: true},
	}}}
	plain := &core.Query{FuncName: "list_ids", Cmd: metadata.CmdMany, Ret: core.QueryValue{Typ: core.PyType{Type: "int"}}}
	cases := []struct {
		query   *core.Query
		retType core.PyType
		want    string
	}{
		{validated, core.PyType{Type: "WidgetConfig"}, "def get_config_row(row: sqlalchemy.Row[typing.Any]) -> WidgetConfig | None:\n    return _WidgetConfig_adapter.validate_python(row[0]) if row[0] is not None else None\n"},
		{plain, core.PyType{Type: "int"}, "def list_ids_row(row: sqlalchemy.Row[typing.Any]) -> int:\n    return row[0]\n"},
	}
	for _, tc := range cases {
		body := builders.NewIndentStringBuilder(" ", 4)
		SQLAlchemyBuildRowConstructor(tc.query, body, tc.retType)
		if got := body.String(); got != tc.want {
			t.Fatalf("row constructor\n got = %q\nwant = %q", got, tc.want)
		}
	}
}

// TestSQLAlchemyRowConstructorNameCollision: without emit_classes the query
// functions are module-level, so a query named `<Other>Row` would shadow the
// constructor of `<Other>`. Generation stops instead.
func TestSQLAlchemyRowConstructorNameCollision(t *testing.T) {
	queries := []core.Query{
		{FuncName: "get_widget", Cmd: metadata.CmdOne},
		{FuncName: "get_widget_row", Cmd: metadata.CmdExec},
	}
	if err := SQLAlchemyCheckRowConstructorNames(queries, &core.Config{EmitClasses: false}); err == nil || !strings.Contains(err.Error(), "get_widget_row") {
		t.Fatalf("want a collision error naming get_widget_row, got %v", err)
	}
	if err := SQLAlchemyCheckRowConstructorNames(queries, &core.Config{EmitClasses: true}); err != nil {
		t.Fatalf("methods on AsyncQuerier can not collide with module functions, got %v", err)
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
