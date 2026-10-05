package drivers

import (
	"fmt"
	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"regexp"
	"strconv"
	"strings"
)

// SQLAlchemyConn is the connection type the generated AsyncQuerier wraps. It is
// the single runtime dependency the driver leaks onto the public surface.
const SQLAlchemyConn = "sqlalchemy.ext.asyncio.AsyncConnection"

// postgresPlaceholderRegexp matches a `$N` positional placeholder, mirroring
// upstream sqlc-gen-python's sqlalchemySQL. `\B` before `$` and `\b` after the
// digits avoid touching `$N` glued to a word char.
var postgresPlaceholderRegexp = regexp.MustCompile(`\B\$(\d+)\b`)

// unbindablePlaceholderRegexp matches a `$N` the rewrite would touch that
// directly follows a backslash or a colon. The rewrite turns it into `\:pN`
// (SQLAlchemy reads an escaped literal `:pN`, so the SQL's backslash and the
// bind are both lost, with no error) or `\::pN` (no bind, and Postgres gets
// the backslash and `::pN`).
var unbindablePlaceholderRegexp = regexp.MustCompile(`[\\:]\$\d+\b`)

// SQLAlchemyRewriteSQL rewrites a query body for SQLAlchemy's text() binds,
// mirroring upstream sqlc-gen-python's sqlalchemySQL. It returns the RUN-TIME
// text; the emission site encodes it for the Python string
// (core.PyTripleQuotedText doubles each backslash, so `\:` is written `\\:`).
// ORDER IS LOAD-BEARING:
//
//  1. Escape EVERY colon `:` -> `\:` over the whole body (SQLAlchemy reads
//     `\:` as a literal colon). This is NOT cast-only — `::` casts become
//     `\:\:`, and a non-cast literal colon (`WHERE label = 'a:b'`) also
//     escapes to `'a\:b'`.
//  2. THEN replace `$N` -> `:pN`. Escaping first ensures the inserted `:pN`
//     bind markers are NOT themselves escaped.
//
// A backslash of the SQL itself stays as it is: SQLAlchemy unescapes only a
// backslash directly before a colon, and step 1 puts its own backslash there.
// The one exception, a `$N` directly after a backslash or a colon, stops
// generation (sqlalchemyCheckPlaceholders).
func SQLAlchemyRewriteSQL(s string) string {
	s = strings.ReplaceAll(s, ":", `\:`)
	return postgresPlaceholderRegexp.ReplaceAllString(s, ":p$1")
}

// SQLAlchemyRewriteHeaderVerb escapes the verb colon on the `-- name: <fn>
// :<verb>` header line (`:one` -> `\:one` at run time), a SEPARATE emission
// site from the body rewrite. SQLAlchemy parses the whole `text()` string for
// `:name` binds including the leading comment, so the verb colon must be
// escaped too.
func SQLAlchemyRewriteHeaderVerb(verb string) string {
	return strings.ReplaceAll(verb, ":", `\:`)
}

// sqlalchemyCheckPlaceholders stops generation for a `$N` that the rewrite can
// not turn into a working bind (unbindablePlaceholderRegexp).
func sqlalchemyCheckPlaceholders(query *core.Query) error {
	if match := unbindablePlaceholderRegexp.FindString(query.SQL); match != "" {
		return fmt.Errorf("query %s: %q: sqlalchemy.text() can not bind a $N placeholder directly after a backslash or a colon; put a space before the placeholder, and keep $<digits> out of string literals", query.MethodName, match)
	}
	return nil
}

// sqlalchemyStreamMarker is the per-query opt-out from the buffered :many
// SELECT. sqlc passes every full-line `--` comment of a query to the plugin in
// Query.Comments (without the `--`) and strips it from the SQL text. A comment
// whose first word is the marker keeps that query on a server-side cursor
// (conn.stream), for a result too large to buffer in memory. The reason goes
// after the marker, `-- @stream <reason>`, and is required: it is the only
// record of why the query keeps the cursor.
const sqlalchemyStreamMarker = "@stream"

