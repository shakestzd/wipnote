package claimledger

import (
	"strings"
	"testing"
)

// TestLedgerHeader_DeclaresLanguage pins the lang attribute on the page. A
// screen reader guesses the language of a page without one (bug-e794cbc0).
func TestLedgerHeader_DeclaresLanguage(t *testing.T) {
	if !strings.Contains(ledgerHeader, `<html lang="en">`) {
		t.Fatalf("ledger header has no lang attribute:\n%s", ledgerHeader)
	}
}
