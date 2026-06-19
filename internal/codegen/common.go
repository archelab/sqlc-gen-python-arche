package codegen

import (
	"fmt"
	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/drivers"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"strings"
)

type TypeBuildPyQueryFunc func(*core.Query, *builders.IndentStringBuilder, []core.FunctionArg, core.PyType, *core.Config) error
type TypeAcceptedDriverCMDs func() []string
type TypeDriverTypeCheckingHook func() []string
type TypeDriverBuildQueryResults func(*builders.IndentStringBuilder) string
type TypeBuildClassTemplate func(sourceName string, connType string, conf *core.Config, body *builders.IndentStringBuilder) string

// TypeRewriteSQL transforms the executable SQL body written into the query
// constant. The default is identity (asyncpg/sqlite keep `$N` placeholders and
// unescaped colons); the SQLAlchemy driver escapes colons and rewrites
// `$N` -> `:pN`. It is dispatched ONLY at the constant site — never at the
// human docstring echo, which keeps canonical `$N`.
type TypeRewriteSQL func(string) string

// TypeRewriteHeaderVerb transforms the `:<verb>` token on the `-- name: <fn>
// :<verb>` header line (a second emission site from the body rewrite). The
// default is identity; the SQLAlchemy driver escapes the verb colon
// `:one` -> `\\:one`.
type TypeRewriteHeaderVerb func(string) string

func defaultDriverTypeCheckingHook() []string {
	return nil
}
func defaultDriverBuildQueryResults(_ *builders.IndentStringBuilder) string {
	return ""
}
func defaultRewriteSQL(s string) string {
	return s
}
func defaultRewriteHeaderVerb(verb string) string {
	return verb
}

// defaultBuildClassTemplate is the shared querier-class body extracted verbatim
// from the old hardcoded buildClassTemplate method (queries.go). It is the
// default for asyncpg/aiosqlite/sqlite3: SnakeToCamel class name, __slots__,
// `def __init__(...) -> None:`, a `conn` @property accessor, and the three
// class/init/conn docstring writes. Drivers that need a different class shape
// (e.g. SQLAlchemy's stable `AsyncQuerier` with no __slots__/property/`-> None`
// and no docstrings) register their own TypeBuildClassTemplate.
func defaultBuildClassTemplate(sourceName string, connType string, conf *core.Config, body *builders.IndentStringBuilder) string {
	className := core.SnakeToCamel(strings.ReplaceAll(sourceName, ".sql", ""), conf)
	body.WriteLine(fmt.Sprintf("class %s:", className))
	body.WriteQueryClassDocstring(sourceName, connType)
	body.WriteIndentedLine(1, `__slots__ = ("_conn",)`)
	body.NewLine()
	body.WriteIndentedLine(1, fmt.Sprintf(`def __init__(self, conn: %s) -> None:`, connType))
	body.WriteQueryClassInitDocstring(2, connType)
	body.WriteIndentedLine(2, "self._conn = conn")
	body.NewLine()
	body.WriteIndentedLine(1, "@property")
	body.WriteIndentedLine(1, fmt.Sprintf(`def conn(self) -> %s:`, connType))
	body.WriteQueryClassConnDocstring(connType)
	body.WriteIndentedLine(2, `return self._conn`)
	body.NewLine()
	return className
}

type Driver struct {
	conf *core.Config

	connType           string
	buildPyQueryFunc   TypeBuildPyQueryFunc
	acceptedDriverCMDs TypeAcceptedDriverCMDs

	driverTypeCheckingHook  TypeDriverTypeCheckingHook
	driverBuildQueryResults TypeDriverBuildQueryResults
	buildClassTemplate      TypeBuildClassTemplate
	rewriteSQL              TypeRewriteSQL
	rewriteHeaderVerb       TypeRewriteHeaderVerb

	//BuildPyQueriesFiles(*core.Importer, []core.Query) ([]*plugin.File, error)
}

