package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// runSQLAlchemyWithLimit generates with the SQLAlchemy/pydantic config at a
// given query_parameter_limit and returns the queries.py contents (the single
// non-models output file).
func runSQLAlchemyWithLimit(t *testing.T, catalog *plugin.Catalog, queries []*plugin.Query, opts string) string {
	t.Helper()
	req := &plugin.GenerateRequest{
		Catalog:       catalog,
		Queries:       queries,
		Settings:      &plugin.Settings{Engine: "postgresql"},
		PluginOptions: []byte(opts),
	}
	resp, err := Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, f := range resp.Files {
		if f.Name != "models.py" {
			return string(f.Contents)
		}
	}
	t.Fatal("no query file emitted")
	return ""
}

// TestSQLAlchemyBundleParamsImportsModelBase pins the Critical regression: a
// query file whose ONLY pydantic class is a bundled `<Method>Params` (no Ret
// struct — here a `:exec` with param count above query_parameter_limit) MUST
// import the model base (`import pydantic`). The class statement evaluates
// `pydantic.BaseModel` at definition time, so a missing import is a NameError
// at import (ruff F821 / pyright reportUndefinedVariable), not a deferred
// annotation. Byte-modelled on the reference file_attachment surface, which imports
// pydantic for the inline InsertFileAttachmentParams bundle. The architecture
// invariant: any generated file that references `pydantic.BaseModel` also
// imports pydantic.
func TestSQLAlchemyBundleParamsImportsModelBase(t *testing.T) {
	// A `:exec` (no result columns -> no Ret struct emitted) with three params,
	// generated at query_parameter_limit=2 so the params bundle into a
	// SetThreeParams class. The only pydantic class in the file is the bundle.
	queries := []*plugin.Query{
		{
			Name:     "SetThreeParams",
			Cmd:      metadata.CmdExec,
			Filename: "queries.sql",
			Text:     "UPDATE file_attachment SET upload_id = $1::text, chat_id = $2::text, extraction_id = $3::text WHERE file_attachment_id = 1",
			Params: []*plugin.Parameter{
				{Number: 1, Column: &plugin.Column{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
				{Number: 2, Column: &plugin.Column{Name: "chat_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
				{Number: 3, Column: &plugin.Column{Name: "extraction_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
			},
		},
	}
	out := runSQLAlchemyWithLimit(t, fileAttachmentCatalog(), queries, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"model_type": "pydantic",
		"emit_classes": true,
		"emit_init_file": false,
		"query_parameter_limit": 2
	}`)

	if !strings.Contains(out, "class SetThreeParamsParams(pydantic.BaseModel):") {
		t.Fatalf("expected a bundled SetThreeParamsParams(pydantic.BaseModel) class, got:\n%s", out)
	}
	// The architecture invariant: a file referencing pydantic.BaseModel imports
	// pydantic, or it raises NameError at import.
	if strings.Contains(out, "pydantic.BaseModel") && !strings.Contains(out, "import pydantic") {
		t.Errorf("query file references pydantic.BaseModel but does NOT import pydantic (NameError at import):\n%s", out)
	}
}

// TestSQLAlchemyBundleBindDictIsMultiLine pins the bundle bind-dict layout: a
// bundled Params arg emits the named-bind dict MULTI-LINE (`, {` on the call
// line, one `"pN": arg.field,` per key, a closing `}`), byte-matching the reference
// insert_file_attachment (file_attachment.py) where all 243 bundle
// binds are multi-line. The flat (non-bundle) path stays single-line.
func TestSQLAlchemyBundleBindDictIsMultiLine(t *testing.T) {
	queries := []*plugin.Query{
		{
			Name:     "SetThreeParams",
			Cmd:      metadata.CmdExec,
			Filename: "queries.sql",
			Text:     "UPDATE file_attachment SET upload_id = $1::text, chat_id = $2::text, extraction_id = $3::text WHERE file_attachment_id = 1",
			Params: []*plugin.Parameter{
				{Number: 1, Column: &plugin.Column{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
				{Number: 2, Column: &plugin.Column{Name: "chat_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
				{Number: 3, Column: &plugin.Column{Name: "extraction_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
			},
		},
	}
	out := runSQLAlchemyWithLimit(t, fileAttachmentCatalog(), queries, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"model_type": "pydantic",
		"emit_classes": true,
		"emit_init_file": false,
		"query_parameter_limit": 2
	}`)

	// The bundle dict opens `, {` then breaks the line; each key sits on its own
	// indented line with a trailing comma; the dict closes on its own line.
	wantMultiLine := ", {\n            \"p1\": arg.upload_id,\n            \"p2\": arg.chat_id,\n            \"p3\": arg.extraction_id,\n        }"
	if !strings.Contains(out, wantMultiLine) {
		t.Errorf("bundle bind dict is not multi-line as the reference emits; got:\n%s", out)
	}
	// And it must NOT be single-line (the pre-fix shape).
	if strings.Contains(out, `, {"p1": arg.upload_id`) {
		t.Errorf("bundle bind dict is single-line; the reference emits multi-line:\n%s", out)
	}
}
