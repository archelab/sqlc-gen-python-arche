package core

import "testing"

func TestPluginVersion(t *testing.T) {
	const want = "v0.5.3"
	if PluginVersion != want {
		t.Fatalf("PluginVersion = %q, want %q", PluginVersion, want)
	}
}

// FileHeader is a typed *string config field (the per-codegen file-header
// knob, decision F5). nil means "no header"; a value means prepend it.
func TestFileHeaderIsTypedOptionalField(t *testing.T) {
	var c Config
	if c.FileHeader != nil {
		t.Fatalf("FileHeader must default to nil, got %v", c.FileHeader)
	}
	hdr := "# pyright: basic"
	c.FileHeader = &hdr
	if c.FileHeader == nil || *c.FileHeader != hdr {
		t.Fatalf("FileHeader did not round-trip: %v", c.FileHeader)
	}
}
