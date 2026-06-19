package internal

import (
	"fmt"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/archelab/sqlc-gen-python-arche/internal/inflection"
	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
	"github.com/sqlc-dev/plugin-sdk-go/sdk"
	"sort"
	"strings"
)

func (gen *PythonGenerator) buildTable(schema *plugin.Schema, table *plugin.Table) core.Table {
	var tableName string
	if schema.Name == gen.req.Catalog.DefaultSchema {
		tableName = table.Rel.Name
	} else {
		tableName = schema.Name + "_" + table.Rel.Name
	}
	structName := tableName
	if !gen.config.EmitExactTableNames {
		structName = inflection.Singular(inflection.SingularParams{
			Name:       structName,
			Exclusions: gen.config.InflectionExcludeTableNames,
		})
	}
	t := core.Table{
		Table:   &plugin.Identifier{Schema: schema.Name, Name: table.Rel.Name},
		Name:    core.SnakeToCamel(structName, gen.config),
		Comment: table.Comment,
	}
	for i, column := range table.Columns {
		t.Columns = append(t.Columns, core.Column{
			Name:    core.ColumnName(column, i),
			Type:    gen.makePythonType(column),
			Comment: column.Comment,
		})
	}
	return t
}

func (gen *PythonGenerator) buildTables() []core.Table {
	tables := make([]core.Table, 0)
	for _, schema := range gen.req.Catalog.Schemas {
		if schema.Name == "pg_catalog" || schema.Name == "information_schema" {
			continue
		}
		for _, table := range schema.Tables {
			t := gen.buildTable(schema, table)
			tables = append(tables, t)
		}
	}
	if len(tables) > 0 {
		sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	}
	return tables
}

// escapeParamName escapes a flat (keyword-only) query-arg name. The escape
// predicate is driver-aware:
//
//   - SQLAlchemy uses the keyword-ONLY escape (core.EscapeFieldName /
//     IsPythonKeyword), which excludes `id` — so a flat param named `id` stays
//     raw `id` (and binds `{"p1": id}`), byte-matching the reference
//     get_account_balance_sheet_by_id(self, *, id: str). This also keeps a flat
//     `id` param consistent with the pydantic model FIELD named `id` (escaped
//     by the same predicate in tables.go / sqlalchemy.go) — without this they
//     diverged (`id_` param vs raw `id` field) in the same generated tree.
//   - asyncpg/aiosqlite/sqlite3 keep the broad core.Escape/IsReserved set
//     (which escapes `id` -> `id_`), preserving their committed goldens.
func (gen *PythonGenerator) escapeParamName(s string) string {
	if gen.config.SqlDriver == core.SQLDriverSQLAlchemy {
		name, _ := core.EscapeFieldName(s)
		return name
	}
	return core.Escape(s)
}

// makePythonType resolves a column to its PyType (applying any type Override),
// then routes the result through the single nullability chokepoint so a
// NullabilityOverride can force IsNullable AFTER the type-remap.
func (gen *PythonGenerator) makePythonType(col *plugin.Column) core.PyType {
	return gen.applyNullabilityOverride(col, gen.makePythonTypeRaw(col))
}

// applyNullabilityOverride is the single post-return chokepoint covering all
// three IsNullable assignment sites in makePythonTypeRaw. The first matching
// NullabilityOverride wins; type information is untouched.
func (gen *PythonGenerator) applyNullabilityOverride(col *plugin.Column, t core.PyType) core.PyType {
	for i := range gen.config.NullabilityOverrides {
		no := &gen.config.NullabilityOverrides[i]
		if no.Matches(col, gen.req.Catalog.DefaultSchema) {
			t.IsNullable = no.Nullable
			return t
		}
	}
	return t
}

