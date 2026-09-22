package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/agent"
)

// recordTool appends one canonical tool event for sessionID.
func recordTool(t *testing.T, projectDir, sessionID, tool, summary, input string) {
	t.Helper()
	appendCanonicalToolEvent(projectDir, sessionID, canonicalToolEvent{
		Tool: tool, Agent: "coder-1", Summary: summary, Input: input,
	})
}

// TestCanonicalToolLog_RoundTrip pins the append/read contract, including the
// "cannot verify" signal the guards fail open on.
func TestCanonicalToolLog_RoundTrip(t *testing.T) {
	projectDir := t.TempDir()
	const sessionID = "canon-tool-log-1"

	if _, ok := readCanonicalToolEvents(projectDir, sessionID); ok {
		t.Fatal("expected ok=false before anything is recorded")
	}

	recordTool(t, projectDir, sessionID, "Read", "core/hooks/yolo_guard.go", `{"file_path":"core/hooks/yolo_guard.go"}`)
	recordTool(t, projectDir, sessionID, "Write", "index.html", `{"file_path":"index.html"}`)

	events, ok := readCanonicalToolEvents(projectDir, sessionID)
	if !ok || len(events) != 2 {
		t.Fatalf("readCanonicalToolEvents = %v (ok=%v), want 2 events", events, ok)
	}
	if events[0].Tool != "Read" || events[1].Tool != "Write" {
		t.Errorf("unexpected tools: %+v", events)
	}

	// An empty tool name is not a tool call and must never be recorded.
	appendCanonicalToolEvent(projectDir, sessionID, canonicalToolEvent{Summary: "no tool"})
	if events, _ := readCanonicalToolEvents(projectDir, sessionID); len(events) != 2 {
		t.Errorf("empty tool name was recorded: %+v", events)
	}
}

// TestCanonicalToolLog_BoundedGrowth pins the size cap: a runaway session must
// not let a hook grow this file without limit.
func TestCanonicalToolLog_BoundedGrowth(t *testing.T) {
	projectDir := t.TempDir()
	const sessionID = "canon-tool-log-bounded"
	big := make([]byte, canonicalToolFieldMax*2)
	for i := range big {
		big[i] = 'x'
	}
	for i := 0; i < 4000; i++ {
		recordTool(t, projectDir, sessionID, "Read", string(big), string(big))
	}
	info, err := os.Stat(canonicalToolLogPath(projectDir, sessionID))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// The cap is checked before each append, so the file may exceed it by at
	// most one record.
	maxExpected := int64(canonicalToolLogMaxBytes) + 4*canonicalToolFieldMax + 64
	if info.Size() > maxExpected {
		t.Errorf("log grew to %d bytes, want <= %d", info.Size(), maxExpected)
	}
	if info.Size() < canonicalToolLogMaxBytes/2 {
		t.Errorf("log suspiciously small (%d bytes) — appends may be failing", info.Size())
	}
}

// TestCanonicalToolEventClassifiers pins the classifiers against the SQL
// predicates they mirror.
func TestCanonicalToolEventClassifiers(t *testing.T) {
	research := []canonicalToolEvent{
		{Tool: "Read", Summary: "main.go"},
		{Tool: "Grep", Summary: "foo"},
		{Tool: "WebSearch", Summary: "golang flock"},
		{Tool: "Bash", Summary: "grep -rn foo ."},
		{Tool: "Bash", Summary: "ls"},
		{Tool: "exec_command", Summary: "cat go.mod"},
		// Read-only sed mirrors buildResearchQuery's `sed %` AND NOT `sed -i%`
		// AND NOT `sed --in-place%` SQL clause on this canonical fallback
		// path, which is the one production actually takes (the SQL
		// projection is empty on the hot hook path).
		{Tool: "Bash", Summary: "sed -n '1,20p' main.go"},
		{Tool: "Bash", Summary: "sed 's/foo/bar/' main.go"},
	}
	for _, ev := range research {
		if !isCanonicalResearchEvent(ev) {
			t.Errorf("isCanonicalResearchEvent(%+v) = false, want true", ev)
		}
	}
	notResearch := []canonicalToolEvent{
		{Tool: "Write", Summary: "main.go"},
		{Tool: "Bash", Summary: "go build ./..."},
		{Tool: "Bash", Summary: "rm -rf build"},
		{Tool: "TodoWrite", Summary: "plan"},
		{Tool: "Bash", Summary: "sed -i 's/foo/bar/' main.go"},
		{Tool: "Bash", Summary: "sed --in-place 's/foo/bar/' main.go"},
	}
	for _, ev := range notResearch {
		if isCanonicalResearchEvent(ev) {
			t.Errorf("isCanonicalResearchEvent(%+v) = true, want false", ev)
		}
	}

	if !isCanonicalUIEditEvent(canonicalToolEvent{Tool: "Edit", Summary: "web/index.html"}) {
		t.Error("expected an html Edit to count as a UI edit")
	}
	if isCanonicalUIEditEvent(canonicalToolEvent{Tool: "Edit", Summary: ".wipnote/bugs/bug-1.html"}) {
		t.Error("a .wipnote/ work item is data, not UI")
	}
	if isCanonicalUIEditEvent(canonicalToolEvent{Tool: "Read", Summary: "web/index.html"}) {
		t.Error("a Read is not a UI edit")
	}

	if !isCanonicalScreenshotEvent(canonicalToolEvent{Tool: "mcp__x__take_screenshot"}) {
		t.Error("expected a dedicated screenshot tool to count")
	}
	if !isCanonicalScreenshotEvent(canonicalToolEvent{
		Tool:  "mcp__claude-in-chrome__browser_batch",
		Input: `{"actions":[{"name":"computer","input":{"action": "screenshot"}}]}`,
	}) {
		t.Error("expected a nested screenshot action to count")
	}
	if isCanonicalScreenshotEvent(canonicalToolEvent{Tool: "Bash", Summary: "go test ./..."}) {
		t.Error("unrelated tool counted as a screenshot")
	}
}

