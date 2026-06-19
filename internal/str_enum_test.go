package internal

import (
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// TestStrEnumEmitter (P-11) drives Generate with a PG ENUM and asserts the
// emitted models.py carries a correct `class X(enum.StrEnum)` — fixing the four
// upstream defects: PascalCase class name (#47), value-derived uppercase member
// names (#49), plain string members not Any (#51), double-quoted values (#4).
func TestStrEnumEmitter(t *testing.T) {
	req := &plugin.GenerateRequest{
		Catalog: &plugin.Catalog{
			DefaultSchema: "public",
			Schemas: []*plugin.Schema{{
				Name: "public",
				Enums: []*plugin.Enum{
					{Name: "order_status", Vals: []string{"pending", "out-for-delivery", "delivered"}},
				},
				Tables: []*plugin.Table{{
					Rel: ident("orders"),
					Columns: []*plugin.Column{
						{Name: "order_id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("orders")},
						{Name: "status", NotNull: true, Type: &plugin.Identifier{Name: "order_status"}, Table: ident("orders")},
					},
				}},
			}},
		},
		Queries: []*plugin.Query{{
			Name:     "GetOrder",
			Cmd:      metadata.CmdOne,
			Filename: "queries.sql",
			Text:     "SELECT order_id, status FROM orders WHERE order_id = $1",
			Columns: []*plugin.Column{
				{Name: "order_id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("orders")},
				{Name: "status", NotNull: true, Type: &plugin.Identifier{Name: "order_status"}, Table: ident("orders")},
			},
			Params: []*plugin.Parameter{{Number: 1, Column: &plugin.Column{Name: "order_id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}}}},
		}},
		Settings: &plugin.Settings{Engine: "postgresql"},
		PluginOptions: []byte(`{
			"package": "m",
			"sql_driver": "sqlalchemy",
			"model_type": "pydantic",
			"emit_classes": true,
			"emit_init_file": false
		}`),
	}
	resp, err := Generate(t.Context(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var models string
	for _, f := range resp.Files {
		if f.Name == "models.py" {
			models = string(f.Contents)
		}
	}
	if models == "" {
		t.Fatal("no models.py emitted")
	}
	if !strings.Contains(models, "class OrderStatus(enum.StrEnum):") {
		t.Error("#47: enum class must be PascalCase OrderStatus with enum.StrEnum base")
	}
	if !strings.Contains(models, "import enum") || strings.Contains(models, "import from") {
		t.Error("models.py must `import enum` (stdlib), not a malformed `import from ... import enums`")
	}
	for _, member := range []string{
		`PENDING = "pending"`,
		`OUT_FOR_DELIVERY = "out-for-delivery"`,
		`DELIVERED = "delivered"`,
	} {
		if !strings.Contains(models, member) {
			t.Errorf("#49/#4: missing clean double-quoted member %q", member)
		}
	}
	// #49: members must NOT be prefixed with the enum name.
	if strings.Contains(models, "OrderStatusPending") || strings.Contains(models, "ORDER_STATUS_PENDING") {
		t.Error("#49: enum members must be value-derived, not prefixed with the enum name")
	}
	// #51: no `Any` typed members.
	if strings.Contains(models, "PENDING: ") {
		t.Error("#51: enum members must be plain string assignments, not typed (no Any)")
	}
	// In-models the enum-typed column is bare, not models.-prefixed.
	if !strings.Contains(models, "status: OrderStatus") {
		t.Error("in models.py the enum column type must be bare OrderStatus (no models. prefix)")
	}
}

// TestStrEnumMemberCollisionFailsLoud pins the de-collision guard: StrEnum
// member identifiers are derived from the raw value (sanitize + uppercase), so
// two values differing only by case or by which non-identifier symbol they use
// collapse to the same member name and the second would silently shadow the
// first in Python. The generator must fail loud naming both values rather than
// emit a lossy enum class.
func TestStrEnumMemberCollisionFailsLoud(t *testing.T) {
	for _, tc := range []struct {
		name string
		vals []string
	}{
		{"case_only", []string{"pending", "PENDING"}},
		{"symbol_only", []string{"a-b", "a/b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &plugin.GenerateRequest{
				Catalog: &plugin.Catalog{
					DefaultSchema: "public",
					Schemas: []*plugin.Schema{{
						Name:  "public",
						Enums: []*plugin.Enum{{Name: "kind", Vals: tc.vals}},
						Tables: []*plugin.Table{{
							Rel: ident("widgets"),
							Columns: []*plugin.Column{
								{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("widgets")},
								{Name: "kind", NotNull: true, Type: &plugin.Identifier{Name: "kind"}, Table: ident("widgets")},
							},
						}},
					}},
				},
				Queries: []*plugin.Query{{
					Name:     "GetWidget",
					Cmd:      metadata.CmdOne,
					Filename: "queries.sql",
					Text:     "SELECT widget_id, kind FROM widgets WHERE widget_id = $1",
					Columns: []*plugin.Column{
						{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("widgets")},
						{Name: "kind", NotNull: true, Type: &plugin.Identifier{Name: "kind"}, Table: ident("widgets")},
					},
					Params: []*plugin.Parameter{{Number: 1, Column: &plugin.Column{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}}}},
				}},
				Settings: &plugin.Settings{Engine: "postgresql"},
				PluginOptions: []byte(`{
					"package": "m",
					"sql_driver": "sqlalchemy",
					"model_type": "pydantic",
					"emit_classes": true,
					"emit_init_file": false
				}`),
			}
			_, err := Generate(t.Context(), req)
			if err == nil {
				t.Fatalf("expected a fail-loud collision error for values %v, got nil", tc.vals)
			}
			for _, v := range tc.vals {
				if !strings.Contains(err.Error(), v) {
					t.Errorf("collision error must name the offending value %q; got: %v", v, err)
				}
			}
		})
	}
}
