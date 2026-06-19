package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

func (dr *Driver) BuildPyTablesFile(imp *core.Importer, tables []core.Table) (*plugin.File, error) {
	fileName, fileContent, err := dr.buildPyTables(imp, tables)
	if err != nil {
		return nil, err
	}
	return &plugin.File{
		Name:     core.SQLToPyFileName(fileName),
		Contents: fileContent,
	}, nil
}

// BuildPyTabel emits a model/struct class. stripModelsPrefix is true ONLY when
// emitting into models.py itself, where a same-file enum reference must be bare
// `OrderStatus` rather than the cross-file `models.OrderStatus` used in query
// files.
func BuildPyTabel(modelType core.ModelType, table *core.Table, body *builders.IndentStringBuilder, stripModelsPrefix bool) {
	inheritance := ""
	switch modelType {
	case core.ModelTypePydantic:
		buildPydanticTable(table, body, stripModelsPrefix)
		return
	case core.ModelTypeDataclass:
		body.WriteLine("@dataclasses.dataclass()")
	case core.ModelTypeAttrs:
		body.WriteLine("@attrs.define()")
	case core.ModelTypeMsgspec:
		inheritance = "(msgspec.Struct)"
	default:
		// ValidateConf gates ModelType upstream, so this is unreachable; panic
		// rather than silently emit a bare `class X:` with no decorator/base.
		panic(fmt.Sprintf("BuildPyTabel: unhandled model type %q", modelType))
	}
	body.WriteLine(fmt.Sprintf("class %s%s:", table.Name, inheritance))
	body.WriteModelClassDocstring(table)
	for _, col := range table.Columns {
		type_ := pyFieldType(col, stripModelsPrefix)
		body.WriteIndentedLine(1, col.Name+": "+type_)
	}
}

// allDunderAnnotation returns the type annotation for the module-level `__all__`
// assignment. `__all__` is emitted BEFORE the import block (the better-python
// base layout), and a module-level annotated assignment is evaluated at runtime
// unless `from __future__ import annotations` is active. The dataclass/attrs/
// msgspec drivers emit future-annotations, so `collections.abc.Sequence[str]`
// stays a deferred string there. pydantic suppresses future-annotations (see
// BuildPyTablesFile / buildPyQueriesFile), so the annotation is evaluated
// immediately — referencing `collections.abc` before it is imported would raise
// `NameError`. The builtin `tuple[str, ...]` needs no import and is the standard
// idiom for typing `__all__`, so pydantic uses it.
func allDunderAnnotation(modelType core.ModelType) string {
	// When future-annotations is suppressed (the runtime-annotation model types),
	// this module-level annotation is evaluated immediately, so it must avoid an
	// unimported name: use the builtin `tuple[str, ...]` rather than
	// `collections.abc.Sequence[str]`.
	if modelType.RuntimeFieldAnnotations() {
		return "tuple[str, ...]"
	}
	return "collections.abc.Sequence[str]"
}

// pyFieldType renders a column's full annotation (list/optional wrapping),
// stripping a leading `models.` when the field is emitted inside models.py.
func pyFieldType(col core.Column, stripModelsPrefix bool) string {
	type_ := col.Type.Type
	if stripModelsPrefix {
		type_ = strings.TrimPrefix(type_, "models.")
	}
	if col.Type.IsList {
		if col.Type.ListIsBuiltin {
			type_ = "list[" + type_ + "]"
		} else {
			type_ = "collections.abc.Sequence[" + type_ + "]"
		}
	}
	if col.Type.IsNullable {
		type_ = type_ + " | None"
	}
	return type_
}

