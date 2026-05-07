package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Unit tests for parseCriteria ---

// TestParseCriteria_LegacyChecks verifies that legacy [ ]/[x]/[F] checkboxes
// under ## Acceptance Criteria are parsed correctly.
func TestParseCriteria_LegacyChecks(t *testing.T) {
	t.Parallel()
	spec := `
## Acceptance Criteria

- [ ] Unchecked criterion
- [x] Passed criterion
- [F] Failed criterion
- [f] Also failed criterion
`
	criteria := parseCriteria(spec)
	if len(criteria) != 4 {
		t.Fatalf("expected 4 criteria, got %d", len(criteria))
	}

	cases := []struct {
		idx    int
		text   string
		status criterionStatus
	}{
		{0, "Unchecked criterion", criterionUnchecked},
		{1, "Passed criterion", criterionPassed},
		{2, "Failed criterion", criterionFailed},
		{3, "Also failed criterion", criterionFailed},
	}

	for _, tc := range cases {
		c := criteria[tc.idx]
		if c.Text != tc.text {
			t.Errorf("criteria[%d].Text = %q, want %q", tc.idx, c.Text, tc.text)
		}
		if c.Status != tc.status {
			t.Errorf("criteria[%d].Status = %v, want %v", tc.idx, c.Status, tc.status)
		}
	}
}

// TestParseCriteria_FailedStatus verifies that [F] and [f] produce criterionFailed.
func TestParseCriteria_FailedStatus(t *testing.T) {
	t.Parallel()
	spec := `
## Acceptance Criteria

- [F] Upper-case failed
- [f] Lower-case failed
`
	criteria := parseCriteria(spec)
	if len(criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(criteria))
	}
	for i, c := range criteria {
		if c.Status != criterionFailed {
			t.Errorf("criteria[%d].Status = %v, want criterionFailed", i, c.Status)
		}
	}
}

// TestParseCriteria_OpenSpecFormat verifies that ### Requirement: blocks under
// ## ADDED Requirements are parsed as new-format criteria.
func TestParseCriteria_OpenSpecFormat(t *testing.T) {
	t.Parallel()
	spec := `
## ADDED Requirements

### Requirement: First requirement

The system SHALL do something useful.

#### Scenario: Happy path

- [x] It does the thing

### Requirement: Second requirement

The system SHALL also do another thing.

#### Scenario: Basic scenario

- [x] The other thing works
- [x] Edge case works
`
	criteria := parseCriteria(spec)
	if len(criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d: %+v", len(criteria), criteria)
	}

	if criteria[0].Text == "" {
		t.Error("criteria[0].Text should not be empty")
	}
	if !strings.Contains(criteria[0].Text, "First requirement") {
		t.Errorf("criteria[0].Text should contain requirement name, got %q", criteria[0].Text)
	}
	if criteria[0].Status != criterionPassed {
		t.Errorf("criteria[0].Status = %v, want criterionPassed (all [x])", criteria[0].Status)
	}

	if !strings.Contains(criteria[1].Text, "Second requirement") {
		t.Errorf("criteria[1].Text should contain requirement name, got %q", criteria[1].Text)
	}
	if criteria[1].Status != criterionPassed {
		t.Errorf("criteria[1].Status = %v, want criterionPassed (all [x])", criteria[1].Status)
	}
}

// TestParseCriteria_NoFalsePollution verifies that a legacy ## Acceptance Criteria
// section containing meta-text like "### Requirement:" does NOT produce new-format criteria.
func TestParseCriteria_NoFalsePollution(t *testing.T) {
	t.Parallel()
	spec := `
## Acceptance Criteria

- [x] Normal legacy criterion
- [ ] Another criterion

### Requirement: This is just descriptive text inside legacy section

More explanation text here.

#### Scenario: More explanation

This paragraph should not generate a criterion.
`
	criteria := parseCriteria(spec)
	// Only the two checkbox lines should be counted, not the ### Requirement: heading.
	if len(criteria) != 2 {
		t.Fatalf("expected 2 criteria (legacy checkboxes only), got %d: %+v", len(criteria), criteria)
	}
	if criteria[0].Status != criterionPassed {
		t.Errorf("criteria[0].Status = %v, want criterionPassed", criteria[0].Status)
	}
	if criteria[1].Status != criterionUnchecked {
		t.Errorf("criteria[1].Status = %v, want criterionUnchecked", criteria[1].Status)
	}
}

