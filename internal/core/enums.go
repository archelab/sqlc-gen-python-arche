package core

import "fmt"

type SQLDriverType string

func (dr *SQLDriverType) String() string {
	return string(*dr)
}

const (
	SQLDriverSQLite     SQLDriverType = "sqlite3"
	SQLDriverAioSQLite  SQLDriverType = "aiosqlite"
	SQLDriverAsyncpg    SQLDriverType = "asyncpg"
	SQLDriverSQLAlchemy SQLDriverType = "sqlalchemy"
)

type ModelType string

func (mt *ModelType) String() string {
	return string(*mt)
}

const (
	ModelTypeDataclass ModelType = "dataclass"
	ModelTypeAttrs     ModelType = "attrs"
	ModelTypeMsgspec   ModelType = "msgspec"
	ModelTypePydantic  ModelType = "pydantic"
)

// RuntimeFieldAnnotations reports whether the model type resolves its field
// annotations at class-definition time (rather than lazily as strings). When it
// does, `from __future__ import annotations` must be suppressed and every
// field-type import must be a RUNTIME import — routing such an import under
// `typing.TYPE_CHECKING` raises at construction. pydantic builds its validator
// at definition time and needs this; dataclass/attrs/msgspec do not. This is the
// single source of truth for that rule, consumed at every annotation-suppression
// and import-classification site.
func (mt ModelType) RuntimeFieldAnnotations() bool {
	return mt == ModelTypePydantic
}

var asyncDrivers = map[SQLDriverType]bool{
	SQLDriverSQLite:     false,
	SQLDriverAioSQLite:  true,
	SQLDriverAsyncpg:    true,
	SQLDriverSQLAlchemy: true,
}

var driversEngine = map[SQLDriverType]string{
	SQLDriverSQLite:     "sqlite",
	SQLDriverAioSQLite:  "sqlite",
	SQLDriverAsyncpg:    "postgresql",
	SQLDriverSQLAlchemy: "postgresql",
}

var validModelTypes = map[ModelType]struct{}{
	ModelTypeDataclass: {},
	ModelTypeAttrs:     {},
	ModelTypeMsgspec:   {},
	ModelTypePydantic:  {},
}

const (
	DocstringConventionNone   = "none"
	DocstringConventionGoogle = "google"
	DocstringConventionNumpy  = "numpy"
	DocstringConventionPEP257 = "pep257"
)

var validDocstringConventions = map[string]struct{}{
	DocstringConventionNone:   {},
	DocstringConventionGoogle: {},
	DocstringConventionNumpy:  {},
	DocstringConventionPEP257: {},
}

func isDriverAsync(sqlDriver SQLDriverType) (bool, error) {
	val, found := asyncDrivers[sqlDriver]
	if !found {
		return false, fmt.Errorf("unknown SQL driver: %s", sqlDriver)
	}
	return val, nil
}

func isDriverValid(sqlDriver SQLDriverType, engine string) error {
	val, found := driversEngine[sqlDriver]
	if !found {
		return fmt.Errorf("unknown SQL driver: %s", sqlDriver)
	}
	if val != engine {
		return fmt.Errorf("SQL driver %s does not support %s", sqlDriver, engine)
	}
	return nil
}

func isModelTypeValid(modelType ModelType) error {
	if _, found := validModelTypes[modelType]; !found {
		return fmt.Errorf("unknown model type: %s", modelType)
	}
	return nil
}

func isDocstringValid(ds *string) error {
	if ds == nil {
		return nil
	}
	if _, found := validDocstringConventions[*ds]; !found {
		return fmt.Errorf("unknown docstring convention: %s", *ds)
	}
	return nil
}