// buildPydanticTable emits a `pydantic.BaseModel` subclass modelled on
// upstream sqlc-gen-python 1.3.0: the table comment becomes the class docstring
// (raw, unconditional — NOT the convention-keyed
// WriteModelClassDocstring used by the dataclass/attrs/msgspec path), each
// column comment becomes a `#` field comment, and field names are raw
// snake_case with NO `Field(alias=...)`/`model_config`/`populate_by_name`
// (the emit_json_tags no-op safety — the reference models.py has zero aliases).
func buildPydanticTable(table *core.Table, body *builders.IndentStringBuilder, stripModelsPrefix bool) {
	body.WriteLine(fmt.Sprintf("class %s(pydantic.BaseModel):", table.Name))
	if table.Comment != "" {
		body.WriteIndentedLine(1, fmt.Sprintf(`"""%s"""`, table.Comment))
	}
	// When any field name is a true Python keyword it is escaped to
	// `<name>_` with the alias carried via the ANNOTATED pattern
	// (`<name>_: typing.Annotated[<type>, pydantic.Field(alias="<name>")]`) so
	// the row still maps to the real DB/result column. The Annotated pattern is
	// load-bearing for STATIC type checking: a bare
	// `<name>_: <type> = pydantic.Field(alias="<name>")` makes type checkers
	// synthesize `__init__` with the ALIAS name (`class`), so the generated row
	// construction `models.X(class_=row[1])` FAILS pyright (reportCallIssue).
	// With the Annotated pattern type checkers use the FIELD name (`class_`)
	// while pydantic still applies the alias at runtime (pydantic docs:
	// concepts/fields — "the annotated pattern... only understood by Pydantic").
	// `model_config = populate_by_name=True` (emitted once) additionally lets the
	// caller construct by field name at RUNTIME. `id` is NOT a keyword and stays
	// raw (`id: int`). This is the ONE place a Field(alias) legitimately appears
	// — reserved-keyword escapes, NOT json-tags.
	hasEscaped := false
	for _, col := range table.Columns {
		if _, escaped := core.EscapeFieldName(col.Name); escaped {
			hasEscaped = true
			break
		}
	}
	if hasEscaped {
		body.WriteIndentedLine(1, "model_config = pydantic.ConfigDict(populate_by_name=True)")
		body.NewLine()
	}
	// A zero-field class (empty Params/Row, e.g. upstream PR #100) needs a
	// `pass` body or the class header is a Python syntax error. Mirrors the enum
	// emitter's empty guard (enums.go). A non-empty class never reaches this (it
	// always emits at least one field line). The table comment alone does NOT
	// satisfy the body — a docstring-only class is valid, but we only have a
	// docstring when Comment != "", so guard on the column count specifically.
	if len(table.Columns) == 0 {
		if table.Comment == "" {
			body.WriteIndentedLine(1, "pass")
		}
		return
	}
	for _, col := range table.Columns {
		if col.Comment != "" {
			body.WriteIndentedLine(1, "# "+col.Comment)
		}
		type_ := pyFieldType(col, stripModelsPrefix)
		fieldName, escaped := core.EscapeFieldName(col.Name)
		if escaped {
			body.WriteIndentedLine(1, fmt.Sprintf(`%s: typing.Annotated[%s, pydantic.Field(alias="%s")]`, fieldName, type_, col.Name))
		} else {
			body.WriteIndentedLine(1, fieldName+": "+type_)
		}
	}
}

func (dr *Driver) buildPyTables(imp *core.Importer, tables []core.Table) (string, []byte, error) {
	fileName := "models.sql"
	body := builders.NewIndentStringBuilder(imp.C.IndentChar, imp.C.CharsPerIndentLevel)
	dr.writeFileHeader(body)
	body.WriteSqlcHeader()
	body.WriteModelFileModuleDocstring()
	// CRITICAL: pydantic resolves field annotations at runtime to build each
	// model's validator, so `from __future__ import annotations` (which turns
	// every annotation into a lazy string) is incompatible — it makes pydantic
	// resolve `"datetime.datetime"` against the module namespace at construction
	// time, raising `PydanticUserError: <Model> is not fully defined` whenever a
	// referenced type lives under `typing.TYPE_CHECKING`. the reference committed
	// models.py emits no future-annotations; the fork matches that for pydantic.
	if !dr.conf.ModelType.RuntimeFieldAnnotations() {
		body.WriteImportAnnotations()
	}
	// __all__ lists enum classes + table classes, sorted (enums are emitted
	// into models.py).
	allNames := make([]string, 0, len(imp.Enums)+len(tables))
	for _, e := range imp.Enums {
		allNames = append(allNames, e.Name)
	}
	for _, table := range tables {
		allNames = append(allNames, table.Name)
	}
	sort.Strings(allNames)
	body.WriteLine(fmt.Sprintf("__all__: %s = (", allDunderAnnotation(dr.conf.ModelType)))
	for _, n := range allNames {
		body.WriteIndentedLine(1, fmt.Sprintf("\"%s\",", n))
	}
	body.WriteLine(")")
	body.WriteString("\n")
	std, tye, pkg := imp.Imports(fileName)
	for _, imp := range std {
		body.WriteLine(imp)
	}
	if len(tye) != 0 {
		if len(std) != 0 {
			body.NewLine()
		}
		if !dr.conf.OmitTypecheckingBlock {
			body.WriteLine("if typing.TYPE_CHECKING:")
			for _, imp := range tye {
				body.WriteIndentedLine(1, imp)
			}
		} else {
			for _, imp := range tye {
				body.WriteLine(imp)
			}
		}
	}
	for i, imp := range pkg {
		if i == 0 {
			body.NewLine()
		}
		body.WriteLine(imp)
	}
	for _, enum := range imp.Enums {
		body.WriteString("\n")
		body.WriteString("\n")
		if err := buildPyEnum(enum, body); err != nil {
			return "", nil, err
		}
	}
	for _, table := range tables {
		body.WriteString("\n")
		body.WriteString("\n")
		BuildPyTabel(imp.C.ModelType, &table, body, true)
	}
	return fileName, []byte(body.String()), nil
}
