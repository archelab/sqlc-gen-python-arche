package internal

import (
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// These tests characterize how far a NullabilityOverride can reach at the
// makePythonType chokepoint, and where it cannot.
//
// The chokepoint CAN target a Table==nil result column (an aggregate or a
// function-call column) by its output-field name: the column's .Name survives
// even when .Table is nil and .IsFuncCall is true. What the chokepoint CANNOT
// do is scope the match to a specific query or struct — makePythonType(col)
// receives only a *plugin.Column, with no query name and no owning-struct name.
// So if two emit sites expose a same-named column and only one should flip, a
// bare-name override matches both.
//
// Consequences for the two override forms:
//   - Uniquely-named aggregate/function-call result columns (a name that appears
//     in exactly one query row and on no base-table model) are targeted exactly
//     by a bare-name override.
//   - A field that appears in several query rows — e.g. a COALESCE column that
//     keeps its source table vs. a CASE-WHEN sibling with Table==nil — is
//     disambiguated by a TABLE-QUALIFIED override (table.column), which hits
//     only the table-retaining column and leaves the Table==nil siblings alone.
//   - A genuine same-name collision that NEITHER bare-name NOR table-qualified
//     can resolve: when a query ColumnRef and a base view-model column share the
//     SAME table, no name+table key separates the override target from the base
//     model, so a bare-name override flips both. Resolving this requires a
//     query/struct-scoped in-generator post-pass (see the skipped test below).

func TestSpikeChokepointTargetsAggregateButNotPerQuery(t *testing.T) {
	gen := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false,
		"nullability_overrides": [{"column": "total_amount", "nullable": true}]
	}`)

	// A SUM() aggregate result column: Table==nil, IsFuncCall==true, NOT NULL
	// as sqlc sees it. The chokepoint reaches it by name.
	agg := &plugin.Column{
		Name:       "total_amount",
		NotNull:    true,
		IsFuncCall: true,
		Table:      nil,
		Type:       &plugin.Identifier{Name: "numeric"},
	}
	if got := gen.makePythonType(agg); !got.IsNullable {
		t.Fatal("chokepoint must reach the Table==nil aggregate column by output-field name")
	}

	// DEMONSTRATING THE LIMITATION: the same output-field name in a different
	// query is also matched, because makePythonType has no query context. This
	// is acceptable only when the targeted fields are uniquely named; it
	// documents when the post-pass fallback would be required.
	sameNameOtherQuery := &plugin.Column{
		Name:    "total_amount",
		NotNull: true,
		Table:   nil,
		Type:    &plugin.Identifier{Name: "numeric"},
	}
	if got := gen.makePythonType(sameNameOtherQuery); !got.IsNullable {
		t.Fatal("bare-name override matches any column of that name (no query scope) - expected")
	}
}

// PINS the over-match: a bare-name loosen override for a column name flips BOTH
// the query function-call column (Table==nil, intended) AND a base view-model
// column of the same name on its source table (NOT intended). No name+table key
// separates them because the query ColumnRef and the base model column share
// the SAME view table. This is the genuine collision; it documents WHY the
// query/struct-scoped post-pass fallback is required, not silently assumed safe.
func TestSpikeScopeBareNameOverMatchesBaseViewModel(t *testing.T) {
	gen := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false,
		"nullability_overrides": [{"column": "owner_kind", "nullable": true}]
	}`)

	// The QUERY function-call column: Table==nil. Intended loosen target.
	queryFuncCall := &plugin.Column{
		Name:       "owner_kind",
		NotNull:    true,
		IsFuncCall: true,
		Table:      nil,
		Type:       &plugin.Identifier{Name: "text"},
	}
	if got := gen.makePythonType(queryFuncCall); !got.IsNullable {
		t.Fatal("bare-name override must loosen the query function-call column (intended)")
	}

	// The BASE VIEW MODEL column of the same name, carried on its source view
	// table. A caller may need this to STAY non-null, but the bare-name override
	// flips it too — there is no name+table key that separates it from the query
	// column (same view table; the function-call sibling has no table at all).
	baseViewModelCol := &plugin.Column{
		Name:    "owner_kind",
		NotNull: true,
		Table:   &plugin.Identifier{Schema: "public", Name: "some_view"},
		Type:    &plugin.Identifier{Name: "text"},
	}
	if got := gen.makePythonType(baseViewModelCol); !got.IsNullable {
		t.Fatal("DOCUMENTED OVER-MATCH: bare-name override also flips the base view-model column - this is why the struct-scoped fallback is required")
	}
}

// TestScopeLoosenDoesNotOverMatchBaseViewModel is the desired behavior the
// query/struct-scoped post-pass fallback would deliver: a bare-name loosen that
// targets a query's function-call column must NOT flip a same-named base
// view-model column on its source table. It is skipped because the post-pass is
// not implemented — this keeps the gap a visible, greppable suite artifact (the
// follow-up to TestSpikeScopeBareNameOverMatchesBaseViewModel) rather than only
// prose.
func TestScopeLoosenDoesNotOverMatchBaseViewModel(t *testing.T) {
	t.Skip("post-pass query/struct-scoped nullability fallback not implemented; " +
		"see TestSpikeScopeBareNameOverMatchesBaseViewModel for the current over-match")

	gen := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false,
		"nullability_overrides": [{"column": "owner_kind", "nullable": true}]
	}`)

	baseViewModelCol := &plugin.Column{
		Name:    "owner_kind",
		NotNull: true,
		Table:   &plugin.Identifier{Schema: "public", Name: "some_view"},
		Type:    &plugin.Identifier{Name: "text"},
	}
	if got := gen.makePythonType(baseViewModelCol); got.IsNullable {
		t.Fatal("base view-model column must stay non-null under a query-scoped loosen")
	}
}
