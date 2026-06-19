package core

import (
	"fmt"
	"github.com/archelab/sqlc-gen-python-arche/internal/typeConversion"
	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"sort"
	"strings"
)

type importSpec struct {
	Module       string
	Name         string
	Alias        string
	TypeChecking bool
}

func (i importSpec) String() string {
	if i.Alias != "" {
		if i.Name == "" {
			return fmt.Sprintf("import %s as %s", i.Module, i.Alias)
		}
		return fmt.Sprintf("from %s import %s as %s", i.Module, i.Name, i.Alias)
	}
	if i.Name == "" {
		return "import " + i.Module
	}
	return fmt.Sprintf("from %s import %s", i.Module, i.Name)
}

type Importer struct {
	Tables  []Table
	Queries []Query
	Enums   []Enum
	C       *Config
}

func (i *Importer) Imports(fileName string) ([]string, []string, []string) {
	if fileName == "models.sql" {
		return i.modelImports()
	}
	return i.queryImports(fileName)
}

func TableUses(name string, s Table) (bool, PyType) {
	for _, col := range s.Columns {
		if col.Type.Type == name {
			return true, col.Type
		}
	}
	return false, PyType{}

}

func (i *Importer) getModelImportSpec() (string, importSpec, error) {
	switch i.C.ModelType {
	case ModelTypeAttrs:
		return "attrs", importSpec{Module: "attrs"}, nil
	case ModelTypeDataclass:
		return "dataclasses", importSpec{Module: "dataclasses"}, nil
	case ModelTypeMsgspec:
		return "msgspec", importSpec{Module: "msgspec"}, nil
	case ModelTypePydantic:
		return "pydantic", importSpec{Module: "pydantic"}, nil
	default:
		return "", importSpec{}, fmt.Errorf("unknown model type: %s", i.C.ModelType)
	}
}

func (i *Importer) splitTypeChecking(pks map[string]importSpec) (map[string]importSpec, map[string]importSpec) {
	normalImports := make(map[string]importSpec)
	typeChecking := make(map[string]importSpec)
	for name, val := range pks {
		if val.TypeChecking {
			typeChecking[name] = val
		} else {
			normalImports[name] = val
		}
	}
	return normalImports, typeChecking
}

func (i *Importer) modelImportSpecs() (map[string]importSpec, map[string]importSpec, map[string]importSpec) {
	modelUses := func(name string) (bool, bool) {
		for _, table := range i.Tables {
			if val, _ := TableUses(name, table); val {
				return true, true
			}
		}
		return false, false
	}

	std := stdImports(modelUses)
	for _, override := range i.C.Overrides {
		if val1, val2 := modelUses(override.PyTypeName); val1 {
			std[override.PyTypeName] = importSpec{Module: override.PyImportPath, Name: override.PyPackageName, TypeChecking: val2}
		}
	}
	std, typeChecking := i.splitTypeChecking(std)
	if len(typeChecking) != 0 {
		std["typing"] = importSpec{Module: "typing"}
	}
	modelName, modelImport, err := i.getModelImportSpec()
	if err != nil {
		// ValidateConf gates ModelType upstream, so this is unreachable; panic
		// rather than silently emit a file missing its model import.
		panic(err)
	}
	std[modelName] = modelImport
	if len(i.Enums) > 0 {
		// Enums are emitted as `enum.StrEnum` subclasses INTO models.py
		// itself, so the models file needs the stdlib `import enum`. (The
		// better-python base emitted a malformed `import from <pkg> import
		// enums` referencing a non-existent separate module — fixed here.)
		std["enum"] = importSpec{Module: "enum"}
	}

	// CRITICAL (runtime field annotations): a pydantic.BaseModel resolves
	// every field annotation at class-definition time to build its validator. The
	// fork does NOT emit `from __future__ import annotations` for pydantic (see
	// tables.go), so any name referenced by a field type — `collections.abc`,
	// `datetime`, `decimal`, `uuid` — MUST be a RUNTIME import, not a
	// `typing.TYPE_CHECKING` one. Routing them under TYPE_CHECKING raises
	// `PydanticUserError: <Model> is not fully defined` the moment a row is
	// constructed. RuntimeFieldAnnotations is the single source of truth for
	// which model types need this fold.
	if i.C.ModelType.RuntimeFieldAnnotations() {
		std, typeChecking = foldPydanticRuntimeImports(std, typeChecking, i.modelFieldTypeModules())
	}

	pkg := make(map[string]importSpec)

	return std, typeChecking, pkg
}

