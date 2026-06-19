package core

import (
	"github.com/archelab/sqlc-gen-python-arche/internal/typeConversion"
	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
	"sort"
	"strings"
)

type Table struct {
	Table   *plugin.Identifier
	Name    string
	Columns []Column
	Comment string
}

type PyType struct {
	SqlType     string
	Type        string
	DefaultType string
	IsList      bool
	IsNullable  bool
	IsEnum      bool
	IsOverride  bool
	Override    *Override
}

func (p *PyType) DoConversion(conversion typeConversion.TypeDoTypeConversion) bool {
	if p.DoOverride() {
		return true
	}
	return conversion(p.SqlType)
}
func (p *PyType) DoOverride() bool {
	return p.IsOverride && p.Override != nil
}

// DoValidate reports whether this column's override opted into fail-loud runtime
// validation (`validate: true`). When true, the READ path emits a
// `pydantic.TypeAdapter(<Type>).validate_python(row[N])` instead of returning the
// decoded jsonb value unwrapped — so a stored row that does not match the typed
// shape raises pydantic.ValidationError. It implies DoOverride (there is nothing
// to validate without an override type), so reading p.Override is safe.
func (p *PyType) DoValidate() bool {
	return p.DoOverride() && p.Override.Validate
}

// ValidateAdapterVar is the module-level cached pydantic.TypeAdapter variable a
// validated read references, derived deterministically from the override type
// name (e.g. WidgetConfig -> _WidgetConfig_adapter). Both the constant emitter
// (queries.go) and the read emitter (the sqlalchemy driver) call this so the
// declaration and the reference always agree.
func ValidateAdapterVar(typeName string) string {
	return "_" + typeName + "_adapter"
}

// ValidatedResultTypes returns the DISTINCT override type names read by these
// queries under `validate: true`, sorted for deterministic output. One cached
// TypeAdapter is emitted per name.
func ValidatedResultTypes(queries []Query) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	add := func(t PyType) {
		if !t.DoValidate() {
			return
		}
		if _, ok := seen[t.Type]; !ok {
			seen[t.Type] = struct{}{}
			out = append(out, t.Type)
		}
	}
	// scanCol descends a result column. A sqlc.embed column composes a nested
	// sub-struct: its OWN Type is the embedded model (constructed, not validated
	// as a scalar), while each embedded field is read positionally and may itself
	// carry a validate override (sqlalchemyColKwarg validates them). Recurse into
	// EmbedFields so the adapter-declaration set equals exactly the positions the
	// emit sites validate — a flat scan would miss an embedded validated column
	// and reference an undeclared `_<Type>_adapter` with no `import pydantic`.
	var scanCol func(col Column)
	scanCol = func(col Column) {
		if len(col.EmbedFields) != 0 {
			for _, embedCol := range col.EmbedFields {
				scanCol(embedCol)
			}
			return
		}
		add(col.Type)
	}
	for _, q := range queries {
		add(q.Ret.Typ)
		if q.Ret.Table != nil {
			for _, col := range q.Ret.Table.Columns {
				scanCol(col)
			}
		}
	}
	sort.Strings(out)
	return out
}

type Constant struct {
	Name  string
	Type  string
	Value string
}

type Enum struct {
	Name      string
	Comment   string
	Constants []Constant
}

func enumReplacer(r rune) rune {
	if strings.ContainsRune("-/:_", r) {
		return '_'
	} else if (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') {
		return r
	} else {
		return -1
	}
}

// EnumReplace removes all non ident symbols (all but letters, numbers and
// underscore) and returns valid ident name for provided name.
func EnumReplace(value string) string {
	return strings.Map(enumReplacer, value)
}

type QueryValue struct {
	Emit   bool
	Name   string
	DBName string // The name of the field in the database. Only set if Struct==nil.
	Table  *Table
	Typ    PyType

	// Number is the SQL placeholder number ($N) this flat arg binds to. The
	// SQLAlchemy named-bind dict keys on this (`pN`), NOT on the slice index,
	// so an out-of-order or reused placeholder still maps correctly. Zero for a
	// bundled struct arg (the per-field numbers live on Table.Columns[i].Number).
	Number int32

	// Column is kept so late in the generation process around to differentiate
	// between mysql slices and pg arrays
	Column *plugin.Column
}

func (v QueryValue) EmitStruct() bool {
	return v.Emit
}

func (v QueryValue) IsStruct() bool {
	return v.Table != nil
}

func (v QueryValue) IsEmpty() bool {
	return v.Typ.Type == "" && v.Name == "" && v.Table == nil
}

func (v QueryValue) Type() string {
	if v.Typ.Type != "" {
		return v.Typ.Type
	}
	if v.Table != nil {
		return v.Table.Name
	}
	panic("no type for QueryValue: " + v.Name)
}

type Query struct {
	Cmd          string
	Comments     []string
	MethodName   string
	FuncName     string
	FieldName    string
	ConstantName string
	SQL          string
	SourceName   string
	Ret          QueryValue
	Args         []QueryValue

	// Used for :copyfrom
	Table *plugin.Identifier
}

func (q Query) HasRetType() bool {
	scanned := q.Cmd == metadata.CmdOne || q.Cmd == metadata.CmdMany ||
		q.Cmd == metadata.CmdBatchMany || q.Cmd == metadata.CmdBatchOne
	return scanned && !q.Ret.IsEmpty()
}

func IsAnyQueryMany(queries []Query) bool {
	for _, query := range queries {
		if query.Cmd == metadata.CmdMany {
			return true
		}
	}
	return false
}