func NewDriver(conf *core.Config) (*Driver, error) {
	var buildPyQueryFunc TypeBuildPyQueryFunc
	var acceptedDriverCMDs TypeAcceptedDriverCMDs
	var connType string
	var driverTypeCheckingHook TypeDriverTypeCheckingHook = defaultDriverTypeCheckingHook
	var driverBuildQueryResults TypeDriverBuildQueryResults = defaultDriverBuildQueryResults
	var buildClassTemplate TypeBuildClassTemplate = defaultBuildClassTemplate
	var rewriteSQL TypeRewriteSQL = defaultRewriteSQL
	var rewriteHeaderVerb TypeRewriteHeaderVerb = defaultRewriteHeaderVerb
	switch conf.SqlDriver {
	case core.SQLDriverAioSQLite:
		buildPyQueryFunc = drivers.AioSQLiteBuildPyQueryFunc
		acceptedDriverCMDs = drivers.AioSQLiteAcceptedDriverCMDs
		connType = drivers.AioSQLiteConn
		driverBuildQueryResults = drivers.AiosqliteBuildQueryResults
	case core.SQLDriverSQLite:
		buildPyQueryFunc = drivers.SQLite3BuildPyQueryFunc
		acceptedDriverCMDs = drivers.SQLite3AcceptedDriverCMDs
		connType = drivers.SQLite3Conn
		driverBuildQueryResults = drivers.SQLite3BuildQueryResults
	case core.SQLDriverAsyncpg:
		buildPyQueryFunc = drivers.AsyncpgBuildPyQueryFunc
		acceptedDriverCMDs = drivers.AsyncpgAcceptedDriverCMDs
		connType = drivers.AsyncpgConn
		driverTypeCheckingHook = drivers.AsyncpgTypeCheckingHook
		driverBuildQueryResults = drivers.AsyncpgBuildQueryResults
	case core.SQLDriverSQLAlchemy:
		buildPyQueryFunc = drivers.SQLAlchemyBuildPyQueryFunc
		acceptedDriverCMDs = drivers.SQLAlchemyAcceptedDriverCMDs
		connType = drivers.SQLAlchemyConn
		buildClassTemplate = drivers.SQLAlchemyBuildClassTemplate
		rewriteSQL = drivers.SQLAlchemyRewriteSQL
		rewriteHeaderVerb = drivers.SQLAlchemyRewriteHeaderVerb
	default:
		return nil, fmt.Errorf("unsupported driver: %s", conf.SqlDriver.String())
	}
	builders.SetDocstringConfig(conf.EmitDocstrings, conf.EmitDocstringsSQL, conf.SqlDriver)

	return &Driver{
		buildPyQueryFunc:        buildPyQueryFunc,
		acceptedDriverCMDs:      acceptedDriverCMDs,
		conf:                    conf,
		connType:                connType,
		driverTypeCheckingHook:  driverTypeCheckingHook,
		driverBuildQueryResults: driverBuildQueryResults,
		buildClassTemplate:      buildClassTemplate,
		rewriteSQL:              rewriteSQL,
		rewriteHeaderVerb:       rewriteHeaderVerb,
	}, nil
}

// writeFileHeader writes the file-header literal as absolute line 1 of an
// emitted file, BEFORE the sqlc header. It is a no-op when the file_header knob
// is unset; when set (e.g. `# pyright: basic`) it is prepended to models.py,
// every query file, AND __init__.py. The literal is written verbatim followed
// by a single newline.
func (dr *Driver) writeFileHeader(body *builders.IndentStringBuilder) {
	if dr.conf.FileHeader == nil {
		return
	}
	body.WriteLine(*dr.conf.FileHeader)
}

func (dr *Driver) supportedCMD(command string) error {
	cmds := dr.acceptedDriverCMDs()
	for _, cmd := range cmds {
		if cmd == command {
			return nil
		}
	}
	return fmt.Errorf("unsupported command for selected driver: %s", command)
}
