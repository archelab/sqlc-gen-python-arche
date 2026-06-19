package core

// IsPythonKeyword reports whether s is a genuine Python keyword that CANNOT be
// used as an identifier (so a column/field named it must be escaped to compile).
//
// This is intentionally a STRICTER set than IsReserved (reserved.go, which is
// auto-generated and also lists `id`). `id` is a builtin function name, NOT a
// keyword: `id = 1` is valid Python, so a column named `id` emits raw as
// `id: int`. The pydantic field-name escape must fire ONLY for true keywords
// (e.g. `class`, `import`, `from`) and leave `id` raw. The param-arg path keeps
// using core.Escape/IsReserved unchanged (byte-neutral for the existing
// drivers). Mirrors Python's keyword.kwlist plus the soft-keyword guards that
// pydantic field names must avoid.
func IsPythonKeyword(s string) bool {
	switch s {
	case "False", "None", "True",
		"and", "as", "assert", "async", "await",
		"break", "class", "continue", "def", "del",
		"elif", "else", "except", "finally", "for",
		"from", "global", "if", "import", "in",
		"is", "lambda", "nonlocal", "not", "or",
		"pass", "raise", "return", "try", "while",
		"with", "yield":
		return true
	default:
		return false
	}
}

// EscapeFieldName returns the Python-safe field name for a pydantic model/struct
// field: a true keyword gets a trailing underscore (`class` -> `class_`),
// everything else (including `id`) is returned unchanged. The bool reports
// whether escaping occurred, which the emitter uses to decide whether to attach
// `pydantic.Field(alias=...)` + `populate_by_name`.
func EscapeFieldName(s string) (string, bool) {
	if IsPythonKeyword(s) {
		return s + "_", true
	}
	return s, false
}