func (gen *PythonGenerator) makePythonTypeRaw(col *plugin.Column) core.PyType {
	columnType := sdk.DataType(col.Type)
	strType := gen.typeConversionFunc(gen.req, col, gen.config)
	for _, override := range gen.config.Overrides {
		if override.PyTypeName == "" {
			continue
		}
		cname := col.Name
		if col.OriginalName != "" {
			cname = col.OriginalName
		}
		sameTable := override.Matches(col.Table, gen.req.Catalog.DefaultSchema)
		columnMatch := override.Column != "" && override.ColumnName.MatchString(cname) && sameTable
		dbTypeMatch := override.DBType != "" && override.DBType == columnType
		if columnMatch || dbTypeMatch {
			return core.PyType{
				SqlType:       columnType,
				Type:          override.PyTypeName,
				DefaultType:   strType,
				IsNullable:    !col.NotNull,
				IsList:        col.GetIsArray() || col.GetIsSqlcSlice(),
				IsEnum:        false,
				IsOverride:    true,
				Override:      &override,
				ListIsBuiltin: gen.config.EmitListArrays != nil && *gen.config.EmitListArrays,
			}
		}
	}
	return core.PyType{
		SqlType:       columnType,
		Type:          strType,
		DefaultType:   strType,
		IsNullable:    !col.NotNull,
		IsList:        col.GetIsArray() || col.GetIsSqlcSlice(),
		IsEnum:        false,
		ListIsBuiltin: gen.config.EmitListArrays != nil && *gen.config.EmitListArrays,
	}
}

func (gen *PythonGenerator) buildEnums() []core.Enum {
	var enums []core.Enum
	for _, schema := range gen.req.Catalog.Schemas {
		if schema.Name == "pg_catalog" || schema.Name == "information_schema" {
			continue
		}
		for _, enum := range schema.Enums {
			var enumName string
			if schema.Name == gen.req.Catalog.DefaultSchema {
				enumName = enum.Name
			} else {
				enumName = schema.Name + "_" + enum.Name
			}

			e := core.Enum{
				Name:    core.SnakeToCamel(enumName, gen.config),
				Comment: enum.Comment,
			}

			seen := make(map[string]struct{}, len(enum.Vals))
			for i, v := range enum.Vals {
				value := core.EnumReplace(v)
				if _, found := seen[value]; found || value == "" {
					value = fmt.Sprintf("value_%d", i)
				}
				e.Constants = append(e.Constants, core.Constant{
					Name:  core.SnakeToCamel(enumName+"_"+value, gen.config),
					Value: v,
					Type:  e.Name,
				})
				seen[value] = struct{}{}
			}
			enums = append(enums, e)
		}
	}
	if len(enums) > 0 {
		sort.Slice(enums, func(i, j int) bool { return enums[i].Name < enums[j].Name })
	}
	return enums
}

type goColumn struct {
	id int
	*plugin.Column
	embed *goEmbed
}

type goEmbed struct {
	modelType string
	modelName string
	fields    []core.Column
}

var cmdReturnsData = map[string]struct{}{
	metadata.CmdBatchMany: {},
	metadata.CmdBatchOne:  {},
	metadata.CmdMany:      {},
	metadata.CmdOne:       {},
}

func putOutColumns(query *plugin.Query) bool {
	_, found := cmdReturnsData[query.Cmd]
	return found
}

// look through all the structs and attempt to find a matching one to embed
// We need the name of the struct and its field names.
func newGoEmbed(embed *plugin.Identifier, structs []core.Table, defaultSchema string) *goEmbed {
	if embed == nil {
		return nil
	}

	for _, s := range structs {
		embedSchema := defaultSchema
		if embed.Schema != "" {
			embedSchema = embed.Schema
		}

		// compare the other attributes
		if embed.Catalog != s.Table.Catalog || embed.Name != s.Table.Name || embedSchema != s.Table.Schema {
			continue
		}

		fields := make([]core.Column, len(s.Columns))
		for i, f := range s.Columns {
			fields[i] = f
		}
		return &goEmbed{
			modelType: s.Name,
			modelName: s.Name,
			fields:    fields,
		}
	}

	return nil
}

