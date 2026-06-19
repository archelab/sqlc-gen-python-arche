package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// runSQLAlchemyFiles generates with the SQLAlchemy/pydantic config and returns
// the models.py and queries.py contents as a map keyed by file name.
func runSQLAlchemyFiles(t *testing.T, catalog *plugin.Catalog, queries []*plugin.Query) map[string]string {
	t.Helper()
	req := &plugin.GenerateRequest{
		Catalog:  catalog,
		Queries:  queries,
		Settings: &plugin.Settings{Engine: "postgresql"},
		PluginOptions: []byte(`{
			"package": "m",
			"sql_driver": "sqlalchemy",
			"model_type": "pydantic",
			"emit_classes": true,
			"emit_init_file": false
		}`),
	}
	resp, err := Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	files := make(map[string]string, len(resp.Files))
	for _, f := range resp.Files {
		files[f.Name] = string(f.Contents)
	}
	return files
}

// datetimeDecimalCatalog mirrors a table carrying BOTH a datetime and a decimal
// column — the two stdlib types the reference tables use most. Under pydantic
// these field annotations are resolved at RUNTIME to build the validator, so
// both imports MUST land in the runtime block.
func datetimeDecimalCatalog() *plugin.Catalog {
	cols := []*plugin.Column{
		{Name: "id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("ledger")},
		{Name: "amount", NotNull: true, Type: &plugin.Identifier{Name: "numeric"}, Table: ident("ledger")},
		{Name: "created_at", NotNull: true, Type: &plugin.Identifier{Name: "timestamptz"}, Table: ident("ledger")},
	}
	return &plugin.Catalog{
		DefaultSchema: "public",
		Schemas: []*plugin.Schema{{
			Name:   "public",
			Tables: []*plugin.Table{{Rel: ident("ledger"), Columns: cols}},
		}},
	}
}

// TestSQLAlchemyPydanticEmitsRuntimeAnnotations is the CRITICAL guard: a pydantic
// model is only constructible at runtime when every type its fields reference is
// importable in the module namespace at class-definition time. So the generated
// files MUST NOT carry `from __future__ import annotations` (which would defer
// the annotations to lazy strings and raise `PydanticUserError: <Model> is not
// fully defined`), and the stdlib type modules (datetime/decimal/uuid/
// collections.abc) MUST be RUNTIME imports — never under `if typing.TYPE_CHECKING:`.
// This pins the byte shape the reference relies on (its committed models.py imports
// datetime/decimal at module top level, no future-annotations, no TYPE_CHECKING
// block).
func TestSQLAlchemyPydanticEmitsRuntimeAnnotations(t *testing.T) {
	queries := []*plugin.Query{
		{
			Name:     "GetLedger",
			Cmd:      metadata.CmdOne,
			Filename: "queries.sql",
			Text:     "SELECT id, amount, created_at FROM ledger WHERE id = $1",
			Columns: []*plugin.Column{
				{Name: "id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("ledger")},
				{Name: "amount", NotNull: true, Type: &plugin.Identifier{Name: "numeric"}, Table: ident("ledger")},
				{Name: "created_at", NotNull: true, Type: &plugin.Identifier{Name: "timestamptz"}, Table: ident("ledger")},
			},
			Params: []*plugin.Parameter{
				{Number: 1, Column: &plugin.Column{Name: "id", NotNull: true, Type: &plugin.Identifier{Name: "bigint"}}},
			},
		},
	}
	files := runSQLAlchemyFiles(t, datetimeDecimalCatalog(), queries)

	models, ok := files["models.py"]
	if !ok {
		t.Fatal("no models.py emitted")
	}
	queriesPy, ok := files["queries.py"]
	if !ok {
		t.Fatal("no queries.py emitted")
	}

	for name, content := range map[string]string{"models.py": models, "queries.py": queriesPy} {
		if strings.Contains(content, "from __future__ import annotations") {
			t.Errorf("%s: pydantic must NOT emit `from __future__ import annotations` (defers annotations to strings → PydanticUserError at construction)", name)
		}
		if strings.Contains(content, "if typing.TYPE_CHECKING:") {
			t.Errorf("%s: pydantic must NOT emit a `typing.TYPE_CHECKING` block — every field-annotation type is a runtime import", name)
		}
	}

	// The stdlib type modules a field references must be RUNTIME top-level imports.
	for _, imp := range []string{"import datetime", "import decimal"} {
		if !strings.Contains(models, "\n"+imp+"\n") {
			t.Errorf("models.py must carry the runtime import line %q (field types datetime/decimal are resolved at runtime by pydantic)", imp)
		}
	}

	// The `__all__` annotation must not reference a not-yet-imported name. It is
	// emitted BEFORE the import block and (without future-annotations) evaluated
	// at runtime, so it uses the builtin `tuple[str, ...]`, never
	// `collections.abc.Sequence[str]` (which would NameError there).
	if !strings.Contains(models, "__all__: tuple[str, ...] = (") {
		t.Error("models.py `__all__` must be annotated `tuple[str, ...]` (import-free, runtime-safe before the import block)")
	}
	if strings.Contains(models, "__all__: collections.abc.Sequence[str]") {
		t.Error("models.py `__all__` must NOT use `collections.abc.Sequence[str]` — it is evaluated before `import collections.abc` and would NameError")
	}
}

// TestSQLAlchemyPydanticOverrideTypeIsRuntimeImport guards the C-#19 jsonb-typed
// case: a pydantic model FIELD whose type is a per-column `py_type` override
// (e.g. a jsonb column mapped to a hand-written model) is a runtime-evaluated
// annotation just like a stdlib field type. So the override's import MUST land
// in the runtime block, NOT under `if typing.TYPE_CHECKING:` — otherwise ruff
// flags TC004 ("import used for more than type hinting") and pydantic raises
// `<Model> is not fully defined` at construction (no future-annotations under
// pydantic). A non-field override (one only used as a query return type, never a
// model field) is out of scope here; this pins the MODEL-FIELD case.
func TestSQLAlchemyPydanticOverrideTypeIsRuntimeImport(t *testing.T) {
	// widget.config is a NOT NULL jsonb column mapped to shapes.WidgetConfig by a
	// per-column override; it surfaces as a `config: WidgetConfig` model field.
	cols := []*plugin.Column{
		{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "integer"}, Table: ident("widget")},
		{Name: "config", NotNull: true, Type: &plugin.Identifier{Name: "jsonb"}, Table: ident("widget")},
	}
	catalog := &plugin.Catalog{
		DefaultSchema: "public",
		Schemas: []*plugin.Schema{{
			Name:   "public",
			Tables: []*plugin.Table{{Rel: ident("widget"), Columns: cols}},
		}},
	}
	req := &plugin.GenerateRequest{
		Catalog:  catalog,
		Queries:  []*plugin.Query{},
		Settings: &plugin.Settings{Engine: "postgresql"},
		PluginOptions: []byte(`{
			"package": "m",
			"sql_driver": "sqlalchemy",
			"model_type": "pydantic",
			"emit_classes": true,
			"emit_init_file": false,
			"overrides": [
				{"column": "widget.config", "py_type": {"import": "shapes", "type": "WidgetConfig"}}
			]
		}`),
	}
	resp, err := Generate(context.Background(), req)
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
	if strings.Contains(models, "if typing.TYPE_CHECKING:") {
		t.Errorf("models.py: the override field type must be a runtime import, NOT under TYPE_CHECKING (pydantic resolves field annotations at runtime):\n%s", models)
	}
	if !strings.Contains(models, "\nfrom shapes import WidgetConfig\n") &&
		!strings.Contains(models, "\nimport shapes\n") {
		t.Errorf("models.py must import the override type at module top level (runtime):\n%s", models)
	}
}

// TestSQLAlchemyJsonbOverrideBindIsNotConstructorWrapped guards the
// better-python #161 bind landmine: the shared override bind path wraps a write
// param in its base-type constructor `DefaultType(value)`. Once jsonb's default
// became `typing.Any`, an overridden jsonb WRITE column rendered as
// `typing.Any(config)` — invalid Python at runtime (`typing.Any` is not
// callable). The SQLAlchemy driver must bind such an override value DIRECTLY
// (the dialect adapts the object to the column type); a non-callable
// `typing.Any(...)` wrapper must never be emitted.
func TestSQLAlchemyJsonbOverrideBindIsNotConstructorWrapped(t *testing.T) {
	cols := []*plugin.Column{
		{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "integer"}, Table: ident("widget")},
		{Name: "config", NotNull: true, Type: &plugin.Identifier{Name: "jsonb"}, Table: ident("widget")},
	}
	catalog := &plugin.Catalog{
		DefaultSchema: "public",
		Schemas: []*plugin.Schema{{
			Name:   "public",
			Tables: []*plugin.Table{{Rel: ident("widget"), Columns: cols}},
		}},
	}
	// An :exec INSERT binding the overridden jsonb column as a flat write param.
	queries := []*plugin.Query{{
		Name:     "InsertWidget",
		Cmd:      metadata.CmdExec,
		Filename: "queries.sql",
		Text:     "INSERT INTO widget (widget_id, config) VALUES ($1, $2)",
		Params: []*plugin.Parameter{
			{Number: 1, Column: &plugin.Column{Name: "widget_id", NotNull: true, Type: &plugin.Identifier{Name: "integer"}}},
			{Number: 2, Column: &plugin.Column{Name: "config", NotNull: true, Type: &plugin.Identifier{Name: "jsonb"}, Table: ident("widget")}},
		},
	}}
	req := &plugin.GenerateRequest{
		Catalog:  catalog,
		Queries:  queries,
		Settings: &plugin.Settings{Engine: "postgresql"},
		PluginOptions: []byte(`{
			"package": "m",
			"sql_driver": "sqlalchemy",
			"model_type": "pydantic",
			"emit_classes": true,
			"emit_init_file": false,
			"overrides": [
				{"column": "widget.config", "py_type": {"import": "shapes", "type": "WidgetConfig"}}
			]
		}`),
	}
	resp, err := Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var queriesPy string
	for _, f := range resp.Files {
		if f.Name == "queries.py" {
			queriesPy = string(f.Contents)
		}
	}
	if queriesPy == "" {
		t.Fatal("no queries.py emitted")
	}
	if strings.Contains(queriesPy, "typing.Any(") {
		t.Errorf("queries.py emits a non-callable `typing.Any(...)` bind cast (#161 landmine); the override value must bind directly:\n%s", queriesPy)
	}
	// The override value must bind directly under its placeholder, keyed on the
	// param name the column carries (`config`), with no constructor wrap. The
	// expected literal is derived from the param name so this stays in lockstep
	// with the bind the generator emits from that column.
	const paramName = "config"
	wantBind := `"p2": ` + paramName
	if !strings.Contains(queriesPy, wantBind) {
		t.Errorf("queries.py must bind the overridden jsonb write param directly as %q:\n%s", wantBind, queriesPy)
	}
}

