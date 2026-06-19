package core

import "github.com/sqlc-dev/plugin-sdk-go/plugin"

type Column struct {
	Name    string // CamelCased name for Go
	DBName  string // Name as used in the DB
	Type    PyType
	Comment string
	Column  *plugin.Column
	// Number is the SQL placeholder number ($N) this field binds to when the
	// column is a bundled Params struct field (the SQLAlchemy named-bind dict
	// keys on `pN`). Zero for a table/result-row column, which is never used as
	// a bind target.
	Number int32
	// EmbedFields contains the embedded fields that require scanning.
	EmbedFields []Column
}
