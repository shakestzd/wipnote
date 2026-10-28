package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/runledger"
)

// codexRunClock pins the run clock; advance moves it forward.
func codexRunClock(t *testing.T) (advance func(time.Duration)) {
	t.Helper()
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	runClock = func() time.Time { return now }
	t.Cleanup(func() { runClock = time.Now })
	return func(d time.Duration) { now = now.Add(d) }
}

func codexBash(projectDir, toolUseID, command string) *CloudEvent {
	return &CloudEvent{SessionID: runsSession, CWD: projectDir, ToolName: "Bash", ToolUseID: toolUseID,
		ToolInput: map[string]any{"command": command}, Harness: HarnessCodex}
}

func codexPost(t *testing.T, pre *CloudEvent, output string) {
	t.Helper()
	post := *pre
	post.ToolResult = map[string]any{"output": output}
	if _, err := PostToolUse(&post); err != nil {
		t.Fatal(err)
	}
}

func pendingCodexRuns(t *testing.T, projectDir string) []runledger.Event {
	t.Helper()
	return readPendingCodexRuns(codexPendingPath(projectDir, runsSession))
}

func TestCodexRuns_StartGoesToScratchNotTheLedger(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	codexRunClock(t)
	if _, err := PreToolUse(codexBash(projectDir, "exec-1", "go test ./...")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runledger.NewStore(wipnoteDir).Dir()); !os.IsNotExist(err) {
		t.Error("a Codex start wrote the tracked runs ledger")
	}
	pending := pendingCodexRuns(t, projectDir)
	if len(pending) != 1 || pending[0].ToolUseID != "exec-1" || pending[0].Command != "go test ./..." {
		t.Fatalf("scratch = %+v, want the one start", pending)
	}
	if rel, _ := filepath.Rel(projectDir, codexPendingPath(projectDir, runsSession)); filepath.Dir(filepath.Dir(rel)) != ".wipnote/sessions" {
		t.Errorf("scratch at %s, want under the gitignored sessions folder", rel)
	}
}

func TestCodexRuns_FastCallIsDropped(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	advance := codexRunClock(t)
	pre := codexBash(projectDir, "exec-1", "ls")
	PreToolUse(pre)
	advance(codexMinRunSeconds*time.Second - time.Second)
	codexPost(t, pre, "a\nb\n")
	if recs := runRecords(t, wipnoteDir, false); len(recs) != 0 {
		t.Fatalf("fast call recorded: %+v", recs)
	}
	if p := pendingCodexRuns(t, projectDir); len(p) != 0 {
		t.Errorf("scratch entry not dropped: %+v", p)
	}
}

func TestCodexRuns_SlowCallIsPromoted(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	advance := codexRunClock(t)
	pre := codexBash(projectDir, "exec-1", "go test ./...")
	PreToolUse(pre)
	started := runClock()
	advance(codexMinRunSeconds * time.Second)
	codexPost(t, pre, "ok  core\nerror: vet found issues\nFAIL\n")
	recs := runRecords(t, wipnoteDir, false)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want the promoted run", len(recs))
	}
	r := recs[0]
	if !r.StartedAt.Equal(started) || !r.EndedAt.Equal(runClock()) || r.Command != "go test ./..." {
		t.Errorf("promoted run = %+v", r)
	}
	if r.Status != runledger.StatusCompleted || r.Exit != nil {
		t.Errorf("status/exit = %s/%v, want completed with no exit code", r.Status, r.Exit)
	}
	if r.FirstError != "error: vet found issues" || r.LastLine != "FAIL" {
		t.Errorf("summary = %q / %q", r.FirstError, r.LastLine)
	}
	if p := pendingCodexRuns(t, projectDir); len(p) != 0 {
		t.Errorf("scratch entry kept after promotion: %+v", p)
	}
}

func TestCodexRuns_ThresholdFromConfig(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	os.WriteFile(filepath.Join(wipnoteDir, "config.json"), []byte(`{"runs":{"codexMinSeconds":2}}`), 0o644)
	advance := codexRunClock(t)
	pre := codexBash(projectDir, "exec-1", "sleep 3")
	PreToolUse(pre)
	advance(3 * time.Second)
	codexPost(t, pre, "")
	if recs := runRecords(t, wipnoteDir, false); len(recs) != 1 {
		t.Fatalf("3s call with a 2s threshold: %d records, want 1", len(recs))
	}
}

// With no pending start (the PreToolUse was lost), the payload's duration
// decides, and the start is back-dated by it.
func TestCodexRuns_NoPendingStartUsesDuration(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	codexRunClock(t)
	slow := codexBash(projectDir, "exec-9", "make")
	slow.DurationMS = durationMS(codexMinRunSeconds * 1000)
	codexPost(t, slow, "done\n")
	fast := codexBash(projectDir, "exec-10", "true")
	fast.DurationMS = 50
	codexPost(t, fast, "")
	recs := runRecords(t, wipnoteDir, false)
	if len(recs) != 1 || recs[0].Command != "make" || recs[0].StartedAt.IsZero() {
		t.Fatalf("records = %+v, want only the slow call with a start", recs)
	}
}

func TestCodexRuns_ScratchIsCappedAndPrunedAtSessionEnd(t *testing.T) {
	projectDir, _ := runsProject(t)
	codexRunClock(t)
	for i := 0; i < codexPendingMax+5; i++ {
		PreToolUse(codexBash(projectDir, fmt.Sprintf("exec-%d", i), "x"))
	}
	pending := pendingCodexRuns(t, projectDir)
	if len(pending) != codexPendingMax {
		t.Fatalf("scratch holds %d, want the newest %d", len(pending), codexPendingMax)
	}
	if pending[0].ToolUseID != "exec-5" {
		t.Errorf("oldest kept start = %s, want exec-5", pending[0].ToolUseID)
	}
	SessionEnd(&CloudEvent{SessionID: runsSession, CWD: projectDir, Harness: HarnessCodex}, projectDir)
	if _, err := os.Stat(codexPendingPath(projectDir, runsSession)); !os.IsNotExist(err) {
		t.Error("scratch survived the session end")
	}
}

func TestCodexRuns_ClaudeBashNeverUsesScratch(t *testing.T) {
	projectDir, _ := runsProject(t)
	pre := codexBash(projectDir, "toolu_1", "ls")
	pre.Harness = HarnessClaude
	PreToolUse(pre)
	if _, err := os.Stat(codexPendingPath(projectDir, runsSession)); !os.IsNotExist(err) {
		t.Error("a Claude call wrote the Codex scratch")
	}
}
