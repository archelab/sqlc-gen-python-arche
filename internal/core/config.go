package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

const PluginVersion = "v0.5.0"

type Config struct {
	Package                     string                `json:"package" yaml:"package"`
	SqlDriver                   SQLDriverType         `json:"sql_driver" yaml:"sql_driver"`
	ModelType                   ModelType             `json:"model_type" yaml:"model_type"`
	FileHeader                  *string               `json:"file_header,omitempty" yaml:"file_header,omitempty"`
	Initialisms                 *[]string             `json:"initialisms,omitempty" yaml:"initialisms,omitempty"`
	EmitExactTableNames         bool                  `json:"emit_exact_table_names" yaml:"emit_exact_table_names"`
	EmitClasses                 bool                  `json:"emit_classes" yaml:"emit_classes"`
	InflectionExcludeTableNames []string              `json:"inflection_exclude_table_names,omitempty" yaml:"inflection_exclude_table_names,omitempty"`
	OmitUnusedModels            bool                  `json:"omit_unused_models" yaml:"omit_unused_models"`
	OmitTypecheckingBlock       bool                  `json:"omit_typechecking_block" yaml:"omit_typechecking_block"`
	QueryParameterLimit         *int32                `json:"query_parameter_limit,omitempty" yaml:"query_parameter_limit"`
	OmitKwargsLimit             *int32                `json:"omit_kwargs_limit,omitempty" yaml:"omit_kwargs_limit"`
	EmitInitFile                *bool                 `json:"emit_init_file" yaml:"emit_init_file"`
	EmitDocstrings              *string               `json:"docstrings" yaml:"docstrings"`
	EmitDocstringsSQL           *bool                 `json:"docstrings_emit_sql" yaml:"docstrings_emit_sql"`
	Speedups                    bool                  `json:"speedups" yaml:"speedups"`
	Overrides                   []Override            `json:"overrides,omitempty" yaml:"overrides"`
	NullabilityOverrides        []NullabilityOverride `json:"nullability_overrides,omitempty" yaml:"nullability_overrides,omitempty"`

	Debug bool `json:"debug" yaml:"debug"`

	IndentChar          string `json:"indent_char" yaml:"indent_char"`
	CharsPerIndentLevel int    `json:"chars_per_indent_level" yaml:"chars_per_indent_level"`

	InitialismsMap map[string]struct{} `json:"-" yaml:"-"`
	// Async is an internal, computed field (derived from the driver via
	// isDriverAsync in ParseConfig), NOT a user option. It MUST carry
	// `json:"-"` so DisallowUnknownFields does not silently accept a
	// `{"Async":true}`/`{"async":true}` key off the closed option vocabulary
	// (Hyrum's Law: an internal generator field must never reach the config
	// face). Mirrors the adjacent InitialismsMap tag.
	Async bool `json:"-" yaml:"-"`
}