// foldPydanticRuntimeImports moves the would-be-TYPE_CHECKING imports that a
// pydantic FIELD annotation references (pydantic resolves annotations at runtime
// to build each validator — see the CRITICAL note above) into the runtime block,
// and drops the stdlib type imports the file does not reference (ruff F401).
// `fieldModules` is the set of module names (as `importSpec.Module`, e.g.
// `datetime`, `collections.abc`, `typing`) that appear in a field annotation.
//
// Driver-infrastructure imports (asyncpg, asyncpg.cursor, sqlite3) are NOT field
// types — they type function/cursor signatures, which are NOT evaluated by
// pydantic — so they STAY in the type-checking block. (For the real downstream target
// sql_driver=sqlalchemy emits no such type-checking imports at all; this only
// matters for the pydantic-model-emitter isolation case that drives queries with
// asyncpg.)
func foldPydanticRuntimeImports(std, typeChecking map[string]importSpec, fieldModules map[string]struct{}) (map[string]importSpec, map[string]importSpec) {
	// `typing` and `collections` are added UNCONDITIONALLY by stdImports. Under
	// pydantic they are useful only as runtime field-annotation imports (there is
	// no future-annotations TYPE_CHECKING deferral), so an unreferenced one is
	// dead and must be removed from BOTH maps — otherwise it lingers as an unused
	// `if typing.TYPE_CHECKING: import collections.abc` block while `typing` itself
	// was dropped, breaking pyright (`typing` undefined) and ruff (unused import).
	if _, used := fieldModules["typing"]; !used {
		delete(std, "typing")
		delete(typeChecking, "typing")
	}
	if _, used := fieldModules["collections.abc"]; !used {
		delete(std, "collections")
		delete(typeChecking, "collections")
	}
	remainingTypeChecking := make(map[string]importSpec)
	for name, spec := range typeChecking {
		if _, isFieldType := fieldModules[spec.Module]; isFieldType {
			spec.TypeChecking = false
			std[name] = spec
		} else {
			remainingTypeChecking[name] = spec
		}
	}
	return std, remainingTypeChecking
}

// modelFieldTypeModules returns the set of import modules referenced by a
// pydantic model field annotation in models.py. Each column's resolved type
// (e.g. `datetime.datetime`, `collections.abc.Sequence[int]`, `typing.Any`) is
// scanned for the dotted module prefixes the importer can emit; a reserved-kw
// field additionally pulls in `typing` (the `typing.Annotated[...]` pattern).
func (i *Importer) modelFieldTypeModules() map[string]struct{} {
	mods := make(map[string]struct{})
	for _, table := range i.Tables {
		for _, col := range table.Columns {
			collectAnnotationModules(col.Type, col.Name, mods)
		}
	}
	return mods
}