// TestSQLAlchemyPydanticDropsUnusedStdlibImports guards the F401/unused-import
// half of the CRITICAL fix: when a pydantic file references neither
// `collections.abc.` (no list field / SELECT :many / Sequence) nor `typing.`
// (no jsonb `typing.Any` / reserved-kw `typing.Annotated`), the unconditional
// stdImports `collections`/`typing` entries must be PRUNED rather than left as a
// dead runtime import or a stale TYPE_CHECKING block.
func TestSQLAlchemyPydanticDropsUnusedStdlibImports(t *testing.T) {
	files := runSQLAlchemyFiles(t, fileAttachmentCatalog(), []*plugin.Query{
		{
			Name:     "GetFileAttachment",
			Cmd:      metadata.CmdOne,
			Filename: "queries.sql",
			Text:     "SELECT file_attachment_id, upload_id FROM file_attachment WHERE upload_id = $1",
			Columns: []*plugin.Column{
				{Name: "file_attachment_id", NotNull: false, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("file_attachment")},
				{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
			},
			Params: []*plugin.Parameter{
				{Number: 1, Column: &plugin.Column{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
			},
		},
	})

	models := files["models.py"]
	// file_attachment here has no list / jsonb / reserved-kw column, so models.py
	// references neither typing nor collections — both must be absent.
	if strings.Contains(models, "import collections") {
		t.Error("models.py must not carry an unused `import collections` (no list field)")
	}
	if strings.Contains(models, "import typing") {
		t.Error("models.py must not carry an unused `import typing` (no jsonb/reserved-kw field)")
	}

	// queries.py always references `typing.Final` for its query constant, so
	// `import typing` MUST be present there; with only a scalar `:one` and no
	// list/many it references no `collections.abc`, so that import must be absent.
	queriesPy := files["queries.py"]
	if !strings.Contains(queriesPy, "\nimport typing\n") {
		t.Error("queries.py must carry runtime `import typing` (query constants are `typing.Final[str]`)")
	}
	if strings.Contains(queriesPy, "import collections") {
		t.Error("queries.py must not carry an unused `import collections` (scalar :one, no AsyncIterator/Sequence)")
	}
}
