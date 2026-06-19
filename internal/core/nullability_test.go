package core

import (
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

func compileNO(t *testing.T, no *NullabilityOverride) {
	t.Helper()
	if err := no.parse(); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
}

// A bare output-field name matches a Table==nil aggregate column purely by
// name (the F2 shared obligation: Override.Matches returns false on nil Table,
// so NullabilityOverride must match aggregate/get_customer_primary_kind
// columns by output-field name).
func TestNullabilityOverrideMatchesAggregateColumn(t *testing.T) {
	no := &NullabilityOverride{Column: "scope_owner_kind", Nullable: true}
	compileNO(t, no)
	aggCol := &plugin.Column{Name: "scope_owner_kind", Table: nil, IsFuncCall: true}
	if !no.Matches(aggCol, "public") {
		t.Fatal("bare-name override must match a Table==nil aggregate column")
	}
	other := &plugin.Column{Name: "something_else", Table: nil}
	if no.Matches(other, "public") {
		t.Fatal("override must not match an unrelated column")
	}
}

// A table-qualified override matches a COALESCE->ColumnRef column that retains
// its source Table.
func TestNullabilityOverrideMatchesQualifiedColumn(t *testing.T) {
	no := &NullabilityOverride{Column: "ledger_charge.penalty_amount", Nullable: false}
	compileNO(t, no)
	col := &plugin.Column{
		Name:  "penalty_amount",
		Table: &plugin.Identifier{Schema: "public", Name: "ledger_charge"},
	}
	if !no.Matches(col, "public") {
		t.Fatal("table-qualified override must match the column on its table")
	}
	wrongTable := &plugin.Column{
		Name:  "penalty_amount",
		Table: &plugin.Identifier{Schema: "public", Name: "other"},
	}
	if no.Matches(wrongTable, "public") {
		t.Fatal("table-qualified override must not match a different table")
	}
}

// A NullabilityOverride targets the OUTPUT field name (the SELECT alias the
// user sees in the generated row), NOT the underlying source column. the reference
// loosen sed matched `legal_form` — the alias of `nat.document_number AS
// legal_form` — so the override must key on col.Name (= the alias /
// generated field name), not col.OriginalName (= the pre-alias source column
// `document_number`). Matching OriginalName would BOTH miss the intended field AND
// over-match every other `document_number`-derived column in the tree.
func TestNullabilityOverrideMatchesOutputAliasNotSourceColumn(t *testing.T) {
	no := &NullabilityOverride{Column: "legal_form", Nullable: true}
	compileNO(t, no)

	// The aliased ColumnRef: generated field name (col.Name) is the alias; the
	// source column it came from (col.OriginalName) is `document_number`.
	aliased := &plugin.Column{Name: "legal_form", OriginalName: "document_number"}
	if !no.Matches(aliased, "public") {
		t.Fatal("override must match the OUTPUT alias (col.Name), even when OriginalName is the source column")
	}

	// A different column whose underlying source is also `document_number` must NOT be
	// flipped — the override keys on the output field name, not the source.
	otherNumero := &plugin.Column{Name: "document_number", OriginalName: "document_number"}
	if no.Matches(otherNumero, "public") {
		t.Fatal("override must not over-match an unrelated column sharing the source name `document_number`")
	}
}

// A three-part schema.table.column override matches only when ALL THREE of
// schema, table, and column line up. The parse() case-3 branch and the
// tableSchema check in Matches (nullability.go:55-64 / :109-111) are otherwise
// reachable but untested. the consuming project is single-catalog/single-schema today, so this
// path is the explicit-schema insurance: a `public.ledger_charge.penalty_amount` spec must
// match the column on schema=public/table=ledger_charge and reject a same-named column on
// a different schema.
func TestNullabilityOverrideMatchesThreePartSchemaQualified(t *testing.T) {
	no := &NullabilityOverride{Column: "public.ledger_charge.penalty_amount", Nullable: false}
	compileNO(t, no)

	col := &plugin.Column{
		Name:  "penalty_amount",
		Table: &plugin.Identifier{Schema: "public", Name: "ledger_charge"},
	}
	if !no.Matches(col, "public") {
		t.Fatal("three-part schema-qualified override must match on schema+table+column")
	}

	// An explicit-schema spec must distinguish schemas: the same table.column in
	// a different schema must NOT be flipped.
	wrongSchema := &plugin.Column{
		Name:  "penalty_amount",
		Table: &plugin.Identifier{Schema: "analytics", Name: "ledger_charge"},
	}
	if no.Matches(wrongSchema, "public") {
		t.Fatal("three-part override must not match a column on a different schema")
	}

	// A schema-empty column falls back to defaultSchema; matching `public` here
	// keeps the spec honest for unqualified catalog rows.
	defaultedSchema := &plugin.Column{
		Name:  "penalty_amount",
		Table: &plugin.Identifier{Schema: "", Name: "ledger_charge"},
	}
	if !no.Matches(defaultedSchema, "public") {
		t.Fatal("three-part override must match when an empty column schema falls back to defaultSchema=public")
	}
	if no.Matches(defaultedSchema, "analytics") {
		t.Fatal("three-part override must not match when the defaultSchema fallback is a different schema")
	}

	// Right schema, wrong table must not match.
	wrongTable := &plugin.Column{
		Name:  "penalty_amount",
		Table: &plugin.Identifier{Schema: "public", Name: "other"},
	}
	if no.Matches(wrongTable, "public") {
		t.Fatal("three-part override must not match a different table")
	}
}

// The validator fails loud on an empty column spec.
func TestNullabilityOverrideValidatorFailsLoud(t *testing.T) {
	no := &NullabilityOverride{Column: "", Nullable: true}
	err := no.parse()
	if err == nil {
		t.Fatal("empty column spec must be rejected")
	}
	if !strings.Contains(err.Error(), "column") {
		t.Fatalf("error must mention the missing column spec, got %q", err.Error())
	}
}