// collectAnnotationModules adds to mods every import module a single field's
// annotation references. The dotted stdlib type modules a column can resolve to
// are a closed set (stdImports); a list column wraps in `collections.abc`, and a
// reserved-keyword field name is emitted via `typing.Annotated[...]`.
func collectAnnotationModules(typ PyType, name string, mods map[string]struct{}) {
	for _, prefix := range []string{"datetime", "decimal", "uuid", "typing", "collections.abc"} {
		if strings.HasPrefix(typ.Type, prefix+".") {
			mods[prefix] = struct{}{}
		}
	}
	if typ.IsList && !typ.ListIsBuiltin {
		mods["collections.abc"] = struct{}{}
	}
	if _, escaped := EscapeFieldName(name); escaped {
		mods["typing"] = struct{}{}
	}
	// A per-column py_type override (e.g. jsonb -> a typed pydantic model) is a
	// runtime-evaluated FIELD annotation under pydantic exactly like a stdlib
	// type, so its import module must be folded into the runtime block. The
	// override import is keyed by `PyImportPath` (the same Module the importer
	// registers for the override), so registering it here lets
	// foldPydanticRuntimeImports lift it out of `typing.TYPE_CHECKING`.
	if typ.DoOverride() && typ.Override.PyImportPath != "" {
		mods[typ.Override.PyImportPath] = struct{}{}
	}
}

func (i *Importer) queryValueUses(name string, qv QueryValue) (bool, bool) {
	if !qv.IsEmpty() {
		if qv.IsStruct() && qv.EmitStruct() {
			if val, pyType := TableUses(name, *qv.Table); val {
				if i.C.SqlDriver == SQLDriverAsyncpg {
					if pyType.DoConversion(typeConversion.AsyncpgDoTypeConversion) {
						return true, false
					} else {
						return true, true
					}
				} else if i.C.SqlDriver == SQLDriverAioSQLite || i.C.SqlDriver == SQLDriverSQLite {
					if pyType.DoConversion(typeConversion.SqliteDoTypeConversion) {
						return true, false
					} else {
						return true, true
					}
				}
				return true, false
			}
		} else if qv.IsStruct() && (i.C.SqlDriver == SQLDriverAioSQLite || i.C.SqlDriver == SQLDriverSQLite) {
			if val, pyType := TableUses(name, *qv.Table); val {
				if pyType.DoConversion(typeConversion.SqliteDoTypeConversion) {
					return true, false
				}
			}
		} else {
			if qv.Typ.Type == name {
				if i.C.SqlDriver == SQLDriverAsyncpg {
					if qv.Typ.DoConversion(typeConversion.AsyncpgDoTypeConversion) {
						return true, false
					} else {
						return true, true
					}
				} else if i.C.SqlDriver == SQLDriverAioSQLite || i.C.SqlDriver == SQLDriverSQLite {
					if qv.Typ.DoConversion(typeConversion.SqliteDoTypeConversion) {
						return true, false
					} else {
						return true, true
					}
				}
				return true, false
			}
		}
	}
	return false, false
}

