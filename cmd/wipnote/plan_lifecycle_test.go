package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanFirstLifecycle exercises the complete plan-first pipeline:
// create → add-slice-yaml → render → critique → validate. Finalize is covered
// by the TestPlanFinalizeFromYAML_* tests.
func TestPlanFirstLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("drives full plan lifecycle")
	}

	dir := t.TempDir()
	for _, sub := range []string{"plans", "features", "tracks"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
	}

	// Step 1: Create plan from topic.
	planID, err := createPlanFromTopic(dir, "Auth Middleware Rewrite", "Rewrite auth for compliance")
	if err != nil {
		t.Fatalf("createPlanFromTopic: %v", err)
	}

	// Verify YAML scaffold was created.
	yamlPath := filepath.Join(dir, "plans", planID+".yaml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Fatalf("YAML scaffold not found: %v", err)
	}

	// Step 2: Add slices via YAML workflow.
	for _, title := range []string{"Error handling layer", "Token validation", "Migration script"} {
		if err := runPlanAddSliceYAML(dir, planID, title,
			"Implement "+title, "", "", "", "", "S", "Low", ""); err != nil {
			t.Fatalf("addSliceYAML(%s): %v", title, err)
		}
	}

	// Step 2b: Render HTML from YAML.
	if err := renderPlanToFile(dir, planID); err != nil {
		t.Fatalf("renderPlanToFile: %v", err)
	}

	// Verify rendered HTML has CRISPI elements.
	planPath := filepath.Join(dir, "plans", planID+".html")
	data, _ := os.ReadFile(planPath)
	if !strings.Contains(string(data), "btn-finalize") {
		t.Error("rendered plan should use CRISPI template with btn-finalize")
	}

	// Step 3: Critique (reads from YAML).
	critique, err := extractCritiqueData(dir, planID)
	if err != nil {
		t.Fatalf("extractCritiqueData: %v", err)
	}
	if !critique.CritiqueWarranted {
		t.Error("3 slices should warrant critique")
	}
	if critique.Complexity != "medium" {
		t.Errorf("complexity = %q, want medium", critique.Complexity)
	}
	if len(critique.Slices) != 3 {
		t.Errorf("critique slices = %d, want 3", len(critique.Slices))
	}

	// Step 4: Validate.
	validation, err := validatePlan(dir, planID)
	if err != nil {
		t.Fatalf("validatePlan: %v", err)
	}
	if !validation.Valid {
		t.Errorf("plan should be valid, got errors: %v", validation.Errors)
	}
}