func ParseConfig(req *plugin.GenerateRequest) (*Config, error) {
	var config Config
	if len(req.PluginOptions) == 0 {
		return &config, nil
	}
	// Reject any option key that is not a typed field of Config (fail loud,
	// naming the offending key) instead of json.Unmarshal silently dropping
	// it. This is what makes the legacy->fork knob mapping enforceable: a
	// half-mapped knob cannot silently no-op.
	dec := json.NewDecoder(bytes.NewReader(req.PluginOptions))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		return nil, fmt.Errorf("parsing plugin options: %w", err)
	}
	if config.SqlDriver == "" {
		config.SqlDriver = SQLDriverAioSQLite
	}
	val, err := isDriverAsync(config.SqlDriver)
	if err != nil {
		return nil, fmt.Errorf("invalid options: %s", err)
	}
	config.Async = val

	for i := range config.Overrides {
		if err := config.Overrides[i].parse(req); err != nil {
			return nil, err
		}
	}

	// NullabilityOverrides are validated at this parse seam to mirror the
	// Override convention above (both run inside ParseConfig, before
	// ValidateConf) — NOT alongside the isDriverValid/isModelTypeValid scalar
	// validators in ValidateConf. The family it mirrors is Override.parse().
	seenNullabilitySpec := make(map[string]struct{}, len(config.NullabilityOverrides))
	for i := range config.NullabilityOverrides {
		if err := config.NullabilityOverrides[i].parse(); err != nil {
			return nil, err
		}
		// Resolution is first-match (applyNullabilityOverride returns on the
		// first matching spec), so two specs with the identical `column` would
		// resolve silently by array order — a reorder could flip a field's
		// Optional/non-Optional with no diagnostic. Reject exact duplicates so
		// that footgun fails loud at parse time.
		spec := config.NullabilityOverrides[i].Column
		if _, dup := seenNullabilitySpec[spec]; dup {
			return nil, fmt.Errorf("duplicate nullability override for column %q; specify it once (resolution is first-match)", spec)
		}
		seenNullabilitySpec[spec] = struct{}{}
	}

	if config.ModelType == "" {
		config.ModelType = ModelTypeDataclass
	}
	if config.QueryParameterLimit == nil {
		// Default 4 matches upstream sqlc-gen-python 1.3.0: <=4 params stay
		// keyword-only, >=5 bundle into a <Method>Params struct. (The
		// better-python base defaulted to 1 but kept the param-bundling path
		// commented out, making the value inert.)
		config.QueryParameterLimit = new(int32)
		*config.QueryParameterLimit = 4
	}
	if config.OmitKwargsLimit == nil {
		config.OmitKwargsLimit = new(int32)
		*config.OmitKwargsLimit = 0
	}
	if config.Initialisms == nil {
		config.Initialisms = new([]string)
		*config.Initialisms = []string{"id"}
	}
	if config.IndentChar == "" {
		config.IndentChar = " "
	}
	if config.CharsPerIndentLevel == 0 {
		config.CharsPerIndentLevel = 4
	}
	if config.EmitDocstrings == nil {
		config.EmitDocstrings = new(string)
		*config.EmitDocstrings = DocstringConventionNone
	}
	if config.EmitDocstringsSQL == nil {
		config.EmitDocstringsSQL = new(bool)
		*config.EmitDocstringsSQL = true
	}

	config.InitialismsMap = map[string]struct{}{}
	for _, initial := range *config.Initialisms {
		config.InitialismsMap[initial] = struct{}{}
	}
	return &config, nil
}
func ValidateConf(conf *Config, engine string) error {
	if *conf.QueryParameterLimit < 0 {
		return fmt.Errorf("invalid options: query parameter limit must not be negative")
	}
	if *conf.OmitKwargsLimit < 0 {
		return fmt.Errorf("invalid options: omit kwarg limit must not be negative")
	}

	if conf.EmitInitFile == nil {
		return fmt.Errorf("invalid options: you need to specify emit_init_file")
	}

	if conf.Package == "" {
		return fmt.Errorf("invalid options: package must not be empty")
	}

	if err := isDriverValid(conf.SqlDriver, engine); err != nil {
		return err
	}

	if err := isModelTypeValid(conf.ModelType); err != nil {
		return fmt.Errorf("invalid options: %s", err)
	}

	if err := isDocstringValid(conf.EmitDocstrings); err != nil {
		return fmt.Errorf("invalid options: %s", err)
	}

	if err := validateValidateKnob(conf); err != nil {
		return err
	}

	return nil
}

// validateValidateKnob fails loud on two ways a `validate: true` override would
// silently misbehave: (1) on any driver other than SQLAlchemy the validated read
// path is never emitted, so the knob is a no-op (and would add a dead
// `import pydantic`); (2) two validated overrides resolving the same type NAME
// from different modules would collapse to a single cached `_<name>_adapter`
// built from whichever import wins, silently validating against the wrong shape.
func validateValidateKnob(conf *Config) error {
	seenImport := make(map[string]string)
	for i := range conf.Overrides {
		o := &conf.Overrides[i]
		if !o.Validate {
			continue
		}
		if conf.SqlDriver != SQLDriverSQLAlchemy {
			return fmt.Errorf("invalid options: override `validate: true` (column %q) is only supported with sql_driver: sqlalchemy, not %q", o.Column, conf.SqlDriver)
		}
		if prev, ok := seenImport[o.PyTypeName]; ok && prev != o.PyImportPath {
			return fmt.Errorf("invalid options: validated overrides use the type name %q from two different modules (%q and %q); the cached TypeAdapter is named from the type name, so rename one to disambiguate", o.PyTypeName, prev, o.PyImportPath)
		}
		seenImport[o.PyTypeName] = o.PyImportPath
	}
	return nil
}
