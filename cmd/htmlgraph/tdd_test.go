package main

import (
	"strings"
	"testing"
)

// TestTDDExtract_OpenSpecRequirements — `### Requirement:` blocks become
// criteria for the TDD generator.
func TestTDDExtract_OpenSpecRequirements(t *testing.T) {
	html := `<html><body>
<section class="spec"><pre>## ADDED Requirements

### Requirement: Login flow works
The implementation SHALL ensure: users authenticate via OAuth.

#### Scenario: valid token
- [x] WHEN the token signature verifies
- [x] THEN the user is logged in

### Requirement: Logout flow works
The implementation SHALL ensure: sessions can be terminated.

#### Scenario: clean logout
- [x] WHEN the user clicks logout
- [x] THEN the session ends
</pre></section>
</body></html>`

	got := extractFromSpecSection(html)
	if len(got) < 2 {
		t.Fatalf("expected ≥2 criteria from OpenSpec spec, got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "Login") {
		t.Errorf("expected Login criterion, got %v", got)
	}
	if !strings.Contains(joined, "Logout") {
		t.Errorf("expected Logout criterion, got %v", got)
	}
}

// TestTDDExtract_LegacyCheckboxes — backward compat with the legacy
// `1. [ ] criterion` format.
func TestTDDExtract_LegacyCheckboxes(t *testing.T) {
	html := `<html><body>
<section class="spec">
## Acceptance Criteria
1. [ ] First criterion
2. [x] Second criterion
3. [ ] Third criterion
</section>
</body></html>`

	got := extractFromSpecSection(html)
	if len(got) != 3 {
		t.Fatalf("expected 3 criteria from legacy spec, got %d: %v", len(got), got)
	}
	if got[0] != "First criterion" {
		t.Errorf("got[0] = %q, want 'First criterion'", got[0])
	}
}

// TestTDDExtract_EmptySpec — section exists but has no parseable criteria;
// extractor returns nil so the caller falls through to data-steps.
func TestTDDExtract_EmptySpec(t *testing.T) {
	html := `<html><body>
<section class="spec"><pre>## Problem
No criteria yet.
</pre></section>
</body></html>`

	got := extractFromSpecSection(html)
	if len(got) != 0 {
		t.Errorf("expected 0 criteria from empty spec, got %d: %v", len(got), got)
	}
}

// TestTDDExtract_NoSpecSection — no <section class="spec"> at all returns
// nil so the caller falls through.
func TestTDDExtract_NoSpecSection(t *testing.T) {
	html := `<html><body><article id="feat-x"></article></body></html>`
	got := extractFromSpecSection(html)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// TestTDDExtract_HybridSpec — section contains BOTH legacy and OpenSpec
// formats; parseCriteria's section-state machine keeps them separate; the
// extractor returns at least one criterion from each.
func TestTDDExtract_HybridSpec(t *testing.T) {
	html := `<html><body>
<section class="spec"><pre>## Acceptance Criteria
1. [ ] Legacy criterion text

## ADDED Requirements

### Requirement: Modern criterion text
The implementation SHALL ensure: it works.

#### Scenario: works
- [x] WHEN run
- [x] THEN passes
</pre></section>
</body></html>`

	got := extractFromSpecSection(html)
	if len(got) < 2 {
		t.Fatalf("expected ≥2 criteria from hybrid spec, got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "Legacy criterion") {
		t.Errorf("missing legacy criterion: %v", got)
	}
	if !strings.Contains(joined, "Modern criterion") {
		t.Errorf("missing modern criterion: %v", got)
	}
}
