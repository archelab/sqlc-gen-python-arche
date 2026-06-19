package core

import "testing"

// TestIsPythonKeyword pins the P-14 escape predicate: true keywords escape,
// `id` does NOT (it is a valid identifier / builtin and the generator emits raw `id`).
func TestIsPythonKeyword(t *testing.T) {
	keywords := []string{"class", "from", "import", "def", "return", "in", "is", "and", "or", "not", "lambda", "yield", "async", "await", "None", "True", "False"}
	for _, kw := range keywords {
		if !IsPythonKeyword(kw) {
			t.Errorf("%q must be a python keyword", kw)
		}
	}
	notKeywords := []string{"id", "type", "name", "label", "status", "upload_id", "class_", "from_"}
	for _, s := range notKeywords {
		if IsPythonKeyword(s) {
			t.Errorf("%q must NOT be a python keyword (id/builtins/escaped names are valid identifiers)", s)
		}
	}
}

// TestEscapeFieldName pins the suffix + escaped-bool contract.
func TestEscapeFieldName(t *testing.T) {
	cases := []struct {
		in          string
		wantName    string
		wantEscaped bool
	}{
		{"class", "class_", true},
		{"from", "from_", true},
		{"id", "id", false},
		{"label", "label", false},
	}
	for _, tc := range cases {
		name, escaped := EscapeFieldName(tc.in)
		if name != tc.wantName || escaped != tc.wantEscaped {
			t.Errorf("EscapeFieldName(%q) = (%q, %v), want (%q, %v)", tc.in, name, escaped, tc.wantName, tc.wantEscaped)
		}
	}
}
