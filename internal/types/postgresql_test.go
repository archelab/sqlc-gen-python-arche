package types

import (
	"testing"

	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

func resolvePg(t *testing.T, sqlType string) string {
	t.Helper()
	req := &plugin.GenerateRequest{
		Catalog: &plugin.Catalog{DefaultSchema: "public"},
	}
	col := &plugin.Column{Type: &plugin.Identifier{Name: sqlType}}
	return PostgresTypeToPython(req, col, &core.Config{})
}

// bytea must resolve to the built-in `bytes`, with no override (P-10). This
// removes the reference inline bytea->bytes override from sqlc.yaml.
func TestByteaDefaultsToBytes(t *testing.T) {
	for _, sqlType := range []string{"bytea", "pg_catalog.bytea", "blob"} {
		if got := resolvePg(t, sqlType); got != "bytes" {
			t.Fatalf("%s resolved to %q, want bytes", sqlType, got)
		}
	}
}