// sqlalchemyKeepsStream reports whether the query carries the marker, and
// fails when the marker has no reason after it.
func sqlalchemyKeepsStream(query *core.Query) (bool, error) {
	for _, comment := range query.Comments {
		if words := strings.Fields(comment); len(words) > 0 && words[0] == sqlalchemyStreamMarker {
			if len(words) == 1 {
				return false, fmt.Errorf("query %s: the %s marker needs a reason: -- %s <why this result is too large to buffer>", query.MethodName, sqlalchemyStreamMarker, sqlalchemyStreamMarker)
			}
			return true, nil
		}
	}
	return false, nil
}

// SQLAlchemyBuildClassTemplate is the querier-class override. It differs from
// defaultBuildClassTemplate on five points required by the SQLAlchemy surface:
//
//  1. a stable literal `AsyncQuerier` class name (NOT SnakeToCamel(sourceName));
//  2. no `__slots__`;
//  3. `def __init__(self, conn: ...AsyncConnection):` with NO `-> None` return
//     annotation and no `@property`/`conn` accessor;
//  4. a plain `self._conn = conn` attribute;
//  5. NO class/init/conn docstrings, regardless of the `docstrings` knob — the
//     SQLAlchemy class body stays docstring-free even when a convention is
//     configured.
func SQLAlchemyBuildClassTemplate(_ string, connType string, _ *core.Config, body *builders.IndentStringBuilder) string {
	className := "AsyncQuerier"
	body.WriteLine(fmt.Sprintf("class %s:", className))
	body.WriteIndentedLine(1, fmt.Sprintf("def __init__(self, conn: %s):", connType))
	body.WriteIndentedLine(2, "self._conn = conn")
	body.NewLine()
	return className
}

