package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupPlanGenerateDir creates a minimal .wipnote directory structure
// with a fake track HTML file, returning the wipnote dir path.
func setupPlanGenerateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create required subdirectories.
	for _, sub := range []string{"tracks", "features", "plans"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Write a minimal track HTML file.
	trackHTML := `<!DOCTYPE html><html><body>
<article id="trk-testabcd">
<header><h1>Test Track</h1></header>
<div data-section="description"><p>A test track description.</p></div>
</article></body></html>`
	trackPath := filepath.Join(dir, "tracks", "trk-testabcd.html")
	if err := os.WriteFile(trackPath, []byte(trackHTML), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestPlanGenerateFromTrackID verifies that a trk-* argument uses retroactive mode
// and produces a plan file.
func TestPlanGenerateFromTrackID(t *testing.T) {
	dir := setupPlanGenerateDir(t)

	_, err := runPlanGenerateFromWorkItem(dir, "trk-testabcd")
	if err != nil {
		t.Fatalf("runPlanGenerateFromWorkItem: %v", err)
	}

	// Exactly one plan file should have been created.
	entries, err := os.ReadDir(filepath.Join(dir, "plans"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 plan file, got %d", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(dir, "plans", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)

	// Plan must reference the source ID.
	if !strings.Contains(html, "trk-testabcd") {
		t.Error("plan HTML does not reference the source track ID")
	}
	// Plan must contain the title.
	if !strings.Contains(html, "Test Track") {
		t.Error("plan HTML does not contain the source title")
	}
}

// TestPlanGenerateFromFeatID verifies that a feat-* argument routes to retroactive mode.
func TestPlanGenerateFromFeatID(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"features", "plans"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	featHTML := `<!DOCTYPE html><html><body>
<article id="feat-abcd1234">
<header><h1>My Feature</h1></header>
</article></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "features", "feat-abcd1234.html"), []byte(featHTML), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runPlanGenerateFromWorkItem(dir, "feat-abcd1234")
	if err != nil {
		t.Fatalf("runPlanGenerateFromWorkItem feat: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Join(dir, "plans"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 plan file, got %d", len(entries))
	}
}

// TestPlanGenerateFreeTextRoutes verifies that a free-text argument (no known
// prefix) delegates to createPlanFromTopic and creates a plan.
func TestPlanGenerateFreeTextRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}

	planID, err := routePlanGenerateByArg(dir, "Auth Middleware Rewrite")
	if err != nil {
		t.Fatalf("routePlanGenerateByArg free text: %v", err)
	}

	if !strings.HasPrefix(planID, "plan-") {
		t.Errorf("expected plan ID, got %q", planID)
	}

	planPath := filepath.Join(dir, "plans", planID+".html")
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("plan file not found: %v", err)
	}
	if !strings.Contains(string(data), "Auth Middleware Rewrite") {
		t.Error("plan HTML does not contain free-text topic title")
	}
}

// TestPlanGeneratePlanPrefixRescaffolds verifies that a plan-* argument
// attempts to re-scaffold the plan (returns error if plan not found).
func TestPlanGeneratePlanPrefixRescaffolds(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plans"), 0o755)

	// Non-existent plan should error.
	_, err := routePlanGenerateByArg(dir, "plan-nonexist")
	if err == nil {
		t.Fatal("expected error for non-existent plan-* argument, got nil")
	}

	// Create a plan, then re-scaffold it.
	planID, err := createPlanFromTopic(dir, "Rescaffold Test", "desc")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := routePlanGenerateByArg(dir, planID)
	if err != nil {
		t.Fatalf("re-scaffold: %v", err)
	}
	if got != planID {
		t.Errorf("re-scaffold returned %q, want %q", got, planID)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "plans", planID+".html"))
	html := string(data)
	if !strings.Contains(html, "Rescaffold Test") {
		t.Error("re-scaffolded HTML missing title")
	}
	if !strings.Contains(html, "btn-finalize") {
		t.Error("re-scaffolded HTML missing CRISPI btn-finalize")
	}
}

// TestPlanGenerateTitleCollisionGuard verifies that a trk-* argument that
// cannot be resolved in the .wipnote dir returns an error — it must not
// fall through to topic creation mode.
func TestPlanGenerateTitleCollisionGuard(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tracks"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := routePlanGenerateByArg(dir, "trk-nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown trk- ID, got nil")
	}

	// No plan file should have been created.
	entries, _ := os.ReadDir(filepath.Join(dir, "plans"))
	if len(entries) > 0 {
		t.Errorf("expected no plan files to be created, found %d", len(entries))
	}
}

// TestPlanGenerateDeduplicateExistingPlan verifies that calling generate again
// for the same source ID returns the existing plan rather than creating a duplicate.
func TestPlanGenerateDeduplicateExistingPlan(t *testing.T) {
	dir := setupPlanGenerateDir(t)

	// First call creates the plan.
	if _, err := runPlanGenerateFromWorkItem(dir, "trk-testabcd"); err != nil {
		t.Fatalf("first generate: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Join(dir, "plans"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 plan after first generate, got %d", len(entries))
	}
	firstPlanName := entries[0].Name()

	// Second call for the same ID should NOT create a second plan.
	if _, err := runPlanGenerateFromWorkItem(dir, "trk-testabcd"); err != nil {
		t.Fatalf("second generate: %v", err)
	}

	entries, _ = os.ReadDir(filepath.Join(dir, "plans"))
	if len(entries) != 1 {
		t.Fatalf("expected still 1 plan after second generate, got %d", len(entries))
	}
	if entries[0].Name() != firstPlanName {
		t.Errorf("plan file name changed between calls: %s → %s", firstPlanName, entries[0].Name())
	}
}
