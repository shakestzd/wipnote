package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

// The four guards below used to read only the hook's database handle, which the
// hot hooks no longer get, so each was inert. They now answer from canonical
// state (feat-af8d1689, owner decision 2026-10-25). Each test checks both
// sides: the evidence present and the evidence absent.

const canonGuardSession = "019ee380-abcd-7000-8000-00000000ca11"

func canonGuardProject(t *testing.T) string {
	t.Helper()
	isolateParityEnv(t)
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectDir)
	return projectDir
}

func logTools(projectDir string, evs ...canonicalToolEvent) {
	for _, ev := range evs {
		appendCanonicalToolEvent(projectDir, canonGuardSession, ev)
	}
}

// (a) hasRecentWebResearch reads web research from the canonical tool log.
func TestHasRecentWebResearch_ReadsCanonicalLog(t *testing.T) {
	projectDir := canonGuardProject(t)
	if !hasRecentWebResearch(canonGuardSession, "", projectDir) {
		t.Fatal("no canonical log: want fail-open true")
	}
	logTools(projectDir, canonicalToolEvent{Tool: "Read", Summary: "go.mod"})
	if hasRecentWebResearch(canonGuardSession, "", projectDir) {
		t.Fatal("log with only a local Read: want false")
	}
	logTools(projectDir, canonicalToolEvent{Tool: "Bash", Summary: "gh release view v1.2.3"})
	if !hasRecentWebResearch(canonGuardSession, "", projectDir) {
		t.Fatal("log with a gh command: want true")
	}
}

func TestHasRecentWebResearch_WebFetchCounts(t *testing.T) {
	projectDir := canonGuardProject(t)
	logTools(projectDir, canonicalToolEvent{Tool: "Edit", Summary: "x.go"},
		canonicalToolEvent{Tool: "WebFetch", Summary: "https://pkg.go.dev"})
	if !hasRecentWebResearch(canonGuardSession, "", projectDir) {
		t.Fatal("log with WebFetch: want true")
	}
}

// (b) the orchestrator research advisory fires only before any prior research
// or sidecar dispatch is recorded in the session's canonical log.
func TestOrchestratorResearchAdvisory_UsesCanonicalLog(t *testing.T) {
	projectDir := canonGuardProject(t)
	ctx := &toolUseContext{SessionID: canonGuardSession, ProjectDir: projectDir}
	ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, ToolName: "WebSearch"}
	if checkOrchestratorResearchDelegationAdvisory(ev, ctx) == "" {
		t.Fatal("first root web research with nothing recorded: want the advisory")
	}
	logTools(projectDir, canonicalToolEvent{Tool: "WebFetch", Summary: "https://example.com"})
	if got := checkOrchestratorResearchDelegationAdvisory(ev, ctx); got != "" {
		t.Fatalf("prior web research recorded: want no advisory, got %q", got)
	}
}

func TestOrchestratorResearchAdvisory_SidecarSuppresses(t *testing.T) {
	for _, prior := range []canonicalToolEvent{
		{Tool: "Agent", Summary: "researcher"},
		{Tool: "Bash", Summary: "codex exec 'look up the docs'"},
	} {
		projectDir := canonGuardProject(t)
		logTools(projectDir, canonicalToolEvent{Tool: "Read", Summary: "x.go"}, prior)
		ctx := &toolUseContext{SessionID: canonGuardSession, ProjectDir: projectDir}
		ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, ToolName: "WebSearch"}
		if got := checkOrchestratorResearchDelegationAdvisory(ev, ctx); got != "" {
			t.Fatalf("prior %s recorded: want no advisory, got %q", prior.Tool, got)
		}
	}
}

// (c) the UserPrompt ACTIVE line comes from the canonical claim ledger.
func TestUserPrompt_ActiveLineFromClaimLedger(t *testing.T) {
	projectDir := canonGuardProject(t)
	ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, Prompt: "carry on", Harness: HarnessClaude}
	res, err := UserPrompt(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.AdditionalContext, "ACTIVE:") {
		t.Fatalf("no claim: want no ACTIVE line, got %q", res.AdditionalContext)
	}

	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	writeCanonGuardFeature(t, wipnoteDir, "feat-c0ffee01", "Brew the coffee")
	store := claimledger.NewStore(wipnoteDir)
	if _, _, err := store.Open(canonGuardSession, claimledger.Episode{WorkItemID: "feat-c0ffee01",
		SessionID: canonGuardSession, RootSessionID: canonGuardSession,
		AgentID: db.AgentRootSentinel, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	ev.Prompt = "carry on, please"
	res, err = UserPrompt(ev)
	if err != nil {
		t.Fatal(err)
	}
	if res.AdditionalContext != "ACTIVE: feat-c0ffee01 — Brew the coffee" {
		t.Fatalf("open claim: want the ACTIVE line, got %q", res.AdditionalContext)
	}
}

func writeCanonGuardFeature(t *testing.T, wipnoteDir, id, title string) {
	t.Helper()
	dir := filepath.Join(wipnoteDir, "features")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	html := `<!DOCTYPE html><html><head><title>` + title + `</title></head><body>` +
		`<article id="` + id + `" data-type="feature" data-status="in-progress">` +
		`<header><h1>` + title + `</h1></header></article></body></html>`
	if err := os.WriteFile(filepath.Join(dir, id+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
}

// (d) an honoured WIPNOTE_GUARDS_OFF is recorded in the canonical tool log.
func TestGuardsOff_RecordsOverrideInCanonicalLog(t *testing.T) {
	projectDir := canonGuardProject(t)
	var stderr bytes.Buffer
	prev := guardOverrideStderr
	guardOverrideStderr = &stderr
	t.Cleanup(func() { guardOverrideStderr = prev })
	ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, ToolName: "Bash",
		ToolInput: map[string]any{"command": "ls"}, Harness: HarnessClaude}

	t.Setenv(guardOverrideEnvVar, "")
	if _, err := PreToolUse(ev); err != nil {
		t.Fatal(err)
	}
	if hasGuardOverrideEvent(projectDir) {
		t.Fatal("override not set: no GuardOverride event expected")
	}
	t.Setenv(guardOverrideEnvVar, "1")
	if _, err := PreToolUse(ev); err != nil {
		t.Fatal(err)
	}
	if !hasGuardOverrideEvent(projectDir) {
		t.Fatal("honoured override must be recorded in the canonical tool log")
	}
}

func hasGuardOverrideEvent(projectDir string) bool {
	events, _ := readCanonicalToolEvents(projectDir, canonGuardSession)
	for _, ev := range events {
		if ev.Tool == guardOverrideToolName && strings.Contains(ev.Summary, "for Bash") {
			return true
		}
	}
	return false
}
