package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// runDriverQueryFile generates with an arbitrary sql_driver/model_type and
// returns the single non-models output file (queries.py). It is the shared
// boundary helper for asserting per-driver emission heterogeneity off ONE
// catalog + ONE query set.
func runDriverQueryFile(t *testing.T, catalog *plugin.Catalog, queries []*plugin.Query, opts string) string {
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

// TestManyIdiomHeterogeneityIsIntentional is the F3-dissent architecture test
// (ruling F3, "Dissent (preserve)"): the published fork deliberately carries TWO
// distinct `:many` idioms forever — asyncpg's EAGER buffered `QueryResults`
// wrapper and SQLAlchemy's native async generator — and that heterogeneity is an
// intentional design property, not an accident, so it must be PINNED as a
// failing-on-regression assertion rather than left implicit in the (large,
// noisy) golden trees.
//
// Driving the SAME catalog + SAME `:many` query through BOTH drivers, this test
// asserts, side by side:
//
//   - asyncpg `:many` → the buffered `QueryResults[T]` wrapper: a synchronous
//     `def` method returning `-> QueryResults[...]` and constructing
//     `QueryResults[...](...)`. (The wrapper's `__await__` is an eager fetch.)
//   - SQLAlchemy `:many` → a native async generator: `async def ... ->
//     collections.abc.AsyncIterator[...]` that `stream`s and `async for ...
//     yield`s, and emits NO `QueryResults` class, return type, or constructor.
//
// A regression that collapsed the two idioms onto one base — e.g. routing
// asyncpg through the no-op `driverBuildQueryResults`, or making SQLAlchemy reuse
// the positional QueryResults builder (the F3 "must NOT" clause) — fails here
// directly, at the Go boundary, independent of any byte-golden drift.
func TestManyIdiomHeterogeneityIsIntentional(t *testing.T) {
	manyQuery := []*plugin.Query{
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

	// --- asyncpg side: the EAGER buffered QueryResults idiom must be present. ---
	asyncpg := runDriverQueryFile(t, fileAttachmentCatalog(), manyQuery, `{
		"package": "m",
		"sql_driver": "asyncpg",
		"model_type": "dataclass",
		"emit_classes": true,
		"emit_init_file": false
	}`)
	if !strings.Contains(asyncpg, "class QueryResults(typing.Generic[T]):") {
		t.Error("asyncpg :many must keep the buffered `QueryResults` wrapper class (F3 heterogeneity)")
	}
	if !strings.Contains(asyncpg, "-> QueryResults[str]:") {
		t.Error("asyncpg :many method must return `QueryResults[str]` (the buffered idiom)")
	}
	if !strings.Contains(asyncpg, "return QueryResults[str](") {
		t.Error("asyncpg :many must construct and return a `QueryResults[str](...)` wrapper")
	}
	if strings.Contains(asyncpg, "async def list_upload_ids(") {
		t.Error("asyncpg :many must be a synchronous `def` returning the wrapper, NOT a native `async def` generator")
	}

	// --- SQLAlchemy side: the native async-generator idiom, no QueryResults. ---
	sqlalchemy := runDriverQueryFile(t, fileAttachmentCatalog(), manyQuery, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"model_type": "pydantic",
		"emit_classes": true,
		"emit_init_file": false
	}`)
	if !strings.Contains(sqlalchemy, "async def list_upload_ids(") {
		t.Error("SQLAlchemy :many must be a native `async def` generator")
	}
	if !strings.Contains(sqlalchemy, "-> collections.abc.AsyncIterator[str]:") {
		t.Error("SQLAlchemy :many must return `collections.abc.AsyncIterator[str]` (the native idiom)")
	}
	if strings.Contains(sqlalchemy, "QueryResults") {
		t.Error("SQLAlchemy :many must NOT reuse the asyncpg QueryResults wrapper anywhere (F3 `must NOT` clause)")
	}

	// --- The heterogeneity itself: the two idioms are genuinely DIFFERENT. ---
	// asyncpg buffers behind QueryResults; SQLAlchemy streams a bare
	// AsyncIterator. If a regression converged them, one of these would flip.
	asyncpgBuffers := strings.Contains(asyncpg, "QueryResults") &&
		!strings.Contains(asyncpg, "-> collections.abc.AsyncIterator[str]:")
	sqlalchemyStreams := !strings.Contains(sqlalchemy, "QueryResults") &&
		strings.Contains(sqlalchemy, "-> collections.abc.AsyncIterator[str]:")
	if !(asyncpgBuffers && sqlalchemyStreams) {
		t.Errorf("the two :many idioms must stay distinct (asyncpg buffers=%v, sqlalchemy streams=%v)", asyncpgBuffers, sqlalchemyStreams)
	}
}
