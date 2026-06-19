package core

import (
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

func parseOpts(t *testing.T, opts string) (*Config, error) {
	t.Helper()
	return ParseConfig(&plugin.GenerateRequest{PluginOptions: []byte(opts)})
}

// A clean fork-vocabulary options block must parse, including the inherited
// omitempty *T pointer fields (emit_init_file / query_parameter_limit /
// initialisms) that DisallowUnknownFields must NOT trip over.
func TestParseConfigCleanForkBlockParses(t *testing.T) {
	opts := `{
		"package": "models",
		"sql_driver": "sqlalchemy",
		"model_type": "pydantic",
		"emit_classes": true,
		"emit_init_file": false,
		"query_parameter_limit": 1,
		"initialisms": ["id"],
		"file_header": "# pyright: basic"
	}`
	conf, err := parseOpts(t, opts)
	if err != nil {
		t.Fatalf("clean fork block must parse, got: %v", err)
	}
	if conf.SqlDriver != SQLDriverSQLAlchemy {
		t.Fatalf("sql_driver did not parse: %v", conf.SqlDriver)
	}
	if conf.ModelType != ModelTypePydantic {
		t.Fatalf("model_type did not parse: %v", conf.ModelType)
	}
	if conf.EmitInitFile == nil || *conf.EmitInitFile != false {
		t.Fatalf("emit_init_file (*bool) did not parse: %v", conf.EmitInitFile)
	}
	if conf.QueryParameterLimit == nil || *conf.QueryParameterLimit != 1 {
		t.Fatalf("query_parameter_limit (*int32) did not parse: %v", conf.QueryParameterLimit)
	}
	if conf.FileHeader == nil || *conf.FileHeader != "# pyright: basic" {
		t.Fatalf("file_header did not parse: %v", conf.FileHeader)
	}
}

func TestParseConfigRejectsLegacyKnob(t *testing.T) {
	opts := `{"package": "models", "sql_driver": "sqlalchemy", "emit_async_querier": true}`
	_, err := parseOpts(t, opts)
	if err == nil {
		t.Fatal("a stray legacy knob must be rejected, not silently dropped")
	}
	if !strings.Contains(err.Error(), "emit_async_querier") {
		t.Fatalf("error must name the offending key, got %q", err.Error())
	}
}

func TestParseConfigRejectsBogusKnob(t *testing.T) {
	opts := `{"package": "models", "sql_driver": "asyncpg", "bogus_knob": 7}`
	_, err := parseOpts(t, opts)
	if err == nil {
		t.Fatal("an unknown key must be rejected")
	}
	if !strings.Contains(err.Error(), "bogus_knob") {
		t.Fatalf("error must name the offending key, got %q", err.Error())
	}
}

// The internal computed field `Async` (config.go) is NOT part of the closed
// option vocabulary. With `json:"-"` it must be rejected like any other unknown
// key — both the Go field name `Async` and its case-insensitive `async` form —
// so the unknown-key guard is exhaustive over internal fields too, not just over
// absent legacy knobs. Without the tag, encoding/json would silently accept it.
func TestParseConfigRejectsInternalAsyncField(t *testing.T) {
	for _, key := range []string{"Async", "async"} {
		opts := `{"package": "models", "sql_driver": "asyncpg", "emit_init_file": false, "` + key + `": true}`
		_, err := parseOpts(t, opts)
		if err == nil {
			t.Fatalf("internal field key %q must be rejected, not silently accepted", key)
		}
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("error must name the offending key %q, got %q", key, err.Error())
		}
	}
}

// Nullability resolution is first-match, so two specs with the identical
// `column` would resolve silently by array order. Parse must reject the exact
// duplicate, naming the column, rather than let a reorder flip nullability.
func TestParseConfigRejectsDuplicateNullabilityOverride(t *testing.T) {
	opts := `{
		"package": "models",
		"sql_driver": "sqlalchemy",
		"model_type": "pydantic",
		"emit_init_file": false,
		"nullability_overrides": [
			{"column": "widget.amount", "nullable": true},
			{"column": "widget.amount", "nullable": false}
		]
	}`
	_, err := parseOpts(t, opts)
	if err == nil {
		t.Fatal("a duplicate nullability override column must be rejected")
	}
	if !strings.Contains(err.Error(), "widget.amount") {
		t.Fatalf("error must name the duplicate column, got %q", err.Error())
	}
}
