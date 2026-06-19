package internal

import (
	"strings"
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

func tbl(sourceName, structName string) core.Table {
	return core.Table{
		Table: &plugin.Identifier{Schema: "public", Name: sourceName},
		Name:  structName,
	}
}

// Two tables singularizing to one struct name must fail loud, naming BOTH
// source tables (decision F4). validate() already builds structNames but
// never reads it back; this is the missing branch.
func TestValidateStructStructCollision(t *testing.T) {
	gen := &PythonGenerator{}
	structs := []core.Table{
		tbl("order_kind", "OrderKind"),
		tbl("order_kinds", "OrderKind"),
	}
	err := gen.validate(nil, structs)
	if err == nil {
		t.Fatal("struct-name collision must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "order_kind") || !strings.Contains(msg, "order_kinds") {
		t.Fatalf("error must name BOTH source tables, got %q", msg)
	}
}

// No collision -> no error.
func TestValidateNoCollision(t *testing.T) {
	gen := &PythonGenerator{}
	structs := []core.Table{
		tbl("customer", "Customer"),
		tbl("order_kind", "OrderKind"),
	}
	if err := gen.validate(nil, structs); err != nil {
		t.Fatalf("distinct struct names must not error: %v", err)
	}
}

// ORDERING REGRESSION LOCK (decision F4 / minor-4): validate() MUST run on the
// UNFILTERED struct set, BEFORE filterUnusedStructs, so an omit_unused_models
// drop cannot silently dodge a real struct<->struct collision. Run() encodes
// this order (gen.go: validate the unfiltered set, then filter when
// OmitUnusedModels). This test pins it: two colliding tables that NO query
// references would BOTH be dropped by filterUnusedStructs, so a post-filter
// validate sees nothing and the collision slips through silently. validate on
// the unfiltered set still fires. If a future refactor moves validate() after
// the filter, this test goes red.
func TestValidateCollisionFiresBeforeFilter(t *testing.T) {
	gen := &PythonGenerator{}
	// Both tables singularize to OrderKind, and neither is referenced by any
	// query, so omit_unused_models would drop both.
	structs := []core.Table{
		tbl("order_kind", "OrderKind"),
		tbl("order_kinds", "OrderKind"),
	}

	// 1) The unfiltered set MUST collide (validate runs here in Run()).
	if err := gen.validate(nil, structs); err == nil {
		t.Fatal("collision in the unfiltered set must be rejected")
	}

	// 2) Prove the hazard is real: with no query referencing either struct,
	//    filterUnusedStructs drops BOTH, so a post-filter validate would NOT
	//    catch the collision. That is exactly why validate must run first.
	var noQueries []core.Query
	_, filtered := filterUnusedStructs(nil, structs, noQueries)
	if len(filtered) != 0 {
		t.Fatalf("filterUnusedStructs should drop both unreferenced colliding structs, got %d", len(filtered))
	}
	if err := gen.validate(nil, filtered); err != nil {
		t.Fatalf("post-filter set no longer collides (proving the order matters), got: %v", err)
	}
}