// SQLAlchemyBuildPyQueryFunc emits the verb method bodies. :one/:exec/:execrows
// land in this batch; :many is deferred (commit #12). The EmitClasses gate is
// honoured exactly as asyncpg.go:56 — without it the method is a flat
// module-level function taking `conn` explicitly; with it the method is a
// keyword-only method on AsyncQuerier reading self._conn.
func SQLAlchemyBuildPyQueryFunc(query *core.Query, body *builders.IndentStringBuilder, args []core.FunctionArg, retType core.PyType, conf *core.Config) error {
	indentLevel := 0
	params := fmt.Sprintf("conn: %s", SQLAlchemyConn)
	conn := "conn"
	docstringConnType := SQLAlchemyConn
	if conf.EmitClasses {
		params = "self"
		conn = "self._conn"
		indentLevel = 1
		docstringConnType = ""
	}

	if err := sqlalchemyCheckPlaceholders(query); err != nil {
		return err
	}
	stream, err := sqlalchemyKeepsStream(query)
	if err != nil {
		return err
	}
	if stream && (query.Cmd != metadata.CmdMany || core.SQLRootIsDML(query.SQL)) {
		return fmt.Errorf("query %s: the %s marker applies only to a :many SELECT", query.MethodName, sqlalchemyStreamMarker)
	}

	switch query.Cmd {
	case metadata.CmdExec:
		body.WriteIndentedString(indentLevel, fmt.Sprintf("async def %s(%s", query.FuncName, params))
		sqlalchemyWriteFunctionArgs(query, body, args, conf)
		body.WriteLine(fmt.Sprintf(") -> %s:", retType.Type))
		body.WriteQueryFunctionDocstring(indentLevel+1, query, docstringConnType, args, retType)
		body.WriteIndentedString(indentLevel+1, fmt.Sprintf("await %s.execute(sqlalchemy.text(%s)", conn, query.ConstantName))
		sqlalchemyWriteParams(query, body, indentLevel+1)
		body.WriteLine(")")
	case metadata.CmdExecRows:
		body.WriteIndentedString(indentLevel, fmt.Sprintf("async def %s(%s", query.FuncName, params))
		sqlalchemyWriteFunctionArgs(query, body, args, conf)
		body.WriteLine(fmt.Sprintf(") -> %s:", retType.Type))
		body.WriteQueryFunctionDocstring(indentLevel+1, query, docstringConnType, args, retType)
		body.WriteIndentedString(indentLevel+1, fmt.Sprintf("result = await %s.execute(sqlalchemy.text(%s)", conn, query.ConstantName))
		sqlalchemyWriteParams(query, body, indentLevel+1)
		body.WriteLine(")")
		body.WriteIndentedLine(indentLevel+1, "return result.rowcount")
	case metadata.CmdOne:
		body.WriteIndentedString(indentLevel, fmt.Sprintf("async def %s(%s", query.FuncName, params))
		sqlalchemyWriteFunctionArgs(query, body, args, conf)
		body.WriteLine(fmt.Sprintf(") -> %s | None:", retType.Type))
		body.WriteQueryFunctionDocstring(indentLevel+1, query, docstringConnType, args, retType)
		body.WriteIndentedString(indentLevel+1, fmt.Sprintf("row = (await %s.execute(sqlalchemy.text(%s)", conn, query.ConstantName))
		sqlalchemyWriteParams(query, body, indentLevel+1)
		body.WriteLine(")).first()")
		body.WriteIndentedLine(indentLevel+1, "if row is None:")
		body.WriteIndentedLine(indentLevel+2, "return None")
		body.WriteIndentedLine(indentLevel+1, fmt.Sprintf("return %s(row)", SQLAlchemyRowConstructorName(query)))
	case metadata.CmdMany:
		if core.SQLRootIsDML(query.SQL) {
			// A :many over INSERT/UPDATE/DELETE ... RETURNING. Postgres
			// rejects a server-side cursor (conn.stream) for a DML statement,
			// so materialize the rows eagerly:
			// `result = await conn.execute(...); return [<func>_row(row) for
			// row in result.all()]` returning list[T]. NOT conn.stream.
			body.WriteIndentedString(indentLevel, fmt.Sprintf("async def %s(%s", query.FuncName, params))
			sqlalchemyWriteFunctionArgs(query, body, args, conf)
			body.WriteLine(fmt.Sprintf(") -> list[%s]:", retType.Type))
			body.WriteQueryFunctionDocstring(indentLevel+1, query, docstringConnType, args, retType)
			body.WriteIndentedString(indentLevel+1, fmt.Sprintf("result = await %s.execute(sqlalchemy.text(%s)", conn, query.ConstantName))
			sqlalchemyWriteParams(query, body, indentLevel+1)
			body.WriteLine(")")
			body.WriteIndentedLine(indentLevel+1, fmt.Sprintf("return [%s(row) for row in result.all()]", SQLAlchemyRowConstructorName(query)))
			break
		}
		// The SQLAlchemy :many SELECT is a NATIVE async generator
		// (collections.abc.AsyncIterator[T]), NOT a QueryResults wrapper
		// (driverBuildQueryResults stays the no-op default). It buffers by
		// default: `result = await conn.execute(...)` + `for row in result:
		// yield ...` is one round trip, where conn.stream opens an asyncpg
		// portal (not SQL DECLARE) and pays a fetch per batch. A query marked
		// `-- @stream` keeps conn.stream + `async for`.
		fetch, loop := "execute", "for row in result:"
		if stream {
			fetch, loop = "stream", "async for row in result:"
		}
		body.WriteIndentedString(indentLevel, fmt.Sprintf("async def %s(%s", query.FuncName, params))
		sqlalchemyWriteFunctionArgs(query, body, args, conf)
		body.WriteLine(fmt.Sprintf(") -> collections.abc.AsyncIterator[%s]:", retType.Type))
		body.WriteQueryFunctionDocstring(indentLevel+1, query, docstringConnType, args, retType)
		body.WriteIndentedString(indentLevel+1, fmt.Sprintf("result = await %s.%s(sqlalchemy.text(%s)", conn, fetch, query.ConstantName))
		sqlalchemyWriteParams(query, body, indentLevel+1)
		body.WriteLine(")")
		body.WriteIndentedLine(indentLevel+1, loop)
		body.WriteIndentedLine(indentLevel+2, fmt.Sprintf("yield %s(row)", SQLAlchemyRowConstructorName(query)))
	default:
		return fmt.Errorf("unsupported command for sqlalchemy driver in this batch: %s", query.Cmd)
	}
	return nil
}

