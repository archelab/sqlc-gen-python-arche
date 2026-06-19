package internal

import (
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/archelab/sqlc-gen-python-arche/internal/types"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

func newPgGen(t *testing.T, opts string) *PythonGenerator {
	t.Helper()
	req := &plugin.GenerateRequest{
		Catalog:       &plugin.Catalog{DefaultSchema: "public"},
		Settings:      &plugin.Settings{Engine: "postgresql"},
		PluginOptions: []byte(opts),
	}
	conf, err := core.ParseConfig(req)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return &PythonGenerator{
		req:                req,
		config:             conf,
		typeConversionFunc: types.PostgresTypeToPython,
	}
}

// A NullabilityOverride keyed by output-field name flips IsNullable at the
// post-return chokepoint, even for a NOT NULL column.
func TestChokepointFlipsNullability(t *testing.T) {
	gen := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false,
		"nullability_overrides": [{"column": "scope_owner_kind", "nullable": true}]
	}`)
	// A NOT NULL aggregate column (Table==nil) that the override loosens.
	col := &plugin.Column{
		Name:       "scope_owner_kind",
		NotNull:    true,
		IsFuncCall: true,
		Type:       &plugin.Identifier{Name: "text"},
	}
	got := gen.makePythonType(col)
	if !got.IsNullable {
		t.Fatal("override must loosen the NOT NULL aggregate column to nullable")
	}
}

// Precedence: the nullability override applies AFTER type-remap. A type
// Override remaps the python type; the NullabilityOverride then forces
// nullability on the SAME column, and both effects survive.
func TestChokepointNullabilityAfterTypeRemap(t *testing.T) {
	gen := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false,
		"overrides": [{"column": "ledger_charge.penalty_amount", "py_type": {"type": "MyMoney", "import": "decimal"}}],
		"nullability_overrides": [{"column": "ledger_charge.penalty_amount", "nullable": true}]
	}`)
	col := &plugin.Column{
		Name:    "penalty_amount",
		NotNull: true,
		Table:   &plugin.Identifier{Schema: "public", Name: "ledger_charge"},
		Type:    &plugin.Identifier{Name: "numeric"},
	}
	got := gen.makePythonType(col)
	if got.Type != "MyMoney" {
		t.Fatalf("type-remap must still apply, got type %q", got.Type)
	}
	if !got.IsNullable {
		t.Fatal("nullability override must apply AFTER the type-remap")
	}
}

// A tighten override forces a nullable column to non-null (the COALESCE
// tighten case).
func TestChokepointTightensNullability(t *testing.T) {
	gen := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false,
		"nullability_overrides": [{"column": "coalesced", "nullable": false}]
	}`)
	col := &plugin.Column{
		Name:    "coalesced",
		NotNull: false,
		Type:    &plugin.Identifier{Name: "text"},
	}
	got := gen.makePythonType(col)
	if got.IsNullable {
		t.Fatal("tighten override must force the nullable column to non-null")
	}
}

// chargePenaltyCol is the test/shape_repro LockChargeRows.charge_penalty_value
// result column: a COALESCE(fb.penalty_amount, fb.interest_amount) over TWO nullable
// numeric columns. COALESCE-over-nullables is inferred NULLABLE by native sqlc
// (NotNull:false), and the column RETAINS its source Table (ledger_charge), which
// is exactly what the table-qualified `ledger_charge.charge_penalty_value` override
// reaches. numeric -> decimal.Decimal.
func chargePenaltyCol() *plugin.Column {
	return &plugin.Column{
		Name:    "charge_penalty_value",
		NotNull: false,
		Table:   &plugin.Identifier{Schema: "public", Name: "ledger_charge"},
		Type:    &plugin.Identifier{Name: "numeric"},
	}
}

// minor-1 dedicated guard: the `ledger_charge.charge_penalty_value: nullable:false`
// override in test/shape_repro/sqlc.yaml is LOAD-BEARING. The deletion-proof
// golden would catch its removal only as an opaque byte-diff; this test names
// the regression directly — strip the override and LockChargeRowsRow's
// charge_penalty_value field regresses from `decimal.Decimal` to
// `decimal.Decimal | None`. It is the COALESCE analogue of the embed
// running-index guard (a fast, dedicated invariant test supplementing a golden
// that could not say WHY it broke). Pinned with the exact shape_repro override
// shape so a future edit that drops the line fails here, loudly.
func TestChokepointChargePenaltyTightenIsLoadBearing(t *testing.T) {
	// WITH the shape_repro override: the COALESCE column is tightened to a
	// non-Optional decimal.Decimal — the LockChargeRowsRow.charge_penalty_value
	// shape the committed golden carries.
	withOverride := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false,
		"nullability_overrides": [
			{"column": "ledger_charge.charge_penalty_value", "nullable": false}
		]
	}`)
	got := withOverride.makePythonType(chargePenaltyCol())
	if got.Type != "decimal.Decimal" {
		t.Fatalf("charge_penalty_value must be decimal.Decimal, got %q", got.Type)
	}
	if got.IsNullable {
		t.Fatal("WITH the ledger_charge.charge_penalty_value override the COALESCE column must be NON-nullable (decimal.Decimal)")
	}

	// WITHOUT the override (the regression the deletion-proof would surface as a
	// byte-diff): the SAME COALESCE column reverts to the native nullable
	// inference — decimal.Decimal | None. This is what stripping the override
	// from test/shape_repro/sqlc.yaml does. If a refactor ever made the tighten a
	// no-op (override inert), `withOverride` above and `stripped` here would
	// agree and this assertion would fail.
	stripped := newPgGen(t, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"emit_init_file": false
	}`)
	regressed := stripped.makePythonType(chargePenaltyCol())
	if !regressed.IsNullable {
		t.Fatal("WITHOUT the override the COALESCE column must regress to nullable (decimal.Decimal | None) — the override is what makes it non-Optional, proving it load-bearing")
	}

	// Targeting proof: the override is table-qualified, so it must NOT reach the
	// sibling CASE-WHEN result column (LockSourceRows.charge_penalty_value), which
	// has Table==nil and stays Optional. A bare-name or over-broad override would
	// wrongly tighten this one too, which the deletion-proof golden pins as
	// `typing.Any | None`.
	caseWhenSibling := &plugin.Column{
		Name:    "charge_penalty_value",
		NotNull: false,
		Table:   nil,
		Type:    &plugin.Identifier{Name: "numeric"},
	}
	sibling := withOverride.makePythonType(caseWhenSibling)
	if !sibling.IsNullable {
		t.Fatal("the table-qualified override must NOT reach the Table==nil CASE-WHEN sibling; it must stay nullable")
	}
}
