package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// ident builds a plugin.Identifier for the default `public` schema.
func ident(name string) *plugin.Identifier {
	return &plugin.Identifier{Schema: "public", Name: name}
}

// fileAttachmentCatalog mirrors the reference file_attachment surface (the C-#12
// byte reference) as a sqlc plugin catalog.
func fileAttachmentCatalog() *plugin.Catalog {
	cols := []*plugin.Column{
		{Name: "file_attachment_id", NotNull: false, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("file_attachment")},
		{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
	}
	return &plugin.Catalog{
		DefaultSchema: "public",
		Schemas: []*plugin.Schema{{
			Name:   "public",
			Tables: []*plugin.Table{{Rel: ident("file_attachment"), Columns: cols}},
		}},
	}
}

// runSQLAlchemy generates with the SQLAlchemy/pydantic config and returns the
// queries.py contents (the single non-models output file).
func runSQLAlchemy(t *testing.T, catalog *plugin.Catalog, queries []*plugin.Query) string {
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
	for _, f := range resp.Files {
		if f.Name != "models.py" {
			return string(f.Contents)
		}
	}
	t.Fatal("no query file emitted")
	return ""
}

// TestSQLAlchemyManyIsNativeAsyncGenerator is the F3 architecture test (ruling
// A). The SQLAlchemy :many SELECT is a native async generator; the driver must
// NOT leak the asyncpg QueryResults wrapper onto its published surface, and the
// driver-agnostic QueryResults/typealias injection sites (queries.go,
// importer.go) must be guarded so they emit nothing for this driver.
func TestSQLAlchemyManyIsNativeAsyncGenerator(t *testing.T) {
	queries := []*plugin.Query{
		{
			Name:     "ListExpiredFileAttachments",
			Cmd:      metadata.CmdMany,
			Filename: "queries.sql",
			Text:     "SELECT file_attachment_id, upload_id FROM file_attachment WHERE expires_at < $1::timestamptz",
			Columns: []*plugin.Column{
				{Name: "file_attachment_id", NotNull: false, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("file_attachment")},
				{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
			},
			Params: []*plugin.Parameter{
				{Number: 1, Column: &plugin.Column{Name: "now", NotNull: true, Type: &plugin.Identifier{Name: "timestamptz"}}},
			},
		},
		{
			Name:     "ListUploadIds",
			Cmd:      metadata.CmdMany,
			Filename: "queries.sql",
			Text:     "SELECT upload_id FROM file_attachment ORDER BY upload_id ASC",
			Columns: []*plugin.Column{
				{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
			},
		},
	}
	out := runSQLAlchemy(t, fileAttachmentCatalog(), queries)

	if strings.Contains(out, "class QueryResults") {
		t.Error("SQLAlchemy driver must NOT emit a QueryResults class")
	}
	if strings.Contains(out, "QueryResultsArgsType") {
		t.Error("SQLAlchemy driver must NOT emit a QueryResultsArgsType typealias")
	}
	// No stray empty entry in __all__ (the F3 driver-agnostic injection guard).
	if strings.Contains(out, `    "",`) {
		t.Error(`__all__ must not contain a stray "" entry`)
	}
	if strings.Contains(out, `"QueryResults"`) {
		t.Error(`__all__ must not list "QueryResults"`)
	}

	// Every :many SELECT method is a native async generator.
	for _, fn := range []string{"list_expired_file_attachments", "list_upload_ids"} {
		needle := "async def " + fn + "("
		i := strings.Index(out, needle)
		if i < 0 {
			t.Fatalf("missing method %q", fn)
		}
		// The method signature line must return collections.abc.AsyncIterator[.
		line := out[i:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, "-> collections.abc.AsyncIterator[") {
			t.Errorf("%s must return collections.abc.AsyncIterator[...], got line: %q", fn, line)
		}
	}
	// The streaming body uses conn.stream + async for/yield, never .execute or
	// .first().
	if !strings.Contains(out, "self._conn.stream(sqlalchemy.text(LIST_EXPIRED_FILE_ATTACHMENTS)") {
		t.Error(":many must use self._conn.stream(...)")
	}
	if !strings.Contains(out, "async for row in result:") {
		t.Error(":many must iterate with `async for row in result:`")
	}
	if !strings.Contains(out, "yield models.FileAttachment(") {
		t.Error("struct :many must yield models.X(...)")
	}
	if !strings.Contains(out, "yield row[0]") {
		t.Error("scalar :many must yield row[0]")
	}
}