func (i *Importer) queryImportSpecs(_ string) (map[string]importSpec, map[string]importSpec, map[string]importSpec, map[string]importSpec) {
	addCiso := false
	queryUses := func(name string) (bool, bool) {
		var uses *bool = nil
		var typeChecking *bool = nil

		helper := func(val1, val2 bool) {
			if uses == nil || typeChecking == nil {
				uses = new(bool)
				typeChecking = new(bool)
				*uses = val1
				*typeChecking = val2
			} else if *typeChecking == true {
				*uses = val1
				*typeChecking = val2
			}
		}
		for _, q := range i.Queries {
			//if q.SourceName != fileName { TODO q.SourceName is the name of the sql file
			//	continue
			//}
			if val1, val2 := i.queryValueUses(name, q.Ret); val1 {
				if q.Cmd == metadata.CmdMany {
					helper(val1, false)
				}
				// if we have speedups enabled then we don't need datetime in the std imports
				// we use ciso8601 for the converting and need datetime only in typechecking
				if val2 == false && (i.C.SqlDriver == SQLDriverAioSQLite || i.C.SqlDriver == SQLDriverSQLite) && i.C.Speedups && (name == "datetime.datetime" || name == "datetime.date") {
					helper(val1, true)
					addCiso = true
				} else {
					helper(val1, val2)
				}
			}
			for _, arg := range q.Args {
				if val1, val2 := i.queryValueUses(name, arg); val1 {
					// if we have speedups enabled then we don't need datetime in the std imports
					// we use ciso8601 for the converting and need datetime only in typechecking
					if val2 == false && (i.C.SqlDriver == SQLDriverAioSQLite || i.C.SqlDriver == SQLDriverSQLite) && i.C.Speedups && (name == "datetime.datetime" || name == "datetime.date") {
						helper(val1, true)
						addCiso = true
					} else {
						helper(val1, val2)
					}
				}
			}
		}
		if uses == nil || typeChecking == nil {
			return false, false
		}
		return *uses, *typeChecking
	}
	querySimpleReturn := func(conv typeConversion.TypeDoTypeConversion) bool {
		for _, q := range i.Queries {
			if !q.Ret.IsStruct() && !conv(q.Ret.Typ.SqlType) {
				return true
			}
		}
		return false
	}

	std := stdImports(queryUses)
	for _, override := range i.C.Overrides {
		if val1, val2 := queryUses(override.PyTypeName); val1 {
			std[override.PyTypeName] = importSpec{Module: override.PyImportPath, Name: override.PyPackageName, TypeChecking: val2}
		}
	}
	std, typeChecking := i.splitTypeChecking(std)
	if i.C.SqlDriver == SQLDriverAsyncpg {
		typeChecking[string(SQLDriverAsyncpg)] = importSpec{Module: string(SQLDriverAsyncpg)}

		if IsAnyQueryMany(i.Queries) {
			typeChecking[string(SQLDriverAsyncpg)+".cursor"] = importSpec{Module: string(SQLDriverAsyncpg) + ".cursor"}
			if querySimpleReturn(typeConversion.AsyncpgDoTypeConversion) {
				std["operator"] = importSpec{Module: "operator"}
			}
		}
	} else if i.C.SqlDriver == SQLDriverAioSQLite {
		// if the std mapping has exactly 2 members, these two are collections and typing,
		// but if they are more than 2, we need to add type conversion and for that we
		// need the aiosqlite in the normal import block, not in the type checking block
		if len(std) > 2 {
			std[string(SQLDriverAioSQLite)] = importSpec{Module: string(SQLDriverAioSQLite)}
		} else {
			typeChecking[string(SQLDriverAioSQLite)] = importSpec{Module: string(SQLDriverAioSQLite)}
		}
		if IsAnyQueryMany(i.Queries) {
			typeChecking[string(SQLDriverSQLite)] = importSpec{Module: string(SQLDriverSQLite)}
			if querySimpleReturn(typeConversion.SqliteDoTypeConversion) {
				std["operator"] = importSpec{Module: "operator"}
			}
		}
	} else if i.C.SqlDriver == SQLDriverSQLite {
		// if the std mapping has exactly 2 members, these two are collections and typing,
		// but if they are more than 2, we need to add type conversion and for that we
		// need the aiosqlite in the normal import block, not in the type checking block
		if len(std) > 2 {
			std[string(SQLDriverSQLite)] = importSpec{Module: string(SQLDriverSQLite)}
		} else {
			typeChecking[string(SQLDriverSQLite)] = importSpec{Module: string(SQLDriverSQLite)}
		}
		if IsAnyQueryMany(i.Queries) {
			if querySimpleReturn(typeConversion.SqliteDoTypeConversion) {
				std["operator"] = importSpec{Module: "operator"}
			}
		}
	}
	if addCiso {
		std["ciso8601"] = importSpec{Module: "ciso8601"}
	}

	pkg := make(map[string]importSpec)
	loc := make(map[string]importSpec)

	if i.C.SqlDriver == SQLDriverSQLAlchemy {
		// `sqlalchemy.text(...)` is called at runtime, so both modules live in
		// the package (runtime) import block — NOT type-checking.
		pkg["sqlalchemy"] = importSpec{Module: "sqlalchemy"}
		pkg["sqlalchemy.ext.asyncio"] = importSpec{Module: "sqlalchemy.ext.asyncio"}
	}

	queryValueModelImports := func(qv QueryValue) {
		if qv.IsStruct() && qv.EmitStruct() {
			modelName, modelImport, err := i.getModelImportSpec()
			if err != nil {
				panic(err)
			}
			std[modelName] = modelImport
		}
	}

	for _, q := range i.Queries {
		//if q.SourceName != fileName { TODO
		//	continue
		//}
		queryValueModelImports(q.Ret)
		// A bundled Params arg (param count > query_parameter_limit, or any
		// :copyfrom) is an inline-emitted struct living in the QUERY file, not
		// models.py. Its class statement references the model base
		// (`pydantic.BaseModel`) at definition time, so the query file needs the
		// model import (`import pydantic`) even when no Ret struct is emitted —
		// without it the file raises NameError at import (ruff F821 / pyright
		// reportUndefinedVariable). The import is resolved over q.Args too so an
		// inline Params bundle with no Ret struct still imports its model base.
		for _, arg := range q.Args {
			queryValueModelImports(arg)
		}
		if q.Cmd == metadata.CmdCopyFrom {
			modelName, modelImport, err := i.getModelImportSpec()
			if err != nil {
				panic(err)
			}
			std[modelName] = modelImport
		}
	}

	// Only import `from <package> import models` when the query file actually
	// references `models.X`. Importing it unconditionally left a DEAD import in
	// scalar/exec-only files (ruff F401 / pyright reportUnusedImport) — the
	// better-python base never surfaced this because its own test queries always
	// return models. the reference query files all reference models, and every
	// existing-driver golden references models, so this is byte-neutral there;
	// it only drops the genuinely-unused import from model-free files.
	if i.queriesReferenceModels() {
		loc["models"] = importSpec{Module: i.C.Package, Name: "models"}
	}

	// CRITICAL (runtime field annotations): see foldPydanticRuntimeImports.
	// Query files carry inline `<Method>Params(pydantic.BaseModel)` bundle classes
	// whose fields (e.g. `created_at: datetime.datetime`) are resolved at runtime,
	// so their type imports must be runtime too. `typing` is ALWAYS referenced in
	// a query file — every query constant is annotated `typing.Final[str]` (a
	// module-level annotated assignment, evaluated at runtime without
	// future-annotations) — so it is never dropped here.
	if i.C.ModelType.RuntimeFieldAnnotations() {
		std, typeChecking = foldPydanticRuntimeImports(std, typeChecking, i.queryFieldTypeModules())
	}

	return std, typeChecking, pkg, loc
}

