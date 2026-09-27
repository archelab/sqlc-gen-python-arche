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
	// An unmarked :many SELECT buffers: one conn.execute round trip, then a
	// plain `for` over the buffered result inside the generator. No
	// server-side cursor (conn.stream), no `async for`, never .first().
	for _, c := range []string{"LIST_EXPIRED_FILE_ATTACHMENTS", "LIST_UPLOAD_IDS"} {
		if !strings.Contains(out, "result = await self._conn.execute(sqlalchemy.text("+c+")") {
			t.Errorf(":many %s must use self._conn.execute(...)", c)
		}
	}
	if strings.Contains(out, ".stream(") {
		t.Error("an unmarked :many must NOT use conn.stream(...)")
	}
	if strings.Contains(out, "async for row in result:") {
		t.Error("an unmarked :many must NOT iterate with `async for`")
	}
	if !strings.Contains(out, "        for row in result:") {
		t.Error("an unmarked :many must iterate with `for row in result:`")
	}
	// Each row comes from the query's module-level row constructor.
	if !strings.Contains(out, "            yield list_expired_file_attachments_row(row)\n") {
		t.Error("struct :many must yield its row constructor")
	}
	if !strings.Contains(out, "def list_expired_file_attachments_row(row: sqlalchemy.Row[typing.Any]) -> models.FileAttachment:\n    return models.FileAttachment(\n") {
		t.Error("struct row constructor must build models.X(...)")
	}
	if !strings.Contains(out, "def list_upload_ids_row(row: sqlalchemy.Row[typing.Any]) -> str:\n    return row[0]\n") {
		t.Error("scalar row constructor must return row[0]")
	}
}

// methodBody returns the generated text of method fn, from its `async def`
// line up to the next method (or the end of the file).
func methodBody(t *testing.T, out, fn string) string {
	t.Helper()
	i := strings.Index(out, "async def "+fn+"(")
	if i < 0 {
		t.Fatalf("missing method %q", fn)
	}
	body := out[i:]
	if j := strings.Index(body[1:], "async def "); j >= 0 {
		body = body[:j+1]
	}
	// The row constructors follow the last method at module level.
	if j := strings.Index(body, "\ndef "); j >= 0 {
		body = body[:j+1]
	}
	return body
}

// TestSQLAlchemyManyStreamMarkerKeepsStream pins the per-query opt-out. sqlc
// hands the plugin every full-line `--` comment of a query (minus the `--`) in
// Query.Comments and strips it from Query.Text. A comment whose first word is
// `@stream` keeps that :many SELECT on conn.stream + `async for`, for a result
// too large to buffer. `@stream` in the middle of a prose comment is NOT a
// marker, and the unmarked sibling still buffers.
func TestSQLAlchemyManyStreamMarkerKeepsStream(t *testing.T) {
	uploadIDs := func(name string, comments ...string) *plugin.Query {
		return &plugin.Query{
			Name:     name,
			Cmd:      metadata.CmdMany,
			Filename: "queries.sql",
			Text:     "SELECT upload_id FROM file_attachment ORDER BY upload_id ASC",
			Comments: comments,
			Columns: []*plugin.Column{
				{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
			},
		}
	}
	out := runSQLAlchemy(t, fileAttachmentCatalog(), []*plugin.Query{
		uploadIDs("ListAllUploadIds", " @stream reads the whole table"),
		uploadIDs("ListProseUploadIds", " no @stream here, only prose"),
		uploadIDs("ListPlainUploadIds"),
	})

	streamed := methodBody(t, out, "list_all_upload_ids")
	if !strings.Contains(streamed, "result = await self._conn.stream(sqlalchemy.text(LIST_ALL_UPLOAD_IDS))") {
		t.Errorf("a @stream :many must use self._conn.stream(...), got:\n%s", streamed)
	}
	if !strings.Contains(streamed, "async for row in result:") {
		t.Errorf("a @stream :many must iterate with `async for`, got:\n%s", streamed)
	}
	for _, fn := range []string{"list_prose_upload_ids", "list_plain_upload_ids"} {
		buffered := methodBody(t, out, fn)
		if strings.Contains(buffered, ".stream(") || !strings.Contains(buffered, ".execute(") {
			t.Errorf("%s has no @stream marker and must buffer with execute, got:\n%s", fn, buffered)
		}
	}
	if strings.Contains(out, "@stream") {
		t.Error("the @stream marker must not leak into the generated code")
	}
}

// TestSQLAlchemyStreamMarkerNeedsReason: the text after `@stream` is the only
// record of why a query keeps the cursor, so a bare marker fails loudly and
// names the query.
func TestSQLAlchemyStreamMarkerNeedsReason(t *testing.T) {
	for _, comment := range []string{" @stream", " @stream   "} {
		_, err := Generate(context.Background(), &plugin.GenerateRequest{
			Catalog: fileAttachmentCatalog(),
			Queries: []*plugin.Query{{
				Name:     "ListAllUploadIds",
				Cmd:      metadata.CmdMany,
				Filename: "queries.sql",
				Text:     "SELECT upload_id FROM file_attachment",
				Comments: []string{comment},
				Columns: []*plugin.Column{
					{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
				},
			}},
			Settings: &plugin.Settings{Engine: "postgresql"},
			PluginOptions: []byte(`{"package": "m", "sql_driver": "sqlalchemy", "model_type": "pydantic",
				"emit_classes": true, "emit_init_file": false}`),
		})
		if err == nil || !strings.Contains(err.Error(), "ListAllUploadIds") || !strings.Contains(err.Error(), "needs a reason") {
			t.Errorf("comment %q: a @stream marker with no reason must fail and name the query, got err=%v", comment, err)
		}
	}
}

// TestSQLAlchemyStreamMarkerOnlyOnManySelect: the marker is legal only where a
// stream exists. On a :one, or on a :many over DML (Postgres rejects a
// server-side cursor there), the plugin fails loudly instead of ignoring it.
func TestSQLAlchemyStreamMarkerOnlyOnManySelect(t *testing.T) {
	for _, q := range []*plugin.Query{
		{Name: "GetUploadId", Cmd: metadata.CmdOne, Text: "SELECT upload_id FROM file_attachment LIMIT 1"},
		{Name: "DeleteUploadIds", Cmd: metadata.CmdMany, Text: "DELETE FROM file_attachment RETURNING upload_id"},
	} {
		q.Filename = "queries.sql"
		q.Comments = []string{" @stream misplaced"}
		q.Columns = []*plugin.Column{
			{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
		}
		_, err := Generate(context.Background(), &plugin.GenerateRequest{
			Catalog:  fileAttachmentCatalog(),
			Queries:  []*plugin.Query{q},
			Settings: &plugin.Settings{Engine: "postgresql"},
			PluginOptions: []byte(`{"package": "m", "sql_driver": "sqlalchemy", "model_type": "pydantic",
				"emit_classes": true, "emit_init_file": false}`),
		})
		if err == nil || !strings.Contains(err.Error(), "@stream") || !strings.Contains(err.Error(), q.Name) {
			t.Errorf("%s: a misplaced @stream must fail and name the marker and the query, got err=%v", q.Name, err)
		}
	}
}
