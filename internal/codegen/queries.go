package codegen

import (
	"fmt"
	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/drivers"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
	"sort"
)

func (dr *Driver) prepareFunctionHeader(query *core.Query, body *builders.IndentStringBuilder) ([]core.FunctionArg, string, []string) {
	pyTableNames := make([]string, 0)
	args := make([]core.FunctionArg, 0)
	for _, arg := range query.Args {
		if !arg.IsEmpty() {
			argType := arg.Typ.Type
			if arg.EmitStruct() && arg.IsStruct() {
				BuildPyTabel(dr.conf.ModelType, arg.Table, body, false)
				body.NNewLine(2)
				pyTableNames = append(pyTableNames, arg.Table.Name)
				if query.Cmd == metadata.CmdCopyFrom {
					argType = fmt.Sprintf("collections.abc.Sequence[%s]", argType)
				}
				args = append(args, core.FunctionArg{
					Name:           arg.Name,
					Type:           argType,
					FunctionFormat: fmt.Sprintf("%s: %s", arg.Name, argType),
				})
			} else {
				if arg.Typ.IsList {
					if arg.Typ.ListIsBuiltin {
						argType = fmt.Sprintf("list[%s]", argType)
					} else {
						argType = fmt.Sprintf("collections.abc.Sequence[%s]", argType)
					}
				}
				if arg.Typ.IsNullable {
					argType = fmt.Sprintf("%s | None", argType)
				}
				args = append(args, core.FunctionArg{
					Name:           arg.Name,
					Type:           argType,
					FunctionFormat: fmt.Sprintf("%s: %s", arg.Name, argType),
				})
			}
		}
	}
	retType := "None"
	if query.Ret.EmitStruct() && query.Ret.IsStruct() {
		BuildPyTabel(dr.conf.ModelType, query.Ret.Table, body, false)
		body.NNewLine(2)
		retType = query.Ret.Table.Name
		pyTableNames = append(pyTableNames, query.Ret.Table.Name)
	} else if !query.Ret.IsEmpty() {
		if query.Ret.IsStruct() {
			retType = fmt.Sprintf("models.%s", query.Ret.Table.Name)
		} else {
			// A scalar (single-column, non-struct) return wraps an array column the
			// same way arg-rendering (above) and pyFieldType (tables.go) do. Without
			// this the bare inner type dropped the list, so an array-valued :one /
			// :many rendered `str` instead of `list[str]` (arche's COALESCE(array_agg
			// (id), ARRAY[]::varchar[]) :one column). The driver wraps `| None` (:one),
			// list[...] / AsyncIterator[...] (:many) AROUND this string, so IsNullable
			// stays the driver's concern here — only the list wrapper belongs here.
			retType = query.Ret.Typ.Type
			if query.Ret.Typ.IsList {
				if query.Ret.Typ.ListIsBuiltin {
					retType = fmt.Sprintf("list[%s]", retType)
				} else {
					retType = fmt.Sprintf("collections.abc.Sequence[%s]", retType)
				}
			}
		}
	}
	if query.Cmd == metadata.CmdExecLastId {
		retType = "int | None"
	}
	if query.Cmd == metadata.CmdExecRows || query.Cmd == metadata.CmdCopyFrom {
		retType = "int"
	}
	return args, retType, pyTableNames
}

func (dr *Driver) BuildPyQueriesFiles(imp *core.Importer, queries []core.Query) ([]*plugin.File, error) {
	files := make([]*plugin.File, 0)
	fileQueries := make(map[string][]core.Query)
	for _, query := range queries {
		if err := dr.supportedCMD(query.Cmd); err != nil {
			return nil, err
		}
		if val, found := fileQueries[query.SourceName]; found {
			fileQueries[query.SourceName] = append(val, query)
		} else {
			fileQueries[query.SourceName] = []core.Query{query}
		}
	}

	for sourceName, queries := range fileQueries {
		data, err := dr.buildPyQueriesFile(imp, queries, sourceName)
		if err != nil {
			return nil, err
		}
		files = append(files, &plugin.File{
			Name:     core.SQLToPyFileName(sourceName),
			Contents: data,
		})
	}

	return files, nil
}

func (dr *Driver) buildQueryHeader(query *core.Query, body *builders.IndentStringBuilder) {
	// TWO driver-aware emission sites. The header verb colon (`:one`) and the
	// executable SQL body are rewritten independently; the default is identity
	// for both (asyncpg/sqlite keep raw `$N` + unescaped verb). The human
	// docstring echo routes through NEITHER hook and keeps canonical `$N`.
	body.WriteLine(fmt.Sprintf(`%s: typing.Final[str] = """-- name: %s %s`, query.ConstantName, query.MethodName, dr.rewriteHeaderVerb(query.Cmd)))
	body.WriteLine(dr.rewriteSQL(query.SQL))
	body.WriteLine(`"""`)
}

