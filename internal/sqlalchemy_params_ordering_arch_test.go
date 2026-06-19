package internal

import (
	"strings"
	"testing"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

// TestSQLAlchemyParamsClassPrecedesConstants PINS the better-python base file
// layout for the SQLAlchemy driver: emitted inline `<Method>Params` classes are
// written as a block BEFORE the SQL query constants (which precede the
// AsyncQuerier class).
//
// This is a DELIBERATE, DOCUMENTED STEP-A migration delta vs the legacy
// committed file_attachment.py, where upstream sqlc-gen-python 1.3.0
// interleaves each Params class immediately after its query's SQL constant.
// The struct-before-constants block is the better-python BASE layout — the same
// category of base-format delta as `__all__` and `from __future__ import
// annotations` (both absent from the legacy reference bytes, both present in every
// fork golden and accepted as STEP-A). The fork's whole purpose is to migrate
// the legacy tree ONTO better-python's format.
//
// Why this does NOT break the cutover deletion proof (GOAL §6): STEP-B is
// byte-empty against the STEP-A MIGRATION BASELINE (the fork's regenerated
// tree), NOT against the legacy committed tree. The sed/perl post-processors
// only do type/nullability TEXT substitutions — they never reorder classes — so
// class POSITION is determined entirely by the generator on BOTH the migration
// baseline and the post-deletion regeneration. The position is therefore
// identical across STEP-A and STEP-B, leaving STEP-B byte-empty regardless of
// which layout the generator chooses. Reproducing the legacy interleave would
// require restructuring the shared body assembly (body + pyTableBody +
// funcBody) used by all three existing drivers — a large, risky, byte-perturbing
// change for ZERO cutover benefit. Decision (review option b): document the
// delta and PIN the chosen layout here so it cannot drift silently.
func TestSQLAlchemyParamsClassPrecedesConstants(t *testing.T) {
	queries := []*plugin.Query{
		{
			Name:     "InsertThing",
			Cmd:      metadata.CmdExec,
			Filename: "queries.sql",
			Text:     "INSERT INTO file_attachment (upload_id, chat_id, extraction_id) VALUES ($1::text, $2::text, $3::text)",
			Params: []*plugin.Parameter{
				{Number: 1, Column: &plugin.Column{Name: "upload_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
				{Number: 2, Column: &plugin.Column{Name: "chat_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
				{Number: 3, Column: &plugin.Column{Name: "extraction_id", NotNull: true, Type: &plugin.Identifier{Name: "text"}}},
			},
		},
	}
	out := runSQLAlchemyWithLimit(t, fileAttachmentCatalog(), queries, `{
		"package": "m",
		"sql_driver": "sqlalchemy",
		"model_type": "pydantic",
		"emit_classes": true,
		"emit_init_file": false,
		"query_parameter_limit": 2
	}`)

	classIdx := strings.Index(out, "class InsertThingParams(pydantic.BaseModel):")
	constIdx := strings.Index(out, "INSERT_THING: typing.Final[str]")
	querierIdx := strings.Index(out, "class AsyncQuerier:")
	if classIdx < 0 || constIdx < 0 || querierIdx < 0 {
		t.Fatalf("missing expected symbols (class=%d const=%d querier=%d):\n%s", classIdx, constIdx, querierIdx, out)
	}
	// Pinned better-python base layout: Params block, THEN SQL constants, THEN
	// the AsyncQuerier class.
	if !(classIdx < constIdx && constIdx < querierIdx) {
		t.Errorf("expected layout Params(%d) < constant(%d) < AsyncQuerier(%d):\n%s", classIdx, constIdx, querierIdx, out)
	}
}
