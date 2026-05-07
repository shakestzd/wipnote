package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePlan_ValidPlan(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plans"), 0o755)

	planID, err := createPlanFromTopic(dir, "Valid Plan", "A valid plan")
	if err != nil {
		t.Fatal(err)
	}
	if err := runPlanAddSliceYAML(dir, planID, "Slice One",
		"Implement slice one", "", "", "", "", "S", "Low", ""); err != nil {
		t.Fatal(err)
	}

	result, err := validatePlan(dir, planID)
	if err != nil {
		t.Fatalf("validatePlan: %v", err)
	}
	if !result.Valid {
		t.Errorf("plan should be valid, got errors: %v", result.Errors)
	}
	if result.Stats.Slices != 1 {
		t.Errorf("slices = %d, want 1", result.Stats.Slices)
	}
}

func TestValidatePlan_EmptyPlanWarns(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plans"), 0o755)

	planID, err := createPlanFromTopic(dir, "Empty Plan", "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := validatePlan(dir, planID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Errorf("empty plan should be valid (warnings only), got: %v", result.Errors)
	}
	if len(result.Warnings) == 0 {
		t.Error("empty plan should have warnings about missing slices/description")
	}
}

func TestValidatePlan_NotFound(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plans"), 0o755)

	_, err := validatePlan(dir, "plan-nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

// TestValidatePlanHTML_ValidCRISPI verifies that a minimal valid CRISPI HTML passes.
func TestValidatePlanHTML_ValidCRISPI(t *testing.T) {
	html := buildMinimalCRISPIHTML(1)
	path := writeTempHTML(t, html)

	errors, warnings, stats := validatePlanHTML(path)
	if len(errors) > 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	_ = warnings
	if stats.graphNodes != 1 {
		t.Errorf("graphNodes = %d, want 1", stats.graphNodes)
	}
}

// TestValidatePlanHTML_MissingGraphData verifies that missing #graph-data triggers an error.
func TestValidatePlanHTML_MissingGraphData(t *testing.T) {
	html := buildMinimalCRISPIHTML(1)
	// Remove graph-data element.
	html = strings.ReplaceAll(html, `id="graph-data"`, `id="graph-data-REMOVED"`)

	path := writeTempHTML(t, html)
	errors, _, _ := validatePlanHTML(path)

	if !containsSubstr(errors, "graph-data") {
		t.Errorf("expected error about missing #graph-data, got: %v", errors)
	}
}

// TestValidatePlanHTML_MissingFinalize verifies that missing #finalizeBtn triggers an error.
func TestValidatePlanHTML_MissingFinalize(t *testing.T) {
	html := buildMinimalCRISPIHTML(1)
	html = strings.ReplaceAll(html, `id="finalizeBtn"`, `id="finalizeBtn-REMOVED"`)

	path := writeTempHTML(t, html)
	errors, _, _ := validatePlanHTML(path)

	if !containsSubstr(errors, "finalizeBtn") {
		t.Errorf("expected error about missing #finalizeBtn, got: %v", errors)
	}
}

// TestValidatePlanHTML_BrokenSectionsJSON verifies that malformed SECTIONS_JSON triggers an error.
func TestValidatePlanHTML_BrokenSectionsJSON(t *testing.T) {
	html := buildMinimalCRISPIHTML(1)
	// Replace valid JSON array with broken JSON.
	html = strings.Replace(html,
		`/*PLAN_SECTIONS_JSON*/["design","outline","slice-1"]/*END_PLAN_SECTIONS_JSON*/`,
		`/*PLAN_SECTIONS_JSON*/["design","outline",BROKEN/*END_PLAN_SECTIONS_JSON*/`,
		1,
	)

	path := writeTempHTML(t, html)
	errors, _, _ := validatePlanHTML(path)

	if !containsSubstr(errors, "SECTIONS_JSON") {
		t.Errorf("expected error about SECTIONS_JSON, got: %v", errors)
	}
}

// TestValidatePlanHTML_MissingSliceCards verifies that no .slice-card triggers a warning.
func TestValidatePlanHTML_MissingSliceCards(t *testing.T) {
	html := buildMinimalCRISPIHTML(0)

	path := writeTempHTML(t, html)
	_, warnings, _ := validatePlanHTML(path)

	if !containsSubstr(warnings, "slice-card") {
		t.Errorf("expected warning about missing .slice-card, got: %v", warnings)
	}
}

// TestValidatePlanHTML_MissingCDNScripts verifies that missing CDN scripts trigger errors.
func TestValidatePlanHTML_MissingCDNScripts(t *testing.T) {
	html := buildMinimalCRISPIHTML(1)
	// Remove the d3 CDN script by replacing its domain.
	html = strings.ReplaceAll(html, "d3js.org/d3", "example.com/MISSING")
	// Remove the dagre-d3 CDN script entirely.
	html = strings.ReplaceAll(html,
		`<script src="https://cdn.jsdelivr.net/npm/dagre-d3@0.6.4/dist/dagre-d3.min.js"></script>`,
		`<!-- dagre-d3 removed for test -->`,
	)

	path := writeTempHTML(t, html)
	errors, _, _ := validatePlanHTML(path)

	if !containsSubstr(errors, "d3.js") {
		t.Errorf("expected error about missing d3.js CDN, got: %v", errors)
	}
	if !containsSubstr(errors, "dagre-d3") {
		t.Errorf("expected error about missing dagre-d3 CDN, got: %v", errors)
	}
}

// TestValidatePlanHTML_BrokenPlaceholders verifies that unreplaced template placeholders
// produce warnings. Only non-injection-point placeholders are flagged.
func TestValidatePlanHTML_BrokenPlaceholders(t *testing.T) {
	html := buildMinimalCRISPIHTML(1)
	// Inject an unreplaced non-injection-point placeholder.
	html = strings.Replace(html, `<div id="graph-data"`, `<!--PLAN_TOTAL_SECTIONS--><div id="graph-data"`, 1)

	path := writeTempHTML(t, html)
	_, warnings, _ := validatePlanHTML(path)

	if !containsSubstr(warnings, "PLAN_TOTAL_SECTIONS") {
		t.Errorf("expected warning about broken placeholder, got: %v", warnings)
	}
}

// TestValidatePlanHTML_DataNodeMissingAttrs verifies that data-node elements
// without required attributes trigger errors.
func TestValidatePlanHTML_DataNodeMissingAttrs(t *testing.T) {
	html := buildMinimalCRISPIHTML(1)
	// Replace the valid data-node with one missing data-name and data-status.
	html = strings.Replace(html,
		`<div data-node="1" data-name="Slice One" data-status="pending" data-deps=""></div>`,
		`<div data-node="1" data-deps=""></div>`,
		1,
	)

	path := writeTempHTML(t, html)
	errors, _, _ := validatePlanHTML(path)

	if !containsSubstr(errors, "data-name") {
		t.Errorf("expected error about missing data-name, got: %v", errors)
	}
	if !containsSubstr(errors, "data-status") {
		t.Errorf("expected error about missing data-status, got: %v", errors)
	}
}

// TestIsCRISPIFile verifies that CRISPI detection works for both file types.
func TestIsCRISPIFile(t *testing.T) {
	// CRISPI files contain the btn-finalize CSS class or plan-sidebar class.
	crispiHTML := `<html><body><button class="btn-finalize" id="finalizeBtn">Finalize</button></body></html>`
	plainHTML := `<html><body><article id="plan-abc"><h1>Plan</h1></article></body></html>`

	crispiPath := writeTempHTML(t, crispiHTML)
	plainPath := writeTempHTML(t, plainHTML)

	if !isCRISPIFile(crispiPath) {
		t.Error("CRISPI file should be detected as CRISPI")
	}
	if isCRISPIFile(plainPath) {
		t.Error("plain node HTML should not be detected as CRISPI")
	}
}

// TestCountOccurrences verifies the helper counts substrings correctly.
func TestCountOccurrences(t *testing.T) {
	cases := []struct {
		s, sub string
		want   int
	}{
		{"aaa", "a", 3},
		{"abab", "ab", 2},
		{"hello", "xyz", 0},
		{"", "a", 0},
	}
	for _, c := range cases {
		if got := countOccurrences(c.s, c.sub); got != c.want {
			t.Errorf("countOccurrences(%q, %q) = %d, want %d", c.s, c.sub, got, c.want)
		}
	}
}

// ---- helpers -----------------------------------------------------------------

// buildMinimalCRISPIHTML constructs a minimal but structurally valid CRISPI plan HTML.
// Pass sliceCount=0 to produce a plan with no slices (triggers warnings).
func buildMinimalCRISPIHTML(sliceCount int) string {
	// Build graph nodes and slice cards.
	var graphNodes, sliceCards strings.Builder
	var sections []string
	sections = append(sections, `"design"`, `"outline"`)

	for i := 1; i <= sliceCount; i++ {
		graphNodes.WriteString(
			`<div data-node="1" data-name="Slice One" data-status="pending" data-deps=""></div>`,
		)
		sliceCards.WriteString(`<div class="slice-card" data-slice="1">`)
		sliceCards.WriteString(`<div class="approval-row">`)
		sliceCards.WriteString(`<label><input type="checkbox" data-section="slice-1" data-action="approve"> Approve</label>`)
		sliceCards.WriteString(`</div></div>`)
		sections = append(sections, `"slice-1"`)
	}

	sectionsJSON := "[" + strings.Join(sections, ",") + "]"
	totalSections := len(sections)

	return `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>Plan: Test</title></head>
<body>
<nav class="plan-sidebar"><a href="/">wipnote</a></nav>
<div class="plan-content">
<article id="plan-test1234" data-feature-id="feat-xxx" data-status="draft">
<header class="plan-header"><h1>Plan: Test</h1></header>
<section class="dep-graph">
  <div id="graph-data" style="display:none">` +
		graphNodes.String() +
		`  </div>
  <svg id="dep-graph-svg" width="100%"></svg>
</section>
<details class="section-card" data-phase="design">
  <summary>A. Design Discussion<span class="badge badge-pending" data-badge-for="design">Pending</span></summary>
  <div class="section-body">
    <div class="approval-row">
      <label><input type="checkbox" data-section="design" data-action="approve"> Approve design</label>
    </div>
  </div>
</details>
<details class="section-card" data-phase="slices">
  <summary>C. Vertical Slices</summary>
  <div class="section-body">` +
		sliceCards.String() +
		`  </div>
</details>
<section class="progress-zone">
  <strong id="totalSections">` + fmt.Sprintf("%d", totalSections) + `</strong>
  <button class="btn-finalize" id="finalizeBtn" disabled>Finalize Plan</button>
</section>
</article>
<script src="https://d3js.org/d3.v7.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/dagre-d3@0.6.4/dist/dagre-d3.min.js"></script>
<script>
var SECTIONS=/*PLAN_SECTIONS_JSON*/` + sectionsJSON + `/*END_PLAN_SECTIONS_JSON*/;
</script>
</div>
</body>
</html>`
}

// writeTempHTML writes html content to a temp file and returns its path.
func writeTempHTML(t *testing.T, html string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "plan-*.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(html); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

// containsSubstr returns true if any string in ss contains substr.
func containsSubstr(ss []string, substr string) bool {
	for _, s := range ss {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
