package core

import (
	"fmt"
	"strings"

	"github.com/sqlc-dev/plugin-sdk-go/pattern"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// NullabilityOverride forces the nullability of a generated field, independent
// of the SQL column's NOT NULL flag. It is intentionally a distinct type from
// Override: Override stays a pure type-remap whose single XOR-enforced invariant
// must not be branched. This is the seam for by-name nullability adjustment
// (loosen rules for LATERAL/LEFT-JOIN/function-call columns, and the COALESCE
// tighten rule).
//
// The spec is `[schema.][table.]column`. A bare `column` matches by output-
// field name ALONE: Override.Matches returns false on Table==nil, so a
// NullabilityOverride must be able to target function-call/aggregate result
// columns (which have a nil Table) purely by their output-field name. A
// table-qualified spec matches a column that retains its source Table (the
// COALESCE->ColumnRef class).
//
// Resolution is FIRST-MATCH: the first spec (in config order) whose Matches
// returns true wins. Exact-duplicate `column` specs are rejected at parse time
// so the order-dependence cannot silently flip a field's nullability.
type NullabilityOverride struct {
	// Column is the [schema.][table.]column spec to match.
	Column string `json:"column" yaml:"column"`
	// Nullable is the forced nullability value: true loosens, false tightens.
	Nullable bool `json:"nullable" yaml:"nullable"`

	columnName  *pattern.Match `json:"-" yaml:"-"`
	tableRel    *pattern.Match `json:"-" yaml:"-"`
	tableSchema *pattern.Match `json:"-" yaml:"-"`
}

func (no *NullabilityOverride) parse() error {
	if no.Column == "" {
		return fmt.Errorf("nullability override must specify a `column`")
	}
	parts := strings.Split(no.Column, ".")
	var err error
	switch len(parts) {
	case 1:
		// bare output-field name -> match by column name only (aggregate /
		// Table==nil class).
		if no.columnName, err = pattern.MatchCompile(parts[0]); err != nil {
			return err
		}
	case 2:
		if no.tableRel, err = pattern.MatchCompile(parts[0]); err != nil {
			return err
		}
		if no.columnName, err = pattern.MatchCompile(parts[1]); err != nil {
			return err
		}
	case 3:
		if no.tableSchema, err = pattern.MatchCompile(parts[0]); err != nil {
			return err
		}
		if no.tableRel, err = pattern.MatchCompile(parts[1]); err != nil {
			return err
		}
		if no.columnName, err = pattern.MatchCompile(parts[2]); err != nil {
			return err
		}
	default:
		return fmt.Errorf("nullability override `column` %q is not the proper format, expected '[schema.][table.]column'", no.Column)
	}
	return nil
}

// Matches reports whether this override targets the given column. A bare-name
// override matches by output-field name regardless of Table (so it can hit
// Table==nil aggregate columns); a table-qualified override additionally
// requires the column's source Table to match.
//
// DELIBERATE ASYMMETRY vs Override: the spec is [schema.][table.]column — three
// parts at most, with NO catalog dimension. Override.Matches additionally checks
// TableCatalog; NullabilityOverride does not. This is intentional and safe for a
// single-catalog consumer. A future multi-catalog reader who needs catalog
// scoping must extend parse() to a four-part spec and add a TableCatalog check
// here, rather than assume the two override mechanisms share a matching surface.
func (no *NullabilityOverride) Matches(col *plugin.Column, defaultSchema string) bool {
	// Match the OUTPUT field name (col.Name) — the SELECT alias the user sees in
	// the generated row — NOT col.OriginalName (the pre-alias SOURCE column). For
	// `some_table.aliased_col AS output_name`, col.Name is `output_name` and
	// col.OriginalName is `aliased_col`; the override targets `output_name`.
	// Keying on OriginalName would BOTH miss the intended field AND over-match
	// every other column derived from the same source name in the tree. Synthetic
	// columns (COALESCE/aggregate/function-call) have an empty OriginalName, so
	// this only changes behaviour for aliased ColumnRefs — exactly where the
	// output name and source name diverge.
	if no.columnName == nil || !no.columnName.MatchString(col.Name) {
		return false
	}
	if no.tableRel == nil {
		// bare-name override: match purely by output-field name.
		return true
	}
	if col.Table == nil {
		return false
	}
	if !no.tableRel.MatchString(col.Table.Name) {
		return false
	}
	schema := col.Table.Schema
	if schema == "" {
		schema = defaultSchema
	}
	if no.tableSchema != nil && !no.tableSchema.MatchString(schema) {
		return false
	}
	return true
}
