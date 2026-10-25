package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractPlanTitleFromMarkdown(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantTitle string
		wantDesc  string
	}{
		{
			name:      "standard plan",
			content:   "# Auth Middleware Rewrite\n\nRewrite the auth layer for compliance.\n\n## Slice 1",
			wantTitle: "Auth Middleware Rewrite",
			wantDesc:  "Rewrite the auth layer for compliance.",
		},
		{
			name:      "no description",
			content:   "# Plan Title\n\n## First Slice",
			wantTitle: "Plan Title",
			wantDesc:  "",
		},
		{
			name:      "no H1",
			content:   "## Slice Only\n\n- item 1",
			wantTitle: "",
			wantDesc:  "",
		},
		{
			name:      "empty content",
			content:   "",
			wantTitle: "",
			wantDesc:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, desc := extractPlanTitleFromMarkdown(tc.content)
			if title != tc.wantTitle {
				t.Errorf("title = %q, want %q", title, tc.wantTitle)
			}
			if desc != tc.wantDesc {
				t.Errorf("description = %q, want %q", desc, tc.wantDesc)
			}
		})
	}
}

func TestParseMarkdownToSlices(t *testing.T) {
	md := `# Plan Title

Overview text.

## Set up database schema

- Create migrations table
- Add users table
- File: internal/db/schema.go

## Implement API endpoints

- GET /users endpoint
- POST /users endpoint
- References cmd/main.go and internal/api.go

### Nested subsection

- Should become its own slice
`

	slices := parseMarkdown(md).slices

	if len(slices) != 3 {
		t.Fatalf("got %d slices, want 3", len(slices))
	}

	// Slice 1: Set up database schema
	if slices[0].Title != "Set up database schema" {
		t.Errorf("slice[0].Title = %q", slices[0].Title)
	}
	if slices[0].Num != 1 {
		t.Errorf("slice[0].Num = %d, want 1", slices[0].Num)
	}
	if slices[0].Effort != "M" {
		t.Errorf("slice[0].Effort = %q, want M", slices[0].Effort)
	}
	if slices[0].Risk != "Med" {
		t.Errorf("slice[0].Risk = %q, want Med", slices[0].Risk)
	}
	if slices[0].Approved {
		t.Error("slice[0].Approved should be false")
	}
	// Should have extracted internal/db/schema.go
	foundSchemaFile := false
	for _, f := range slices[0].Files {
		if f == "internal/db/schema.go" {
			foundSchemaFile = true
		}
	}
	if !foundSchemaFile {
		t.Errorf("slice[0].Files = %v, expected to contain internal/db/schema.go", slices[0].Files)
	}

	// Slice 2: Implement API endpoints
	if slices[1].Title != "Implement API endpoints" {
		t.Errorf("slice[1].Title = %q", slices[1].Title)
	}
	if slices[1].Num != 2 {
		t.Errorf("slice[1].Num = %d, want 2", slices[1].Num)
	}

	// Should have extracted file paths from references
	hasAPIFile := false
	hasMainFile := false
	for _, f := range slices[1].Files {
		if f == "internal/api.go" {
			hasAPIFile = true
		}
		if f == "cmd/main.go" {
			hasMainFile = true
		}
	}
	if !hasAPIFile {
		t.Errorf("slice[1].Files = %v, expected to contain internal/api.go", slices[1].Files)
	}
	if !hasMainFile {
		t.Errorf("slice[1].Files = %v, expected to contain cmd/main.go", slices[1].Files)
	}

	// Slice 3: Nested subsection (H3)
	if slices[2].Title != "Nested subsection" {
		t.Errorf("slice[2].Title = %q", slices[2].Title)
	}
	if slices[2].Num != 3 {
		t.Errorf("slice[2].Num = %d, want 3", slices[2].Num)
	}
}

func TestParseMarkdownToSlices_Empty(t *testing.T) {
	slices := parseMarkdown("# Just a title\n\nSome body text.").slices
	if len(slices) != 0 {
		t.Errorf("got %d slices from markdown without H2/H3, want 0", len(slices))
	}
}

func TestParseMarkdownToSlices_SkipsStructuralHeadings(t *testing.T) {
	md := `# My Plan

## Context

Background info about the problem.

## Step 1: Do the thing

- Action item one
- Action item two

## Verification

- Run go test ./...
- Check output

## Step 2: Do another thing

- More work here

## Files Changed

Some files listed here.

## Performance

Some perf notes.
`

	slices := parseMarkdown(md).slices

	if len(slices) != 2 {
		t.Fatalf("got %d slices, want 2 (structural headings should be filtered out)", len(slices))
	}
	if slices[0].Title != "Step 1: Do the thing" {
		t.Errorf("slice[0].Title = %q, want Step 1: Do the thing", slices[0].Title)
	}
	if slices[1].Title != "Step 2: Do another thing" {
		t.Errorf("slice[1].Title = %q, want Step 2: Do another thing", slices[1].Title)
	}
	if slices[0].Num != 1 {
		t.Errorf("slice[0].Num = %d, want 1", slices[0].Num)
	}
	if slices[1].Num != 2 {
		t.Errorf("slice[1].Num = %d, want 2", slices[1].Num)
	}
}