func (dr *Driver) buildPyQueriesFile(imp *core.Importer, queries []core.Query, sourceName string) ([]byte, error) {
	body := builders.NewIndentStringBuilder(imp.C.IndentChar, imp.C.CharsPerIndentLevel)
	dr.writeFileHeader(body)
	body.WriteSqlcHeader()
	body.WriteQueryFileModuleDocstring(sourceName)
	// CRITICAL: query files carry inline `<Method>Params(pydantic.BaseModel)`
	// bundle classes whose annotations pydantic resolves at runtime, so
	// future-annotations is suppressed for pydantic exactly as in models.py
	// (see tables.go).
	if !dr.conf.ModelType.RuntimeFieldAnnotations() {
		body.WriteImportAnnotations()
	}

	newLines := 2
	if dr.conf.EmitClasses {
		newLines = 1
	}

	allNames := make([]string, 0)
	funcBody := builders.NewIndentStringBuilder(imp.C.IndentChar, imp.C.CharsPerIndentLevel)
	pyTableBody := builders.NewIndentStringBuilder(imp.C.IndentChar, imp.C.CharsPerIndentLevel)
	for _, query := range queries {
		if !dr.conf.EmitClasses {
			allNames = append(allNames, query.FuncName)
		}
		dr.buildQueryHeader(&query, funcBody)
		funcBody.NewLine()
	}
	// The QueryResults injection is driver-AGNOSTIC and fires on any
	// :many. For a driver whose driverBuildQueryResults is the no-op default
	// (SQLAlchemy, whose :many is a native async generator), it would emit
	// spurious blank lines and append "" to __all__ (sorting to the front as a
	// stray `"",` entry). Compute the name first and only inject when it is
	// non-empty.
	if core.IsAnyQueryMany(queries) {
		probe := builders.NewIndentStringBuilder(imp.C.IndentChar, imp.C.CharsPerIndentLevel)
		if name := dr.driverBuildQueryResults(probe); name != "" {
			funcBody.NewLine()
			funcBody.WriteString(probe.String())
			allNames = append(allNames, name)
			funcBody.NewLine()
		}
	}
	// Module-level cached pydantic.TypeAdapter per distinct validated override
	// type (`validate: true`). Caching avoids rebuilding the pydantic-core
	// validator on every read; the explicit `pydantic.TypeAdapter[<Type>]`
	// annotation also gives the validated read a KNOWN return type (a bare
	// `pydantic.TypeAdapter(<alias>)` call types as Unknown for a union/generic
	// alias). Only the SQLAlchemy driver emits validated reads today.
	if dr.conf.SqlDriver == core.SQLDriverSQLAlchemy {
		validatedTypes := core.ValidatedResultTypes(queries)
		for _, typeName := range validatedTypes {
			funcBody.NewLine()
			funcBody.WriteLine(fmt.Sprintf("%s: pydantic.TypeAdapter[%s] = pydantic.TypeAdapter(%s)", core.ValidateAdapterVar(typeName), typeName, typeName))
		}
		if len(validatedTypes) > 0 {
			// Keep the PEP8 2-blank separation before the class (E302). The
			// no-adapter path falls through unchanged, so non-validate files stay
			// byte-identical.
			funcBody.NewLine()
		}
	}
	funcBody.NewLine()
	if dr.conf.EmitClasses {
		allNames = append(allNames, dr.buildClassTemplate(sourceName, dr.connType, dr.conf, funcBody))
	}
	for i, query := range queries {
		args, retType, addedPyTableNames := dr.prepareFunctionHeader(&query, pyTableBody)
		returnType := core.PyType{
			SqlType: query.Ret.Typ.SqlType,
			Type:    retType,
		}
		allNames = append(allNames, addedPyTableNames...)
		err := dr.buildPyQueryFunc(&query, funcBody, args, returnType, dr.conf)
		if err != nil {
			return nil, err
		}
		if i != len(queries)-1 {
			funcBody.NNewLine(newLines)
		}
	}
	body.WriteLine(fmt.Sprintf("__all__: %s = (", allDunderAnnotation(dr.conf.ModelType)))
	if len(allNames) > 0 {
		sort.Slice(allNames, func(i, j int) bool { return allNames[i] < allNames[j] })
	}
	for _, n := range allNames {
		body.WriteIndentedLine(1, fmt.Sprintf("\"%s\",", n))
	}
	body.WriteLine(")")
	body.NewLine()
	std, tye, pkg := imp.Imports(sourceName)
	tyeHook := dr.driverTypeCheckingHook()
	for _, imp := range std {
		body.WriteLine(imp)
	}
	if len(tye) != 0 || len(tyeHook) != 0 {
		if len(std) != 0 {
			body.NewLine()
		}
		body.WriteLine("if typing.TYPE_CHECKING:")
		for _, imp := range tye {
			body.WriteIndentedLine(1, imp)
		}
		for i, imp := range tyeHook {
			if i == 0 && len(tye) != 0 {
				body.NewLine()
			}
			body.WriteIndentedLine(1, imp)
		}
	}
	body.WriteLine("")
	for _, imp := range pkg {
		body.WriteLine(imp)
	}
	body.NNewLine(2)
	if dr.conf.SqlDriver == core.SQLDriverAioSQLite {
		drivers.AioSQLiteBuildTypeConvFunc(queries, body, dr.conf)
	}
	if dr.conf.SqlDriver == core.SQLDriverSQLite {
		drivers.SQLite3BuildTypeConvFunc(queries, body, dr.conf)
	}
	return []byte(body.String() + pyTableBody.String() + funcBody.String()), nil
}