// TestHasRecentResearch_CanonicalFallback pins bug-a3b17225 for the research
// gate: with an EMPTY projection (the real read-only hook handle) the SQL
// probe can only say "recording gap", so the canonical tool log decides.
func TestHasRecentResearch_CanonicalFallback(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	stubRouteSQLAsync(t, true)

	const sessionID = "canon-research-1"
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	database, _ := OpenHookDBReadOnly("pretooluse", sessionID, ":memory:")
	if database == nil {
		t.Fatal("OpenHookDBReadOnly returned nil handle")
	}
	t.Cleanup(func() { _ = database.Close() })

	// Nothing recorded anywhere: genuine gap → fail open.
	if !hasRecentResearch(database, sessionID, "coder-1", projectDir) {
		t.Fatal("expected fail-open with no canonical log")
	}

	// Tool calls happened, none of them research → block.
	recordTool(t, projectDir, sessionID, "Bash", "go build ./...", "")
	recordTool(t, projectDir, sessionID, "TodoWrite", "plan", "")
	if hasRecentResearch(database, sessionID, "coder-1", projectDir) {
		t.Fatal("expected no-research block once the canonical log shows non-research tool calls")
	}

	// A real research read clears the gate.
	recordTool(t, projectDir, sessionID, "Read", "core/hooks/pretooluse.go", "")
	if !hasRecentResearch(database, sessionID, "coder-1", projectDir) {
		t.Fatal("expected the gate to clear after a canonical Read")
	}
}

// TestHasRecentResearch_CanonicalFallbackUsesFamilyRoot covers the Codex-style
// lineage: the orchestrator's research is recorded under the family root, and
// its subagent (a distinct session ID) inherits it.
func TestHasRecentResearch_CanonicalFallbackUsesFamilyRoot(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	stubRouteSQLAsync(t, true)

	const rootSession = "canon-research-root"
	const childSession = "canon-research-child"
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	if err := agent.RegisterSessionFamily(projectDir, childSession, rootSession); err != nil {
		t.Fatalf("RegisterSessionFamily: %v", err)
	}
	database, _ := OpenHookDBReadOnly("pretooluse", childSession, ":memory:")
	if database == nil {
		t.Fatal("OpenHookDBReadOnly returned nil handle")
	}
	t.Cleanup(func() { _ = database.Close() })

	recordTool(t, projectDir, childSession, "Bash", "go build ./...", "")
	if hasRecentResearch(database, childSession, "coder-1", projectDir) {
		t.Fatal("expected block: only non-research tool calls recorded")
	}
	recordTool(t, projectDir, rootSession, "Grep", "handler", "")
	if !hasRecentResearch(database, childSession, "coder-1", projectDir) {
		t.Fatal("expected the family root's research to clear the child's gate")
	}
}

// TestUIValidationGuard_CanonicalFallback pins bug-a3b17225 for the
// UI-validation gate on the read-only hook path.
func TestUIValidationGuard_CanonicalFallback(t *testing.T) {
	clearNestedEnv(t)
	repoDir := setupTempGitRepo(t)
	const sessionID = "canon-ui-1"

	htmlFile := filepath.Join(repoDir, "index.html")
	if err := os.WriteFile(htmlFile, []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}
	addCmd := exec.Command("git", "add", "index.html")
	addCmd.Dir = repoDir
	addCmd.Env = cleanEnv()
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	database, _ := OpenHookDBReadOnly("pretooluse", sessionID, ":memory:")
	if database == nil {
		t.Fatal("OpenHookDBReadOnly returned nil handle")
	}
	t.Cleanup(func() { _ = database.Close() })

	event := &CloudEvent{
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "git commit -m 'ui change'"},
	}

	// No canonical log: cannot verify that UI work happened → allow.
	if warn := checkYoloUIValidationGuard(event, true, database, sessionID, repoDir); warn != "" {
		t.Fatalf("expected fail-open with no canonical log, got: %s", warn)
	}

	// The session edited a UI file and never screenshotted → block.
	recordTool(t, repoDir, sessionID, "Edit", "index.html", `{"file_path":"index.html"}`)
	warn := checkYoloUIValidationGuard(event, true, database, sessionID, repoDir)
	if warn == "" {
		t.Fatal("expected a block once the canonical log shows an unvalidated UI edit")
	}

	// A screenshot clears the gate.
	recordTool(t, repoDir, sessionID, "mcp__claude-in-chrome__computer", "", `{"action":"screenshot"}`)
	if warn := checkYoloUIValidationGuard(event, true, database, sessionID, repoDir); warn != "" {
		t.Fatalf("expected allow after a canonical screenshot, got: %s", warn)
	}
}