// sqlalchemyValidateExpr builds the fail-loud validated read expression for a
// `validate: true` override: `<adapter>.validate_python(<accessor>)`. When the
// column is nullable it is guarded with `if <accessor> is not None else None` so
// a SQL NULL returns None instead of raising ValidationError against a
// non-Optional model (mirrors the struct-field nullable branch in
// sqlalchemyColKwarg).
func sqlalchemyValidateExpr(adapterVar, accessor string, nullable bool) string {
	if nullable {
		return fmt.Sprintf("%s.validate_python(%s) if %s is not None else None", adapterVar, accessor, accessor)
	}
	return fmt.Sprintf("%s.validate_python(%s)", adapterVar, accessor)
}

// sqlalchemyRowKwargs builds the ordered `field=row[N]` keyword list for a
// result-Row constructor, tracking a SINGLE running row index across all
// columns. A plain column consumes one row position; a sqlc.embed column
// expands into a nested `field=models.X(inner=row[N], ...)` over its embedded
// columns, advancing the running index by the count of embedded columns (NOT 1)
// — matching the asyncpg driver's embed scan. Using the column's loop position
// as the row index would mis-scan every column after an embed.
func sqlalchemyRowKwargs(table *core.Table) []string {
	kwargs := make([]string, 0, len(table.Columns))
	rowIdx := 0
	for _, col := range table.Columns {
		if len(col.EmbedFields) != 0 {
			name, _ := core.EscapeFieldName(col.Name)
			inner := make([]string, 0, len(col.EmbedFields))
			for _, embedCol := range col.EmbedFields {
				inner = append(inner, sqlalchemyColKwarg(embedCol, rowIdx))
				rowIdx++
			}
			kwargs = append(kwargs, fmt.Sprintf("%s=%s(%s)", name, col.Type.Type, strings.Join(inner, ", ")))
			continue
		}
		kwargs = append(kwargs, sqlalchemyColKwarg(col, rowIdx))
		rowIdx++
	}
	return kwargs
}

// sqlalchemyColKwarg builds one `field=row[N]` (or override-wrapped) keyword
// for a model/row constructor, at row index `i`. The field name is escaped when
// it is a true Python keyword (`class` -> `class_`), matching the pydantic
// model's escaped field name (constructed positionally by name, which
// populate_by_name accepts). No trailing comma — the caller adds it.
func sqlalchemyColKwarg(col core.Column, i int) string {
	idx := strconv.Itoa(i)
	name, _ := core.EscapeFieldName(col.Name)
	if col.Type.DoValidate() {
		return fmt.Sprintf("%s=%s", name, sqlalchemyValidateExpr(core.ValidateAdapterVar(col.Type.Type), fmt.Sprintf("row[%s]", idx), col.Type.IsNullable))
	}
	if col.Type.DoOverride() {
		if col.Type.IsNullable {
			return fmt.Sprintf("%s=%s(row[%s]) if row[%s] is not None else None", name, col.Type.Type, idx, idx)
		}
		return fmt.Sprintf("%s=%s(row[%s])", name, col.Type.Type, idx)
	}
	return fmt.Sprintf("%s=row[%s]", name, idx)
}

