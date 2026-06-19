package core

import (
	"fmt"
	"github.com/sqlc-dev/plugin-sdk-go/pattern"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
	"strings"
)

// OverridePyType is the structured `py_type: {import, type, package}` form this
// fork uses. (Upstream sqlc-gen-go carried an additional single-string `go_type`
// spec format; the fork replaced it entirely with the three structured fields,
// so there is no string-spec parsing path.)
type OverridePyType struct {
	Import  string `json:"import" yaml:"import"`
	Name    string `json:"type" yaml:"type"`
	Package string `json:"package" yaml:"package"`
	BuiltIn bool   `json:"-"`
}

type ParsedOverridePyType struct {
	ImportPath  string
	TypeName    string
	PackageName string
	BasicType   bool
}

func (gt OverridePyType) parse() (*ParsedOverridePyType, error) {
	return &ParsedOverridePyType{
		ImportPath:  gt.Import,
		TypeName:    gt.Name,
		PackageName: gt.Package,
		BasicType:   gt.Import == "",
	}, nil
}

type Override struct {
	// name of the golang type to use, e.g. `github.com/segmentio/ksuid.KSUID`
	PyType OverridePyType `json:"py_type" yaml:"py_type"`

	// fully qualified name of the Go type, e.g. `github.com/segmentio/ksuid.KSUID`
	DBType string `json:"db_type" yaml:"db_type"`

	// fully qualified name of the column, e.g. `accounts.id`
	Column string `json:"column" yaml:"column"`

	// Validate, when true, makes the generated READ path FAIL LOUD: instead of
	// returning the decoded jsonb value unwrapped (typed-as-model but never
	// checked), it emits `pydantic.TypeAdapter(<PyType>).validate_python(...)`, so
	// a stored row whose JSON does not match the declared shape raises
	// pydantic.ValidationError at read time. Off by default (byte-neutral when
	// unset). TypeAdapter — not `<Model>.model_validate` — is used so the override
	// type may be a union / generic / nested alias (e.g. `A | B | C[D]`), not only
	// a single pydantic.BaseModel.
	Validate bool `json:"validate" yaml:"validate"`

	ColumnName    *pattern.Match `json:"-"`
	TableCatalog  *pattern.Match `json:"-"`
	TableSchema   *pattern.Match `json:"-"`
	TableRel      *pattern.Match `json:"-"`
	PyImportPath  string         `json:"-"`
	PyPackageName string         `json:"-"`
	PyTypeName    string         `json:"-"`
	PyBasicType   bool           `json:"-"`
}

func (o *Override) Matches(n *plugin.Identifier, defaultSchema string) bool {
	if n == nil {
		return false
	}
	schema := n.Schema
	if n.Schema == "" {
		schema = defaultSchema
	}
	if o.TableCatalog != nil && !o.TableCatalog.MatchString(n.Catalog) {
		return false
	}
	if o.TableSchema == nil && schema != "" {
		return false
	}
	if o.TableSchema != nil && !o.TableSchema.MatchString(schema) {
		return false
	}
	if o.TableRel == nil && n.Name != "" {
		return false
	}
	if o.TableRel != nil && !o.TableRel.MatchString(n.Name) {
		return false
	}
	return true
}

func (o *Override) parse(req *plugin.GenerateRequest) (err error) {

	schema := "public"
	if req != nil && req.Catalog != nil {
		schema = req.Catalog.DefaultSchema
	}

	// validate option combinations
	switch {
	case o.Column != "" && o.DBType != "":
		return fmt.Errorf("Override specifying both `column` (%q) and `db_type` (%q) is not valid.", o.Column, o.DBType)
	case o.Column == "" && o.DBType == "":
		return fmt.Errorf("Override must specify one of either `column` or `db_type`")
	}

	// validate Column
	if o.Column != "" {
		colParts := strings.Split(o.Column, ".")
		switch len(colParts) {
		case 2:
			if o.ColumnName, err = pattern.MatchCompile(colParts[1]); err != nil {
				return err
			}
			if o.TableRel, err = pattern.MatchCompile(colParts[0]); err != nil {
				return err
			}
			if o.TableSchema, err = pattern.MatchCompile(schema); err != nil {
				return err
			}
		case 3:
			if o.ColumnName, err = pattern.MatchCompile(colParts[2]); err != nil {
				return err
			}
			if o.TableRel, err = pattern.MatchCompile(colParts[1]); err != nil {
				return err
			}
			if o.TableSchema, err = pattern.MatchCompile(colParts[0]); err != nil {
				return err
			}
		case 4:
			if o.ColumnName, err = pattern.MatchCompile(colParts[3]); err != nil {
				return err
			}
			if o.TableRel, err = pattern.MatchCompile(colParts[2]); err != nil {
				return err
			}
			if o.TableSchema, err = pattern.MatchCompile(colParts[1]); err != nil {
				return err
			}
			if o.TableCatalog, err = pattern.MatchCompile(colParts[0]); err != nil {
				return err
			}
		default:
			return fmt.Errorf("Override `column` specifier %q is not the proper format, expected '[catalog.][schema.]tablename.colname'", o.Column)
		}
	}

	// validate GoType
	parsed, err := o.PyType.parse()
	if err != nil {
		return err
	}
	o.PyImportPath = parsed.ImportPath
	o.PyTypeName = parsed.TypeName
	o.PyBasicType = parsed.BasicType
	o.PyPackageName = parsed.PackageName
	return nil
}