// TestParseCriteria_StatusFromScenario_AllPass verifies that a requirement with
// multiple scenarios all having [x] resolves to criterionPassed.
func TestParseCriteria_StatusFromScenario_AllPass(t *testing.T) {
	t.Parallel()
	spec := `
## ADDED Requirements

### Requirement: Multi-scenario all pass

The system SHALL handle multiple scenarios.

#### Scenario: First scenario

- [x] First check passes

#### Scenario: Second scenario

- [x] Second check passes
- [x] Third check passes
`
	criteria := parseCriteria(spec)
	if len(criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(criteria))
	}
	if criteria[0].Status != criterionPassed {
		t.Errorf("criteria[0].Status = %v, want criterionPassed", criteria[0].Status)
	}
}

// TestParseCriteria_StatusFromScenario_AnyFail verifies the any-fail-is-fail rule:
// if any scenario task line contains [F], the requirement is failed.
func TestParseCriteria_StatusFromScenario_AnyFail(t *testing.T) {
	t.Parallel()
	spec := `
## ADDED Requirements

### Requirement: Requirement with failure

The system SHALL do something.

#### Scenario: Mostly passing

- [x] This passes
- [F] This failed
- [x] This also passes
`
	criteria := parseCriteria(spec)
	if len(criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(criteria))
	}
	if criteria[0].Status != criterionFailed {
		t.Errorf("criteria[0].Status = %v, want criterionFailed (any-fail-is-fail)", criteria[0].Status)
	}
}

// TestParseCriteria_StatusFromScenario_AnyUnchecked verifies that a requirement
// with all [x] except one [ ] resolves to criterionUnchecked (not passed).
func TestParseCriteria_StatusFromScenario_AnyUnchecked(t *testing.T) {
	t.Parallel()
	spec := `
## ADDED Requirements

### Requirement: Partially done requirement

The system SHALL do something else.

#### Scenario: Most done

- [x] Done
- [x] Also done
- [ ] Not done yet
`
	criteria := parseCriteria(spec)
	if len(criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(criteria))
	}
	if criteria[0].Status != criterionUnchecked {
		t.Errorf("criteria[0].Status = %v, want criterionUnchecked", criteria[0].Status)
	}
}

// TestParseCriteria_HybridDocument verifies that a document with both
// ## Acceptance Criteria AND ## ADDED Requirements sections produces
// criteria from both sections with no cross-contamination.
func TestParseCriteria_HybridDocument(t *testing.T) {
	t.Parallel()
	spec := `
## Acceptance Criteria

- [x] Legacy criterion one
- [ ] Legacy criterion two

## ADDED Requirements

### Requirement: New format requirement

The system SHALL meet this new requirement.

#### Scenario: Basic test

- [x] New format check passes
`
	criteria := parseCriteria(spec)
	// Should have 3 criteria: 2 legacy + 1 new-format.
	if len(criteria) != 3 {
		t.Fatalf("expected 3 criteria, got %d: %+v", len(criteria), criteria)
	}

	// First two should be legacy checkboxes.
	if criteria[0].Status != criterionPassed {
		t.Errorf("criteria[0] (legacy passed): got %v, want criterionPassed", criteria[0].Status)
	}
	if criteria[1].Status != criterionUnchecked {
		t.Errorf("criteria[1] (legacy unchecked): got %v, want criterionUnchecked", criteria[1].Status)
	}

	// Third should be the new-format requirement.
	if !strings.Contains(criteria[2].Text, "New format requirement") {
		t.Errorf("criteria[2].Text should contain 'New format requirement', got %q", criteria[2].Text)
	}
	if criteria[2].Status != criterionPassed {
		t.Errorf("criteria[2] (new format passed): got %v, want criterionPassed", criteria[2].Status)
	}
}