// SQLAlchemyRowConstructorName is the module-level per-row constructor of a
// row-returning query: `<func>_row`. The querier's :one, :many and :many-over-DML
// bodies call it, and so does any consumer that builds the same row from a
// result it fetched itself (arche's chat dispatcher streams the query constant).
func SQLAlchemyRowConstructorName(query *core.Query) string {
	return query.FuncName + "_row"
}

// SQLAlchemyHasRowConstructor reports whether the query returns rows, and so
// gets a row constructor.
func SQLAlchemyHasRowConstructor(query *core.Query) bool {
	return query.Cmd == metadata.CmdOne || query.Cmd == metadata.CmdMany
}

// SQLAlchemyCheckRowConstructorNames stops generation when a constructor name
// is also a query function name. That can only happen without emit_classes,
// where the query functions live at module level next to the constructors.
func SQLAlchemyCheckRowConstructorNames(queries []core.Query, conf *core.Config) error {
	if conf.EmitClasses {
		return nil
	}
	funcs := make(map[string]bool, len(queries))
	for _, query := range queries {
		funcs[query.FuncName] = true
	}
	for i := range queries {
		if name := SQLAlchemyRowConstructorName(&queries[i]); SQLAlchemyHasRowConstructor(&queries[i]) && funcs[name] {
			return fmt.Errorf("query %s: its row constructor %s has the name of another query function", queries[i].MethodName, name)
		}
	}
	return nil
}

// SQLAlchemyBuildRowConstructor writes the module-level
// `def <func>_row(row: sqlalchemy.Row[typing.Any]) -> T:`, the one place that
// builds a result row: either a bare `return row[0]` for a scalar result or a
// `return models.X(field=row[N], ...)` for a struct, by running row index
// (handling sqlc.embed expansion via sqlalchemyRowKwargs). The indexes are
// positional from 0, so a row with extra trailing columns (a count appended by
// a wrapping query) builds the same value.
func SQLAlchemyBuildRowConstructor(query *core.Query, body *builders.IndentStringBuilder, retType core.PyType) {
	// A nullable validated scalar returns None for a SQL NULL, so its
	// constructor says so. Every other scalar reads row[0] unwrapped (typed
	// Any), and its constructor keeps the querier's element type.
	annotation := retType.Type
	if !query.Ret.IsStruct() && query.Ret.Typ.DoValidate() && query.Ret.Typ.IsNullable {
		annotation += " | None"
	}
	body.WriteLine(fmt.Sprintf("def %s(row: sqlalchemy.Row[typing.Any]) -> %s:", SQLAlchemyRowConstructorName(query), annotation))
	if !query.Ret.IsStruct() {
		// The validate decision reads the FULL result type (query.Ret.Typ carries
		// the override + Validate); `retType` here is the stripped header type
		// (name only), so retType.DoOverride() stays false and the cast branch is
		// inert — preserving the SQLAlchemy read-unwrapped (#161) byte shape.
		switch {
		case query.Ret.Typ.DoValidate():
			body.WriteIndentedLine(1, "return "+sqlalchemyValidateExpr(core.ValidateAdapterVar(retType.Type), "row[0]", query.Ret.Typ.IsNullable))
		case retType.DoOverride():
			body.WriteIndentedLine(1, fmt.Sprintf("return %s(row[0])", retType.Type))
		default:
			body.WriteIndentedLine(1, "return row[0]")
		}
		return
	}
	body.WriteIndentedLine(1, fmt.Sprintf("return %s(", retType.Type))
	for _, kw := range sqlalchemyRowKwargs(query.Ret.Table) {
		body.WriteIndentedLine(2, kw+",")
	}
	body.WriteIndentedLine(1, ")")
}

