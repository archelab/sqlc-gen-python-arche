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

// SQLAlchemyRewriteSQL rewrites a query body for SQLAlchemy's text() binds,
// mirroring upstream sqlc-gen-python's sqlalchemySQL. ORDER IS LOAD-BEARING:
//
//  1. Escape EVERY colon `:` -> `\\:` over the whole body (two backslash
//     characters + colon in the emitted Python text, so the runtime SQL string
//     carries `\:` which SQLAlchemy reads as a literal colon). This is NOT
//     cast-only — `::` casts become `\\:\\:`, and a non-cast literal colon
//     (`WHERE label = 'a:b'`) also escapes to `'a\\:b'`.
//  2. THEN replace `$N` -> `:pN`. Escaping first ensures the inserted `:pN`
//     bind markers are NOT themselves escaped.
func SQLAlchemyRewriteSQL(s string) string {
	s = strings.ReplaceAll(s, ":", `\\:`)
	return postgresPlaceholderRegexp.ReplaceAllString(s, ":p$1")
}

// SQLAlchemyRewriteHeaderVerb escapes the verb colon on the `-- name: <fn>
// :<verb>` header line (`:one` -> `\\:one`), a SEPARATE emission site from the
// body rewrite. SQLAlchemy parses the whole `text()` string for `:name` binds
// including the leading comment, so the verb colon must be escaped too.
func SQLAlchemyRewriteHeaderVerb(verb string) string {
	return strings.ReplaceAll(verb, ":", `\\:`)
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
		sqlalchemyWriteRowConstruction(query, body, retType, indentLevel)
	case metadata.CmdMany:
		if core.SQLRootIsDML(query.SQL) {
			// A :many over INSERT/UPDATE/DELETE ... RETURNING. Postgres
			// rejects a server-side cursor (conn.stream) for a DML statement,
			// so materialize the rows eagerly:
			// `result = await conn.execute(...); return [Row(..) for row in
			// result.all()]` returning list[T]. NOT conn.stream.
			body.WriteIndentedString(indentLevel, fmt.Sprintf("async def %s(%s", query.FuncName, params))
			sqlalchemyWriteFunctionArgs(query, body, args, conf)
			body.WriteLine(fmt.Sprintf(") -> list[%s]:", retType.Type))
			body.WriteQueryFunctionDocstring(indentLevel+1, query, docstringConnType, args, retType)
			body.WriteIndentedString(indentLevel+1, fmt.Sprintf("result = await %s.execute(sqlalchemy.text(%s)", conn, query.ConstantName))
			sqlalchemyWriteParams(query, body, indentLevel+1)
			body.WriteLine(")")
			sqlalchemyWriteListComprehension(query, body, retType, indentLevel)
			break
		}
		// The SQLAlchemy :many SELECT is a NATIVE async generator
		// (collections.abc.AsyncIterator[T]) backed by conn.stream(...) +
		// `async for row in result: yield ...`, NOT a QueryResults wrapper
		// (driverBuildQueryResults stays the no-op default).
		body.WriteIndentedString(indentLevel, fmt.Sprintf("async def %s(%s", query.FuncName, params))
		sqlalchemyWriteFunctionArgs(query, body, args, conf)
		body.WriteLine(fmt.Sprintf(") -> collections.abc.AsyncIterator[%s]:", retType.Type))
		body.WriteQueryFunctionDocstring(indentLevel+1, query, docstringConnType, args, retType)
		body.WriteIndentedString(indentLevel+1, fmt.Sprintf("result = await %s.stream(sqlalchemy.text(%s)", conn, query.ConstantName))
		sqlalchemyWriteParams(query, body, indentLevel+1)
		body.WriteLine(")")
		body.WriteIndentedLine(indentLevel+1, "async for row in result:")
		sqlalchemyWriteYieldRowConstruction(query, body, retType, indentLevel)
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

// sqlalchemyWriteYieldRowConstruction writes the :many `async for row in
// result:` body: either `yield row[0]` for a scalar result or `yield
// models.X(field=row[N], ...)` for a struct, by integer index. The yield sits
// one indent deeper than the :one `return` (under the `async for`).
func sqlalchemyWriteYieldRowConstruction(query *core.Query, body *builders.IndentStringBuilder, retType core.PyType, indentLevel int) {
	if !query.Ret.IsStruct() {
		switch {
		case query.Ret.Typ.DoValidate():
			body.WriteIndentedLine(indentLevel+2, "yield "+sqlalchemyValidateExpr(core.ValidateAdapterVar(retType.Type), "row[0]", query.Ret.Typ.IsNullable))
		case retType.DoOverride():
			body.WriteIndentedLine(indentLevel+2, fmt.Sprintf("yield %s(row[0])", retType.Type))
		default:
			body.WriteIndentedLine(indentLevel+2, "yield row[0]")
		}
		return
	}
	body.WriteIndentedLine(indentLevel+2, fmt.Sprintf("yield %s(", retType.Type))
	for _, kw := range sqlalchemyRowKwargs(query.Ret.Table) {
		body.WriteIndentedLine(indentLevel+3, kw+",")
	}
	body.WriteIndentedLine(indentLevel+2, ")")
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

// sqlalchemyWriteRowConstruction writes the `:one` return statement: either a
// bare `return row[0]` for a scalar result or a `return models.X(field=row[N],
// ...)` for a struct, by running row index (handling sqlc.embed expansion via
// sqlalchemyRowKwargs).
func sqlalchemyWriteRowConstruction(query *core.Query, body *builders.IndentStringBuilder, retType core.PyType, indentLevel int) {
	if !query.Ret.IsStruct() {
		// The validate decision reads the FULL result type (query.Ret.Typ carries
		// the override + Validate); `retType` here is the stripped header type
		// (name only), so retType.DoOverride() stays false and the cast branch is
		// inert — preserving the SQLAlchemy read-unwrapped (#161) byte shape.
		switch {
		case query.Ret.Typ.DoValidate():
			body.WriteIndentedLine(indentLevel+1, "return "+sqlalchemyValidateExpr(core.ValidateAdapterVar(retType.Type), "row[0]", query.Ret.Typ.IsNullable))
		case retType.DoOverride():
			body.WriteIndentedLine(indentLevel+1, fmt.Sprintf("return %s(row[0])", retType.Type))
		default:
			body.WriteIndentedLine(indentLevel+1, "return row[0]")
		}
		return
	}
	body.WriteIndentedLine(indentLevel+1, fmt.Sprintf("return %s(", retType.Type))
	for _, kw := range sqlalchemyRowKwargs(query.Ret.Table) {
		body.WriteIndentedLine(indentLevel+2, kw+",")
	}
	body.WriteIndentedLine(indentLevel+1, ")")
}

// sqlalchemyWriteListComprehension writes the :many-over-DML return: a
// list comprehension over result.all(). A scalar result is a one-liner
// `return [row[0] for row in result.all()]`; a struct result is a multiline
// comprehension constructing `models.X(field=row[N], ...)` per row (handling
// sqlc.embed expansion via sqlalchemyRowKwargs).
func sqlalchemyWriteListComprehension(query *core.Query, body *builders.IndentStringBuilder, retType core.PyType, indentLevel int) {
	if !query.Ret.IsStruct() {
		switch {
		case query.Ret.Typ.DoValidate():
			body.WriteIndentedLine(indentLevel+1, fmt.Sprintf("return [%s for row in result.all()]", sqlalchemyValidateExpr(core.ValidateAdapterVar(retType.Type), "row[0]", query.Ret.Typ.IsNullable)))
		case retType.DoOverride():
			body.WriteIndentedLine(indentLevel+1, fmt.Sprintf("return [%s(row[0]) for row in result.all()]", retType.Type))
		default:
			body.WriteIndentedLine(indentLevel+1, "return [row[0] for row in result.all()]")
		}
		return
	}
	body.WriteIndentedLine(indentLevel+1, "return [")
	body.WriteIndentedLine(indentLevel+2, fmt.Sprintf("%s(", retType.Type))
	for _, kw := range sqlalchemyRowKwargs(query.Ret.Table) {
		body.WriteIndentedLine(indentLevel+3, kw+",")
	}
	body.WriteIndentedLine(indentLevel+2, ")")
	body.WriteIndentedLine(indentLevel+2, "for row in result.all()")
	body.WriteIndentedLine(indentLevel+1, "]")
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