func (gen *PythonGenerator) buildQueries(tables []core.Table) ([]core.Query, error) {
	qs := make([]core.Query, 0, len(gen.req.Queries))
	for _, query := range gen.req.Queries {
		if query.Name == "" {
			continue
		}
		if query.Cmd == "" {
			continue
		}

		constantName := core.UpperSnakeCase(query.Name)

		comments := query.Comments

		gq := core.Query{
			Cmd:          query.Cmd,
			ConstantName: constantName,
			FuncName:     strings.ToLower(constantName),
			FieldName:    sdk.LowerTitle(query.Name) + "Stmt",
			MethodName:   query.Name,
			SourceName:   query.Filename,
			SQL:          query.Text,
			Comments:     comments,
			Table:        query.InsertIntoTable,
		}

		// param-bundling: the keyword-only-vs-bundle boundary is driven
		// by query_parameter_limit (qpl, default 1). When the param count
		// exceeds qpl the args bundle into a generated `<Method>Params` struct
		// passed positionally as `arg` (body reads arg.field); at-or-below qpl
		// (and the single-param case) the args stay flat keyword-only. :copyfrom
		// ALWAYS bundles regardless of qpl (the upstream contract). qpl==0 means
		// "never bundle by count" — only :copyfrom bundles.
		qpl := int(*gen.config.QueryParameterLimit)
		bundle := query.Cmd == metadata.CmdCopyFrom ||
			(qpl > 0 && len(query.Params) > qpl)
		if bundle {
			var cols []goColumn
			for _, p := range query.Params {
				cols = append(cols, goColumn{
					id:     int(p.Number),
					Column: p.Column,
				})
			}
			s, err := gen.columnsToStruct(gq.MethodName+"Params", cols, true)
			if err != nil {
				return nil, err
			}
			argName := "arg"
			if query.Cmd == metadata.CmdCopyFrom {
				// :copyfrom keeps the historical `params` arg name (it is a
				// Sequence[<Method>Params], not a single struct).
				argName = "params"
			}
			gq.Args = []core.QueryValue{{
				Emit:  true,
				Name:  argName,
				Table: s,
				Typ: core.PyType{
					Type: gq.MethodName + "Params",
				},
			}}
		} else if len(query.Params) >= 1 {
			// De-collide flat arg names sharing a base (two params named
			// `status` -> `status` / `status_2`), via the SAME disambiguator
			// rule columnsToStruct uses. useID is false here, so this path does
			// NOT itself collapse two args that reuse one placeholder Number —
			// it relies on sqlc's upstream dedup of query.Params by Number, so a
			// reused $N reaches here only once. The bind dict keys on the real
			// placeholder Number regardless.
			disambiguator := core.NewNameDisambiguator()
			var values []core.QueryValue
			for i, p := range query.Params {
				base := gen.escapeParamName(core.ParamName(p))
				name := disambiguator.Next(base, i, int(p.Number), p.Column.GetIsNamedParam(), false)
				values = append(values, core.QueryValue{
					Name:   name,
					DBName: p.Column.GetName(),
					Typ:    gen.makePythonType(p.Column),
					Number: p.Number,
					Column: p.Column,
				})
			}
			gq.Args = values
		}

		if len(query.Columns) == 1 && query.Columns[0].EmbedTable == nil {
			c := query.Columns[0]
			name := core.ColumnName(c, 0)
			name = strings.Replace(name, "$", "_", -1)
			gq.Ret = core.QueryValue{
				Name:   core.Escape(name),
				DBName: name,
				Typ:    gen.makePythonType(c),
			}
		} else if putOutColumns(query) {
			var gs *core.Table
			var emit bool

			// makePythonType is a pure function of the column (override + nullability
			// scan); hoist each query column's resolved .Type once instead of
			// recomputing it for every candidate table in the O(tables × columns)
			// comparison below.
			queryColTypes := make([]string, len(query.Columns))
			for i, c := range query.Columns {
				queryColTypes[i] = gen.makePythonType(c).Type
			}

			for _, s := range tables {
				if len(s.Columns) != len(query.Columns) {
					continue
				}
				same := true
				for i, f := range s.Columns {
					c := query.Columns[i]
					sameName := f.Name == core.ColumnName(c, i)
					sameType := f.Type.Type == queryColTypes[i]
					sameTable := sdk.SameTableName(c.Table, s.Table, gen.req.Catalog.DefaultSchema)
					if !sameName || !sameType || !sameTable {
						same = false
					}
				}
				if same {
					gs = &s
					break
				}
			}

			if gs == nil {
				var columns []goColumn
				for i, c := range query.Columns {
					columns = append(columns, goColumn{
						id:     i,
						Column: c,
						embed:  newGoEmbed(c.EmbedTable, tables, gen.req.Catalog.DefaultSchema),
					})
				}
				var err error
				gs, err = gen.columnsToStruct(gq.MethodName+"Row", columns, true)
				if err != nil {
					return nil, err
				}
				emit = true
			}
			gq.Ret = core.QueryValue{
				Emit:  emit,
				Name:  "i",
				Table: gs,
			}
		}

		qs = append(qs, gq)
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].MethodName < qs[j].MethodName })
	return qs, nil
}