// sqlalchemyWriteParams emits the named-bind dict `{"pN": value, ...}` the
// SQLAlchemy `text()` API consumes. A zero-param query emits no dict at all
// (`text(C)`), mirroring asyncpg's asyncpgWriteParams early-return.
//
// `stmtIndent` is the indent level of the call statement the dict is appended
// to (the level passed to the preceding WriteIndentedString) — it positions the
// BUNDLE dict's multi-line layout.
//
// Two arg shapes, emitted with DIFFERENT layouts:
//   - FLAT keyword-only args → SINGLE-LINE `, {"p1": a, "p2": b}` where N is the
//     real SQL placeholder Number ($N), NOT the slice index — an out-of-order or
//     reused placeholder still maps correctly.
//   - BUNDLE (a single positional struct arg `arg: <Method>Params`) → MULTI-LINE:
//     `, {` on the call line, one `"pN": arg.field,` per key at stmtIndent+1, a
//     closing `}` at stmtIndent (no trailing newline — the caller appends the
//     rest of the call, e.g. `)).first()`).
//
// An overridden column binds its value DIRECTLY — there is NO `DefaultType(value)`
// constructor wrap (asyncpg keeps that cast; SQLAlchemy does not). The SQLAlchemy
// named-bind path lets the dialect adapt the Python object to the column type, so
// the base-type constructor is both unnecessary and — for the jsonb default
// `typing.Any` (better-python #161) — invalid Python at runtime (`typing.Any`
// is not callable). Binding the value as-is is symmetric with the READ path,
// which returns an override type unwrapped (`return row[0]`).
func sqlalchemyWriteParams(query *core.Query, body *builders.IndentStringBuilder, stmtIndent int) {
	if len(query.Args) == 1 && query.Args[0].IsStruct() {
		arg := query.Args[0]
		pairs := make([]string, 0, len(arg.Table.Columns))
		for _, col := range arg.Table.Columns {
			// Read the Params struct field by its emitted (keyword-escaped)
			// name (e.g. `arg.class_`), matching the pydantic Params field.
			fieldName, _ := core.EscapeFieldName(col.Name)
			pairs = append(pairs, fmt.Sprintf(`"p%d": %s.%s`, col.Number, arg.Name, fieldName))
		}
		if len(pairs) == 0 {
			return
		}
		body.WriteString(", {\n")
		for _, pair := range pairs {
			body.WriteIndentedLine(stmtIndent+1, pair+",")
		}
		body.WriteIndentedString(stmtIndent, "}")
		return
	}
	pairs := make([]string, 0, len(query.Args))
	for _, arg := range query.Args {
		if arg.IsEmpty() {
			continue
		}
		pairs = append(pairs, fmt.Sprintf(`"p%d": %s`, arg.Number, arg.Name))
	}
	if len(pairs) == 0 {
		return
	}
	body.WriteString(", {" + strings.Join(pairs, ", ") + "}")
}

// sqlalchemyWriteFunctionArgs writes the method's argument list. A BUNDLE (a
// single positional struct arg) is written plain — `, arg: <Method>Params` with
// NO leading `*`. FLAT args delegate to the shared WriteQueryFunctionArgs,
// which prepends `, *` for the keyword-only `(self, *, a, b)` form below the
// bundle limit. The fork base emits `*` even for the single copyfrom `params`
// arg; the SQLAlchemy bundle is positional, so this driver owns the divergence.
func sqlalchemyWriteFunctionArgs(query *core.Query, body *builders.IndentStringBuilder, args []core.FunctionArg, conf *core.Config) {
	if len(query.Args) == 1 && query.Args[0].IsStruct() && len(args) == 1 {
		body.WriteString(fmt.Sprintf(", %s", args[0].FunctionFormat))
		return
	}
	body.WriteQueryFunctionArgs(args, conf)
}

func SQLAlchemyAcceptedDriverCMDs() []string {
	return []string{
		metadata.CmdExec,
		metadata.CmdExecRows,
		metadata.CmdOne,
		metadata.CmdMany,
	}
}
