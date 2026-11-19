package hooks

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

const activeLineWant = "ACTIVE: feat-c0ffee01 — Brew the coffee"

// activeProject opens a claim on feat-c0ffee01 and returns the project dir.
func activeProject(t *testing.T) string {
	t.Helper()
	projectDir := canonGuardProject(t)
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	writeCanonGuardFeature(t, wipnoteDir, "feat-c0ffee01", "Brew the coffee")
	store := claimledger.NewStore(wipnoteDir)
	if _, _, err := store.Open(canonGuardSession, claimledger.Episode{WorkItemID: "feat-c0ffee01",
		SessionID: canonGuardSession, RootSessionID: canonGuardSession,
		AgentID: db.AgentRootSentinel, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

func promptContext(t *testing.T, projectDir, prompt string) string {
	t.Helper()
	res, err := UserPrompt(&CloudEvent{SessionID: canonGuardSession, CWD: projectDir, Prompt: prompt, Harness: HarnessClaude})
	if err != nil {
		t.Fatal(err)
	}
	return res.AdditionalContext
}

func startSession(t *testing.T, projectDir, source string) {
	t.Helper()
	ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, Source: source, Harness: HarnessClaude}
	if _, err := SessionStart(ev, projectDir); err != nil {
		t.Fatal(err)
	}
}

func TestUserPrompt_ActiveLineOnlyOnChange(t *testing.T) {
	projectDir := activeProject(t)
	if got := promptContext(t, projectDir, "first prompt"); got != activeLineWant {
		t.Fatalf("first prompt: want %q, got %q", activeLineWant, got)
	}
	if got := promptContext(t, projectDir, "second prompt"); strings.Contains(got, "ACTIVE:") {
		t.Fatalf("same item: want no ACTIVE line, got %q", got)
	}
}

func TestUserPrompt_ActiveLineSentOnItemChange(t *testing.T) {
	projectDir := activeProject(t)
	promptContext(t, projectDir, "first prompt")
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	writeCanonGuardFeature(t, wipnoteDir, "feat-c0ffee02", "Grind the beans")
	store := claimledger.NewStore(wipnoteDir)
	if _, _, err := store.Open(canonGuardSession, claimledger.Episode{WorkItemID: "feat-c0ffee02",
		SessionID: canonGuardSession, RootSessionID: canonGuardSession,
		AgentID: db.AgentRootSentinel, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if got := promptContext(t, projectDir, "third prompt"); got != "ACTIVE: feat-c0ffee02 — Grind the beans" {
		t.Fatalf("item change: want the new ACTIVE line, got %q", got)
	}
}

func testResentAfter(t *testing.T, source string) {
	projectDir := activeProject(t)
	promptContext(t, projectDir, "first prompt")
	startSession(t, projectDir, source)
	if got := promptContext(t, projectDir, "after "+source); got != activeLineWant {
		t.Fatalf("after %s: want %q, got %q", source, activeLineWant, got)
	}
	if got := promptContext(t, projectDir, "next"); strings.Contains(got, "ACTIVE:") {
		t.Fatalf("after %s then one prompt: want no ACTIVE line, got %q", source, got)
	}
}

func TestUserPrompt_ResentAfterSessionStart(t *testing.T) { testResentAfter(t, "startup") }
func TestUserPrompt_ResentAfterResume(t *testing.T)       { testResentAfter(t, "resume") }
func TestUserPrompt_ResentAfterCompact(t *testing.T)      { testResentAfter(t, "compact") }
func TestUserPrompt_ResentAfterClear(t *testing.T)        { testResentAfter(t, "clear") }
