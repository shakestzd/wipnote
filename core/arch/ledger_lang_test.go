package arch

import (
	"strings"
	"testing"
)

// TestMarshalLedger_DeclaresLanguage pins the lang attribute on the
// architecture page. A screen reader guesses the language of a page without
// one (bug-e794cbc0).
func TestMarshalLedger_DeclaresLanguage(t *testing.T) {
	out, err := marshalLedger(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `<html lang="en">`) {
		t.Fatalf("architecture ledger has no lang attribute:\n%s", out)
	}
}
