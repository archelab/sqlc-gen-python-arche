package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// validateKnobReq builds a minimal postgresql request with a `widget` table
// carrying `config` and `payload` jsonb columns, for exercising the
// `validate: true` config guards (ValidateConf.validateValidateKnob).
func validateKnobReq(driver string, overridesJSON string) *plugin.GenerateRequest {
	cols := []*plugin.Column{
		{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "integer"}, Table: ident("widget")},
		{Name: "config", NotNull: true, Type: &plugin.Identifier{Name: "jsonb"}, Table: ident("widget")},
		{Name: "payload", NotNull: true, Type: &plugin.Identifier{Name: "jsonb"}, Table: ident("widget")},
	}
	catalog := &plugin.Catalog{
		DefaultSchema: "public",
		Schemas:       []*plugin.Schema{{Name: "public", Tables: []*plugin.Table{{Rel: ident("widget"), Columns: cols}}}},
	}
	opts := `{"package":"m","sql_driver":"` + driver + `","model_type":"pydantic","emit_classes":true,"emit_init_file":false,"overrides":` + overridesJSON + `}`
	return &plugin.GenerateRequest{
		Catalog:       catalog,
		Queries:       []*plugin.Query{},
		Settings:      &plugin.Settings{Engine: "postgresql"},
		PluginOptions: []byte(opts),
	}
}

// TestValidateKnobRejectedOnNonSQLAlchemy: `validate: true` is only honored by the
// SQLAlchemy driver. On any other driver it would be a silent no-op (and add a
// dead `import pydantic`), so config validation must reject it loudly. asyncpg is
// the realistic non-SQLAlchemy Postgres driver that reaches this guard (the
// sqlite drivers are rejected earlier by the engine-compatibility check).
func TestValidateKnobRejectedOnNonSQLAlchemy(t *testing.T) {
	req := validateKnobReq("asyncpg", `[{"column":"widget.config","py_type":{"import":"shapes","type":"WidgetConfig"},"validate":true}]`)
	_, err := Generate(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error for validate:true on asyncpg, got nil")
	}
	if !strings.Contains(err.Error(), "sqlalchemy") {
		t.Errorf("error should mention sqlalchemy, got: %v", err)
	}
}

// TestValidateKnobAcceptedOnSQLAlchemy: the same override is accepted on sqlalchemy.
func TestValidateKnobAcceptedOnSQLAlchemy(t *testing.T) {
	req := validateKnobReq("sqlalchemy", `[{"column":"widget.config","py_type":{"import":"shapes","type":"WidgetConfig"},"validate":true}]`)
	if _, err := Generate(context.Background(), req); err != nil {
		t.Fatalf("validate:true on sqlalchemy should succeed, got: %v", err)
	}
}

// TestValidateKnobCollisionAcrossModules: two validated overrides resolving the
// SAME type name from DIFFERENT modules would collapse to one cached
// `_WidgetConfig_adapter` built from whichever import wins — a silent
// wrong-validator. The guard rejects the ambiguity.
func TestValidateKnobCollisionAcrossModules(t *testing.T) {
	overrides := `[` +
		`{"column":"widget.config","py_type":{"import":"a.shapes","type":"WidgetConfig"},"validate":true},` +
		`{"column":"widget.payload","py_type":{"import":"b.shapes","type":"WidgetConfig"},"validate":true}` +
		`]`
	req := validateKnobReq("sqlalchemy", overrides)
	_, err := Generate(context.Background(), req)
	if err == nil {
		t.Fatal("expected a collision error for same type name from different modules, got nil")
	}
	if !strings.Contains(err.Error(), "different modules") {
		t.Errorf("error should mention different modules, got: %v", err)
	}
}

// TestValidateKnobSameTypeSameModuleAllowed: the SAME type from the SAME module on
// two columns is NOT a collision — it dedups to one adapter (config + extra ->
// WidgetConfig, the jsonb_validate fixture shape).
func TestValidateKnobSameTypeSameModuleAllowed(t *testing.T) {
	overrides := `[` +
		`{"column":"widget.config","py_type":{"import":"shapes","type":"WidgetConfig"},"validate":true},` +
		`{"column":"widget.payload","py_type":{"import":"shapes","type":"WidgetConfig"},"validate":true}` +
		`]`
	req := validateKnobReq("sqlalchemy", overrides)
	if _, err := Generate(context.Background(), req); err != nil {
		t.Fatalf("same type from same module on two columns should dedup, got: %v", err)
	}
}