func (gen *PythonGenerator) columnsToStruct(name string, columns []goColumn, useID bool) (*core.Table, error) {
	gs := core.Table{
		Name: name,
	}
	disambiguator := core.NewNameDisambiguator()
	for i, c := range columns {
		colName := core.ColumnName(c.Column, i)

		// override col/tag with expected model name
		if c.embed != nil {
			colName = c.embed.modelName
		}

		baseFieldName := core.SnakeToCamel(colName, gen.config)
		// Track suffixes by the ID of the column, so that columns referring to
		// the same numbered parameter can be reused (single-sourced in
		// core.NameDisambiguator, shared with the flat multi-param arg loop).
		disambiguatedBase := disambiguator.Next(baseFieldName, i, c.id, c.IsNamedParam, useID)
		// The de-collision suffix ("" or "_2", "_3", …) the disambiguator assigned
		// when two result columns share a base name. It MUST be applied to the
		// emitted field name; otherwise two `entidade_id` columns both emit
		// `entidade_id`, producing a duplicate keyword argument at the row
		// construction site (invalid Python). Upstream emits `entidade_id` /
		// `entidade_id_2` — this restores that.
		decollisionSuffix := strings.TrimPrefix(disambiguatedBase, baseFieldName)

		// Result-column field name: singularized (historical default) or
		// verbatim when singularize_result_columns:false (drop-in-from-upstream).
		fieldName := core.ColumnName(c.Column, i)
		if gen.config.SingularizeResultColumns == nil || *gen.config.SingularizeResultColumns {
			fieldName = inflection.Singular(inflection.SingularParams{
				Name:       fieldName,
				Exclusions: gen.config.InflectionExcludeTableNames,
			})
		}
		fieldName += decollisionSuffix
		f := core.Column{
			Name:   fieldName,
			DBName: colName,
			Number: int32(c.id),
			Column: c.Column,
		}

		if c.embed == nil {
			f.Type = gen.makePythonType(c.Column)
		} else {
			f.Type = core.PyType{
				SqlType:    c.embed.modelType,
				Type:       "models." + c.embed.modelType,
				IsList:     false,
				IsNullable: false,
				IsEnum:     false,
			}
			f.EmbedFields = c.embed.fields
		}

		gs.Columns = append(gs.Columns, f)
	}

	// If a field does not have a known type, but another
	// field with the same name has a known type, assign
	// the known type to the field without a known type
	/*for i, field := range gs.Columns {
		if len(seen[field.Name]) > 1 && field.Type.Type == "interface{}" {
			for _, j := range seen[field.Name] {
				if i == j {
					continue
				}
				otherField := gs.Fields[j]
				if otherField.Type != field.Type {
					field.Type = otherField.Type
				}
				gs.Fields[i] = field
			}
		}
	}*/

	err := checkIncompatibleFieldTypes(gs.Columns)
	if err != nil {
		return nil, err
	}

	return &gs, nil
}

func checkIncompatibleFieldTypes(fields []core.Column) error {
	fieldTypes := map[string]string{}
	for _, field := range fields {
		if fieldType, found := fieldTypes[field.Name]; !found {
			fieldTypes[field.Name] = field.Type.Type
		} else if field.Type.Type != fieldType {
			return fmt.Errorf("named param %s has incompatible types: %s, %s", field.Name, field.Type.Type, fieldType)
		}
	}
	return nil
}