// queryFieldTypeModules returns the set of import modules a query file's emitted
// annotations reference at runtime (no future-annotations under pydantic): the
// field types of every inline-emitted Row/Params struct, plus `typing` (every
// query constant is annotated `typing.Final[str]`, a module-level annotated
// assignment evaluated at runtime) and `collections.abc` when a SELECT `:many`
// (AsyncIterator return), a list arg, or a :copyfrom Sequence is emitted.
func (i *Importer) queryFieldTypeModules() map[string]struct{} {
	mods := make(map[string]struct{})
	// Query constants are always annotated `typing.Final[str]`.
	mods["typing"] = struct{}{}
	addQueryValue := func(qv QueryValue) {
		if qv.IsEmpty() {
			return
		}
		if qv.IsStruct() && qv.EmitStruct() {
			for _, col := range qv.Table.Columns {
				collectAnnotationModules(col.Type, col.Name, mods)
			}
			return
		}
		// A flat list arg/return is emitted as `collections.abc.Sequence[T]`
		// (or the builtin `list[T]` under emit_list_arrays, which needs no import).
		if qv.Typ.IsList && !qv.Typ.ListIsBuiltin {
			mods["collections.abc"] = struct{}{}
		}
	}
	for _, q := range i.Queries {
		// A SELECT :many returns `collections.abc.AsyncIterator[T]`. A :many over
		// INSERT/UPDATE/DELETE ... RETURNING returns the builtin `list[T]`
		// via result.all() — NO collections.abc.
		if (q.Cmd == metadata.CmdMany && !SQLRootIsDML(q.SQL)) || q.Cmd == metadata.CmdCopyFrom {
			mods["collections.abc"] = struct{}{}
		}
		addQueryValue(q.Ret)
		for _, arg := range q.Args {
			addQueryValue(arg)
		}
	}
	return mods
}

