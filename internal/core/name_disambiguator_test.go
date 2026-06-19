package core

import "testing"

// TestNameDisambiguatorSuffixes pins the P-16 de-collision rule that is now
// single-sourced and shared between columnsToStruct and the flat multi-param
// arg loop: a repeated base name gets `_<n>` suffixes; a named param is never
// suffixed; same-id (same placeholder) reuses the first chosen suffix.
func TestNameDisambiguatorSuffixes(t *testing.T) {
	d := NewNameDisambiguator()
	// Two distinct ids sharing base "status" -> status, status_2.
	if got := d.Next("status", 0, 1, false, false); got != "status" {
		t.Fatalf("first = %q, want status", got)
	}
	if got := d.Next("status", 1, 2, false, false); got != "status_2" {
		t.Fatalf("second = %q, want status_2", got)
	}
	if got := d.Next("status", 2, 3, false, false); got != "status_3" {
		t.Fatalf("third = %q, want status_3", got)
	}
	// A distinct base is untouched.
	if got := d.Next("other", 3, 4, false, false); got != "other" {
		t.Fatalf("distinct base = %q, want other", got)
	}
}

// A named param is never suffixed even when its base repeats.
func TestNameDisambiguatorNamedParamNeverSuffixed(t *testing.T) {
	d := NewNameDisambiguator()
	_ = d.Next("x", 0, 1, false, false)
	if got := d.Next("x", 1, 2, true, false); got != "x" {
		t.Fatalf("named param = %q, want x (never suffixed)", got)
	}
}

// useID=true: two entries for the same placeholder id reuse the first suffix
// (mirrors columnsToStruct's `suffixes[c.id]` reuse for the same numbered
// parameter).
func TestNameDisambiguatorSameIDReusesSuffix(t *testing.T) {
	d := NewNameDisambiguator()
	first := d.Next("v", 0, 7, false, true)
	second := d.Next("v", 1, 7, false, true)
	if first != second {
		t.Fatalf("same id must reuse suffix: %q vs %q", first, second)
	}
}
