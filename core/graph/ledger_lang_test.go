package graph

import (
	"strings"
	"testing"
)

// TestMarshalLedger_DeclaresLanguage pins the lang attribute on the archive
// page. A screen reader guesses the language of a page without one
// (bug-e794cbc0).
func TestMarshalLedger_DeclaresLanguage(t *testing.T) {
	out := string(marshalLedger(nil))
	if !strings.Contains(out, `<html lang="en">`) {
		t.Fatalf("archive ledger has no lang attribute:\n%s", out)
	}
}