func TestParseMarkdown_ContextPopulatesProblem(t *testing.T) {
	md := `# Plan Title

## Context

This is the problem statement. It explains the background.

## Slice One

- Do something
`

	result := parseMarkdown(md)

	if result.problem == "" {
		t.Error("problem should be populated from Context heading")
	}
	if !strings.Contains(result.problem, "problem statement") {
		t.Errorf("problem = %q, want to contain 'problem statement'", result.problem)
	}
	if len(result.slices) != 1 {
		t.Fatalf("got %d slices, want 1", len(result.slices))
	}
	if result.slices[0].Title != "Slice One" {
		t.Errorf("slice[0].Title = %q", result.slices[0].Title)
	}
}

func TestParseMarkdown_VerificationPopulatesDoneWhen(t *testing.T) {
	md := `# Plan Title

## Slice One

- Do the work

## Verification

- Run go test ./...
- Check the output
- Verify all pass
`

	result := parseMarkdown(md)

	if len(result.doneWhen) != 3 {
		t.Fatalf("got %d doneWhen items, want 3", len(result.doneWhen))
	}
	if result.doneWhen[0] != "Run go test ./..." {
		t.Errorf("doneWhen[0] = %q", result.doneWhen[0])
	}
	if len(result.slices) != 1 {
		t.Fatalf("got %d slices, want 1", len(result.slices))
	}
}

func TestIsStructuralHeading(t *testing.T) {
	structural := []string{
		"Context", "context", "CONTEXT",
		"Background", "Overview", "Summary",
		"Verification", "Testing", "Test plan", "Tests",
		"Performance", "Performance budget",
		"Files Changed", "File Changes", "Key Files",
		"Dependencies", "Prerequisites", "Requirements",
		"Risk", "Risks", "Risk assessment",
		"Notes", "Open questions",
		"Key Design Decisions",
	}
	for _, h := range structural {
		if !isStructuralHeading(h) {
			t.Errorf("isStructuralHeading(%q) = false, want true", h)
		}
	}

	notStructural := []string{
		"Step 1: Setup",
		"Implement caching",
		"Refactor auth module",
		"Add database migrations",
		"Wire into CLI",
	}
	for _, h := range notStructural {
		if isStructuralHeading(h) {
			t.Errorf("isStructuralHeading(%q) = true, want false", h)
		}
	}
}

func TestMostRecentMarkdownFile(t *testing.T) {
	dir := t.TempDir()

	// Create two .md files with staggered mod times.
	old := filepath.Join(dir, "old-plan.md")
	if err := os.WriteFile(old, []byte("# Old Plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Back-date the old file so the newer one is reliably "most recent".
	past := time.Now().Add(-10 * time.Second)
	os.Chtimes(old, past, past)

	recent := filepath.Join(dir, "new-plan.md")
	if err := os.WriteFile(recent, []byte("# New Plan"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := mostRecentMarkdownFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(result, "new-plan.md") {
		t.Errorf("result = %q, want path ending in new-plan.md", result)
	}
}

func TestMostRecentMarkdownFile_NoFiles(t *testing.T) {
	dir := t.TempDir()

	_, err := mostRecentMarkdownFile(dir)
	if err == nil {
		t.Error("expected error for empty directory, got nil")
	}
}

// The suggestion must name commands that exist: `wipnote plan review` was
// removed with the dashboard, and review now runs through `plan slices`.
func TestExitPlanModeSuggestion_NamesLiveCommands(t *testing.T) {
	got := exitPlanModeSuggestion("/p/.wipnote/plans/plan-1.yaml", "plan-1")
	if strings.Contains(got, "plan review") {
		t.Fatalf("suggestion names the removed `plan review` command: %q", got)
	}
	for _, want := range []string{"/p/.wipnote/plans/plan-1.yaml", "wipnote plan validate-yaml plan-1", "wipnote plan slices plan-1"} {
		if !strings.Contains(got, want) {
			t.Errorf("suggestion %q missing %q", got, want)
		}
	}
	plan := planCmdWithExtras()
	for _, sub := range []string{"validate-yaml", "slices"} {
		if c, _, err := plan.Find([]string{sub}); err != nil || c == plan {
			t.Errorf("`wipnote plan %s` is not a registered command: %v", sub, err)
		}
	}
}