// queriesReferenceModels reports whether any query in the file emits a
// `models.X` reference, requiring the `from <package> import models` import. A
// reference arises from (1) a Ret struct that lives in models.py (IsStruct and
// NOT emitted inline), or (2) an embedded sub-struct: a column inside an
// inline-emitted Row/Params struct whose resolved type carries the `models.`
// prefix (set for sqlc.embed columns at builders.go). Mirrors exactly the two
// places the body emitter writes `models.`.
func (i *Importer) queriesReferenceModels() bool {
	columnsReferenceModels := func(cols []Column) bool {
		for _, c := range cols {
			if strings.HasPrefix(c.Type.Type, "models.") {
				return true
			}
		}
		return false
	}
	structReferencesModels := func(qv QueryValue) bool {
		if !qv.IsStruct() {
			return false
		}
		if !qv.EmitStruct() {
			// A struct return NOT emitted inline resolves to `models.X`.
			return true
		}
		// An inline-emitted struct (Row/Params) references models only via an
		// embedded sub-struct column.
		return columnsReferenceModels(qv.Table.Columns)
	}
	for _, q := range i.Queries {
		if structReferencesModels(q.Ret) {
			return true
		}
		for _, arg := range q.Args {
			if structReferencesModels(arg) {
				return true
			}
		}
	}
	return false
}

func (i *Importer) queryImports(fileName string) ([]string, []string, []string) {
	std, typeCheck, pkg, loc := i.queryImportSpecs(fileName)
	if validated := ValidatedResultTypes(i.Queries); len(validated) > 0 {
		// A validated read emits `pydantic.TypeAdapter(...)` in the query body, so
		// the file needs `import pydantic` regardless of whether it emits a model.
		std["pydantic"] = importSpec{Module: "pydantic"}
		// Each cached `_<T>_adapter: pydantic.TypeAdapter[T] = pydantic.TypeAdapter(T)`
		// declaration references its override type T BY NAME at runtime, so T must
		// be a runtime import — even when T is never a query signature type and so
		// is not pulled in by queryImportSpecs (the case for a validated column
		// nested inside a sqlc.embed, which surfaces only as a field of the
		// embedded model). For a top-level validated return type this re-asserts
		// the import it already has (idempotent: same name -> same runtime spec).
		validatedSet := make(map[string]struct{}, len(validated))
		for _, t := range validated {
			validatedSet[t] = struct{}{}
		}
		for idx := range i.C.Overrides {
			o := &i.C.Overrides[idx]
			if !o.Validate || o.PyTypeName == "" {
				continue
			}
			if _, used := validatedSet[o.PyTypeName]; !used {
				continue
			}
			std[o.PyTypeName] = importSpec{Module: o.PyImportPath, Name: o.PyPackageName}
			delete(typeCheck, o.PyTypeName) // the adapter needs T at runtime, not under TYPE_CHECKING
		}
	}

	importLines := make([]string, 0)
	typeLines := make([]string, 0)
	packageLines := make([]string, 0)
	if len(std) != 0 {
		importLines = append(importLines, buildImportBlock(std)...)
	}
	if len(typeCheck) != 0 {
		typeLines = append(typeLines, buildImportBlock(typeCheck)...)
	}
	// The QueryResultsArgsType typealias is driver-AGNOSTIC and fires
	// on any :many, but it only types the QueryResults wrapper. The SQLAlchemy
	// driver emits no QueryResults (its :many is a native async generator), so
	// the typealias would be dead text in every SQLAlchemy query file — skip it.
	if IsAnyQueryMany(i.Queries) && i.C.SqlDriver != SQLDriverSQLAlchemy {
		if len(typeCheck) != 0 {
			typeLines[len(typeLines)-1] = typeLines[len(typeLines)-1] + "\n"
		}
		queryResultsArgsType := "QueryResultsArgsType: typing.TypeAlias = int | float | str | memoryview"
		if IsInMultipleMaps("decimal", std, typeCheck) {
			queryResultsArgsType += " | decimal.Decimal"
		}
		if IsInMultipleMaps("uuid", std, typeCheck) {
			queryResultsArgsType += " | uuid.UUID"
		}
		if IsInMultipleMaps("datetime", std, typeCheck) {
			queryResultsArgsType += " | datetime.date | datetime.time | datetime.datetime | datetime.timedelta"
		}
		queryResultsArgsType += " | None"
		typeLines = append(typeLines, queryResultsArgsType)
	}

	if len(pkg) != 0 {
		packageLines = append(packageLines, buildImportBlock(pkg)...)
	}
	if len(loc) != 0 {
		if len(packageLines) != 0 {
			packageLines = append(packageLines, "")
		}
		packageLines = append(packageLines, buildImportBlock(loc)...)
	}
	return importLines, typeLines, packageLines
}

