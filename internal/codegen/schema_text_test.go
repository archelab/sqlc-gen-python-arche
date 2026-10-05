package codegen

import (
	"strings"
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
)

// TestSchemaTextIsEncodedForPython pins the schema sites that copy SQL text
// into Python source: an enum or table comment is a `"""` docstring, an enum
// label is a `"..."` literal, and a column comment is a `#` block. Unencoded,
// Python read the label `a\b` as "a" + backspace, a comment that ends in `"`
// broke the docstring, and the second line of a column comment ran as code.
func TestSchemaTextIsEncodedForPython(t *testing.T) {
	enum := builders.NewIndentStringBuilder("    ", 1)
	err := buildPyEnum(core.Enum{
		Name:    "Mood",
		Comment: `say "hi"`,
		Constants: []core.Constant{
			{Value: `a\b`},
			{Value: `say "hi"`},
		},
	}, enum)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`    """say "hi\""""` + "\n",
		` = "a\\b"` + "\n",
		` = "say \"hi\""` + "\n",
	} {
		if !strings.Contains(enum.String(), want) {
			t.Errorf("enum: missing %q in:\n%s", want, enum.String())
		}
	}

	table := builders.NewIndentStringBuilder("    ", 1)
	buildPydanticTable(&core.Table{
		Name:    "Note",
		Comment: `rows with a "quoted" end"`,
		Columns: []core.Column{{Name: "body", Type: core.PyType{Type: "str"}, Comment: "first line\nsecond = 1"}},
	}, table, false)
	for _, want := range []string{
		`    """rows with a "quoted" end\""""` + "\n",
		"    # first line\n    # second = 1\n    body: str\n",
	} {
		if !strings.Contains(table.String(), want) {
			t.Errorf("table: missing %q in:\n%s", want, table.String())
		}
	}
}
