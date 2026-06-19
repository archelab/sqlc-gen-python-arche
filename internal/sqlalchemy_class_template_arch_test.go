package internal

import (
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// classTemplateQueries is one trivial :one query — enough to force the
// AsyncQuerier class to be emitted so its header/__init__ shape can be pinned.
func classTemplateQueries() []*plugin.Query {
	return []*plugin.Query{
		{
			Name:     "GetFileAttachment",
			Cmd:      metadata.CmdOne,
			Filename: "queries.sql",
			Text:     "SELECT file_attachment_id, upload_id FROM file_attachment WHERE upload_id = $1::text",
			Columns: []*plugin.Column{
				{Name: "file_attachment_id", NotNull: false, Type: &plugin.Identifier{Name: "bigint"}, Table: ident("file_attachment")},
				{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}, Table: ident("file_attachment")},
			},
			Params: []*plugin.Parameter{
				{Number: 1, Column: &plugin.Column{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
			},
		},
	}
}

// assertSQLAlchemyClassShape checks the FOUR F6 class-template deltas the
// SQLAlchemy driver owns vs the extracted default body, plus the minor-2
// class/init/conn docstring suppression. `out` is the generated queries.py.
func assertSQLAlchemyClassShape(t *testing.T, out string) {
	t.Helper()

	// Delta 1 — stable literal class name `AsyncQuerier`, NOT the default
	// SnakeToCamel(sourceName). The source file is queries.sql, so the default
	// would emit `class Queries:`.
	if !strings.Contains(out, "class AsyncQuerier:") {
		t.Error("SQLAlchemy class name must be the stable literal `class AsyncQuerier:`")
	}
	if strings.Contains(out, "class Queries:") {
		t.Error("SQLAlchemy driver must NOT emit the default SnakeToCamel(sourceName) class name")
	}

	// Delta 2 — no __slots__.
	if strings.Contains(out, "__slots__") {
		t.Error("SQLAlchemy class must NOT emit __slots__")
	}

	// Delta 3 — __init__ takes the AsyncConnection with NO `-> None` return
	// annotation and NO @property/conn accessor.
	wantInit := "def __init__(self, conn: sqlalchemy.ext.asyncio.AsyncConnection):"
	if !strings.Contains(out, wantInit) {
		t.Errorf("SQLAlchemy __init__ must be exactly %q (plain colon, no `-> None`)", wantInit)
	}
	if strings.Contains(out, "def __init__(self, conn: sqlalchemy.ext.asyncio.AsyncConnection) -> None:") {
		t.Error("SQLAlchemy __init__ must NOT carry a `-> None` return annotation")
	}
	if strings.Contains(out, "@property") {
		t.Error("SQLAlchemy class must NOT emit a @property accessor")
	}

	// Delta 4 — plain `self._conn = conn` attribute assignment.
	if !strings.Contains(out, "        self._conn = conn") {
		t.Error("SQLAlchemy __init__ must assign plain `self._conn = conn`")
	}

	// minor-2 — the SQLAlchemy class body must carry NO class/init/conn
	// docstring. We assert that the two lines forming the class header
	// (`class AsyncQuerier:` then `def __init__`) are immediately adjacent — a
	// class docstring would insert a `"""` line between them — and likewise that
	// `self._conn = conn` immediately follows the __init__ signature with no
	// intervening init/conn docstring.
	classHeader := "class AsyncQuerier:\n    def __init__(self, conn: sqlalchemy.ext.asyncio.AsyncConnection):\n        self._conn = conn\n"
	if !strings.Contains(out, classHeader) {
		t.Errorf("SQLAlchemy class header must be docstring-free and exactly:\n%q\ngot:\n%s", classHeader, out)
	}
}

// TestSQLAlchemyClassTemplateFourDeltas pins F6: the SQLAlchemy buildClassTemplate
// hook supplies a class body with all four deltas vs the extracted shared
// default (stable AsyncQuerier name, no __slots__, no `-> None`, plain
// self._conn, no @property), with `docstrings` UNSET (the default early-returns
// the class/init/conn docstring writes).
func TestSQLAlchemyClassTemplateFourDeltas(t *testing.T) {
	out := runSQLAlchemy(t, fileAttachmentCatalog(), classTemplateQueries())
	assertSQLAlchemyClassShape(t, out)
}

// TestSQLAlchemyClassTemplateSuppressesDocstringsWhenSet pins minor-2: even when
// `docstrings` IS set to a convention, the SQLAlchemy class/init/conn docstrings
// stay suppressed. The default buildClassTemplate would emit three docstring
// writes (WriteQueryClassDocstring/InitDocstring/ConnDocstring); the SQLAlchemy
// override emits none regardless of the knob. The verb method's own
// function-level docstring is a separate, knob-driven concern and is NOT
// asserted here.
func TestSQLAlchemyClassTemplateSuppressesDocstringsWhenSet(t *testing.T) {
	out := runSQLAlchemyWithLimit(t, fileAttachmentCatalog(), classTemplateQueries(), `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"model_type": "pydantic",
		"emit_classes": true,
		"emit_init_file": false,
		"docstrings": "google"
	}`)
	assertSQLAlchemyClassShape(t, out)
}

// TestDefaultClassTemplateUnchangedByExtraction is the F6 byte-neutrality proof
// for the THREE existing drivers at the Go level: extracting buildClassTemplate
// into a Driver hook must leave the DEFAULT body (used by asyncpg/aiosqlite/
// sqlite3) byte-identical to the old inline body. Whole-tree byte-neutrality is
// proven authoritatively by the asyncpg_check/aiosqlite_check/sqlite3_check
// golden `sqlc diff` sessions; this is the fast Go-level guard pinning that the
// default class template still emits the four shape markers the SQLAlchemy
// override deliberately drops — so the default and the override can never
// silently converge. asyncpg is the representative default (all three share the
// same shared buildClassTemplate default).
func TestDefaultClassTemplateUnchangedByExtraction(t *testing.T) {
	out := runSQLAlchemyWithLimit(t, fileAttachmentCatalog(), classTemplateQueries(), `{
		"package": "m",
		"sql_driver": "asyncpg",
		"model_type": "dataclass",
		"emit_classes": true,
		"emit_init_file": false
	}`)

	// The default body keeps SnakeToCamel(sourceName) — source file queries.sql
	// -> `class Queries:` — NOT the SQLAlchemy stable literal.
	if !strings.Contains(out, "class Queries:") {
		t.Error("default class template must keep SnakeToCamel(sourceName) -> `class Queries:`")
	}
	if strings.Contains(out, "class AsyncQuerier:") {
		t.Error("default class template must NOT emit the SQLAlchemy `AsyncQuerier` literal")
	}
	// The default body keeps __slots__, the `-> None` __init__ annotation, and a
	// `conn` @property accessor — the exact three markers the SQLAlchemy override
	// drops (delta 2/3/4). If the extraction had perturbed the default, one of
	// these would be missing.
	if !strings.Contains(out, `__slots__ = ("_conn",)`) {
		t.Error("default class template must keep __slots__")
	}
	if !strings.Contains(out, ") -> None:") {
		t.Error("default class template __init__ must keep the `-> None` return annotation")
	}
	if !strings.Contains(out, "@property") {
		t.Error("default class template must keep the `conn` @property accessor")
	}
}
