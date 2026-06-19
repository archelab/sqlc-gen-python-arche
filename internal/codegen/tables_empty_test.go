package codegen

import (
	"strings"
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
)

// TestBuildPydanticEmptyClassEmitsPass pins P-15 (upstream PR #100): a pydantic
// class with zero columns must emit a `pass` body, or `class X(pydantic.BaseModel):`
// with no body is a Python SyntaxError. A docstring-only class (Comment set) is a
// valid body on its own and must NOT also get a `pass`.
func TestBuildPydanticEmptyClassEmitsPass(t *testing.T) {
	body := builders.NewIndentStringBuilder("    ", 1)
	BuildPyTabel(core.ModelTypePydantic, &core.Table{Name: "EmptyParams", Columns: nil}, body, false)
	out := body.String()
	if !strings.Contains(out, "class EmptyParams(pydantic.BaseModel):") {
		t.Fatalf("missing class header:\n%s", out)
	}
	if !strings.Contains(out, "pass") {
		t.Errorf("empty pydantic class must emit a `pass` body:\n%s", out)
	}

	// A non-empty class never emits `pass`.
	body2 := builders.NewIndentStringBuilder("    ", 1)
	BuildPyTabel(core.ModelTypePydantic, &core.Table{
		Name:    "OneField",
		Columns: []core.Column{{Name: "x", Type: core.PyType{Type: "int"}}},
	}, body2, false)
	if strings.Contains(body2.String(), "pass") {
		t.Errorf("non-empty class must not emit `pass`:\n%s", body2.String())
	}

	// A docstring-only (commented) empty class is a valid body — no `pass`.
	body3 := builders.NewIndentStringBuilder("    ", 1)
	BuildPyTabel(core.ModelTypePydantic, &core.Table{Name: "Documented", Comment: "a doc", Columns: nil}, body3, false)
	if strings.Contains(body3.String(), "pass") {
		t.Errorf("docstring-only empty class must not emit `pass` (docstring is the body):\n%s", body3.String())
	}
}