// --- Integration test for computeCompliance / printComplianceJSON ---

// TestComplianceCommand_OpenSpecSpec verifies the full compliance pipeline
// (computeCompliance + printComplianceJSON) against a feature with new-format
// OpenSpec requirements covering all three status values.
func TestComplianceCommand_OpenSpecSpec(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	wipnoteDir := filepath.Join(tmpDir, ".wipnote")
	featuresDir := filepath.Join(wipnoteDir, "features")
	if err := os.MkdirAll(featuresDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	featureID := "feat-openspec-test"
	specContent := `
## ADDED Requirements

### Requirement: Must pass requirement

The system SHALL pass this check.

#### Scenario: All green

- [x] Check one
- [x] Check two

### Requirement: Must fail requirement

The system SHALL fail this check (for test purposes).

#### Scenario: Has failure

- [x] This is fine
- [F] This one failed

### Requirement: Unchecked requirement

The system SHALL not be checked yet.

#### Scenario: Pending

- [ ] Not checked yet
`

	featureHTML := minimalFeatureHTML(featureID, specContent, "")
	featurePath := filepath.Join(featuresDir, featureID+".html")
	if err := os.WriteFile(featurePath, []byte(featureHTML), 0o644); err != nil {
		t.Fatalf("write feature HTML: %v", err)
	}

	origProjectDir := projectDirFlag
	projectDirFlag = tmpDir
	defer func() { projectDirFlag = origProjectDir }()

	result, err := computeCompliance(featureID)
	if err != nil {
		t.Fatalf("computeCompliance: %v", err)
	}

	if !result.HasSpec {
		t.Fatal("expected HasSpec=true")
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if result.Passed != 1 {
		t.Errorf("Passed = %d, want 1", result.Passed)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
	if result.Unchecked != 1 {
		t.Errorf("Unchecked = %d, want 1", result.Unchecked)
	}
	if !result.HasFailure {
		t.Error("HasFailure should be true")
	}

	// Verify JSON output includes all three status values via a pipe capture.
	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("pipe: %v", pipeErr)
	}
	os.Stdout = w

	jsonErr := printComplianceJSON(result)

	w.Close()
	os.Stdout = oldStdout

	if jsonErr != nil {
		t.Fatalf("printComplianceJSON: %v", jsonErr)
	}

	var buf strings.Builder
	readBuf := make([]byte, 4096)
	for {
		n, readErr := r.Read(readBuf)
		if n > 0 {
			buf.Write(readBuf[:n])
		}
		if readErr != nil {
			break
		}
	}
	r.Close()

	jsonOutput := buf.String()

	// Parse the JSON and validate structure.
	var out struct {
		FeatureID  string `json:"feature_id"`
		Total      int    `json:"total"`
		Passed     int    `json:"passed"`
		Failed     int    `json:"failed"`
		Unchecked  int    `json:"unchecked"`
		HasFailure bool   `json:"has_failure"`
		Criteria   []struct {
			Index  int    `json:"index"`
			Text   string `json:"text"`
			Status string `json:"status"`
		} `json:"criteria"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &out); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput was: %s", err, jsonOutput)
	}

	if out.Total != 3 {
		t.Errorf("JSON Total = %d, want 3", out.Total)
	}

	statusCounts := map[string]int{}
	for _, c := range out.Criteria {
		statusCounts[c.Status]++
	}
	if statusCounts["pass"] != 1 {
		t.Errorf("JSON: expected 1 pass criterion, got %d", statusCounts["pass"])
	}
	if statusCounts["fail"] != 1 {
		t.Errorf("JSON: expected 1 fail criterion, got %d", statusCounts["fail"])
	}
	if statusCounts["unchecked"] != 1 {
		t.Errorf("JSON: expected 1 unchecked criterion, got %d", statusCounts["unchecked"])
	}
}