func (i *Importer) modelImports() ([]string, []string, []string) {
	std, typeCheck, pkg := i.modelImportSpecs()
	importLines := make([]string, 0)
	typeLines := make([]string, 0)
	packageLines := make([]string, 0)
	if len(std) != 0 {
		importLines = append(importLines, buildImportBlock(std)...)
	}
	if len(typeCheck) != 0 {
		typeLines = append(typeLines, buildImportBlock(typeCheck)...)
	}
	if len(pkg) != 0 {
		packageLines = append(packageLines, buildImportBlock(pkg)...)
	}
	return importLines, typeLines, packageLines
}

func buildImportBlock(pkgs map[string]importSpec) []string {
	pkgImports := make([]importSpec, 0)
	fromImports := make(map[string][]string)
	for _, is := range pkgs {
		if is.Name == "" || is.Alias != "" {
			pkgImports = append(pkgImports, is)
		} else {
			names, ok := fromImports[is.Module]
			if !ok {
				names = make([]string, 0, 1)
			}
			names = append(names, is.Name)
			fromImports[is.Module] = names
		}
	}

	importStrings := make([]string, 0, len(pkgImports)+len(fromImports))
	for _, is := range pkgImports {
		importStrings = append(importStrings, is.String())
	}
	for modName, names := range fromImports {
		sort.Strings(names)
		nameString := strings.Join(names, ", ")
		importStrings = append(importStrings, fmt.Sprintf("from %s import %s", modName, nameString))
	}
	sort.Strings(importStrings)
	return importStrings
}

// typeCheckingOverwriteProtection function that takes in importSpec map and adds/replaced imports.
// Important here is that importSpec's with TypeChecking set to false have higher priority then
// type checking imports.
func typeCheckingOverwriteProtection(std map[string]importSpec, name string, newImport importSpec) {
	if val, found := std[name]; found {
		if val.TypeChecking == true {
			std[name] = newImport
		}
	} else {
		std[name] = newImport
	}
}

func stdImports(uses func(name string) (bool, bool)) map[string]importSpec {
	std := make(map[string]importSpec)
	std["collections"] = importSpec{Module: "collections.abc", TypeChecking: true}
	std["typing"] = importSpec{Module: "typing", TypeChecking: false}
	add := func(name, module string) {
		if use, typeChecking := uses(name); use {
			typeCheckingOverwriteProtection(std, module, importSpec{Module: module, TypeChecking: typeChecking})
		}
	}

	add("decimal.Decimal", "decimal")

	add("datetime.date", "datetime")
	add("datetime.time", "datetime")
	add("datetime.datetime", "datetime")
	add("datetime.timedelta", "datetime")

	add("uuid.UUID", "uuid")
	return std
}
