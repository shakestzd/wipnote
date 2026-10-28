package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/runledger"
)

const runsSession = "019ee380-abcd-7000-8000-0000000072a5"

func runsProject(t *testing.T) (projectDir, wipnoteDir string) {
	t.Helper()
	projectDir = canonGuardProject(t)
	return projectDir, filepath.Join(projectDir, ".wipnote")
}

func claimFor(t *testing.T, wipnoteDir, item string) {
	t.Helper()
	_, _, err := claimledger.NewStore(wipnoteDir).Open(runsSession, claimledger.Episode{WorkItemID: item,
		SessionID: runsSession, RootSessionID: runsSession, AgentID: db.AgentRootSentinel, StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
}

func runRecords(t *testing.T, wipnoteDir string, ended bool) []runledger.Record {
	t.Helper()
	recs, err := runledger.NewStore(wipnoteDir).Records(func(string) bool { return ended })
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func claudePost(projectDir, tool, toolUseID string, input, result map[string]any) *CloudEvent {
	return &CloudEvent{SessionID: runsSession, CWD: projectDir, ToolName: tool, ToolUseID: toolUseID,
		ToolInput: input, ToolResult: result, Harness: HarnessClaude}
}

func backgroundBash(projectDir, toolUseID, taskID, desc string) *CloudEvent {
	return claudePost(projectDir, "Bash", toolUseID,
		map[string]any{"command": "cd " + projectDir + " && scripts/gate.sh", "description": desc, "run_in_background": true},
		map[string]any{"stdout": "", "stderr": "", "backgroundTaskId": taskID})
}

// notificationLine is a transcript enqueue line in the recorded shape.
func notificationLine(taskID, toolUseID, outputFile, status, summary string) string {
	content := "<task-notification>\n<task-id>" + taskID + "</task-id>\n<tool-use-id>" + toolUseID +
		"</tool-use-id>\n<output-file>" + outputFile + "</output-file>\n<status>" + status +
		"</status>\n<summary>" + summary + "</summary>\n</task-notification>"
	return `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-10-27T16:35:00.000Z","content":` +
		jsonString(content) + "}\n"
}

func jsonString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(s)
}

// notifiedTranscript writes a task output file and a transcript holding one
// ordinary line and the task's completion notification.
func notifiedTranscript(t *testing.T, taskID, toolUseID, output, summary string) string {
	t.Helper()
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "tasks", taskID+".output")
	os.MkdirAll(filepath.Dir(outPath), 0o755)
	os.WriteFile(outPath, []byte(output), 0o644)
	transcript := filepath.Join(tmp, "session.jsonl")
	appendFile(t, transcript, `{"type":"user","message":{"content":"hi"}}`+"\n")
	appendFile(t, transcript, notificationLine(taskID, toolUseID, outPath, "completed", summary))
	return transcript
}

func TestRuns_BackgroundBashStartPlusNotificationIsComplete(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	claimFor(t, wipnoteDir, "feat-c0ffee01")
	PostToolUseRecord(backgroundBash(projectDir, "toolu_A", "bwylhfzv1", "Rebuild and run the full gate"))

	recs := runRecords(t, wipnoteDir, false)
	if len(recs) != 1 || recs[0].Status != runledger.StatusRunning {
		t.Fatalf("after start: %+v, want one running run", recs)
	}
	r := recs[0]
	if r.Kind != runledger.KindShell || r.TaskID != "bwylhfzv1" || r.WorkItem != "feat-c0ffee01" || r.Session != runsSession {
		t.Errorf("start record = %+v", r)
	}
	if r.Command != "cd . && scripts/gate.sh" || r.Folder != "." {
		t.Errorf("command/folder = %q/%q, want relative to the project", r.Command, r.Folder)
	}

	transcript := notifiedTranscript(t, "bwylhfzv1", "toolu_A", "--- FAIL: TestFlaky (0.01s)\nretry ok\ngate done\n",
		`Background command "Rebuild and run the full gate" completed (exit code 0)`)

	if _, err := Stop(&CloudEvent{SessionID: runsSession, CWD: projectDir, TranscriptPath: transcript, Harness: HarnessClaude}); err != nil {
		t.Fatal(err)
	}
	recs = runRecords(t, wipnoteDir, false)
	if len(recs) != 1 {
		t.Fatalf("after end: %d records, want 1", len(recs))
	}
	r = recs[0]
	if r.Status != runledger.StatusCompleted || r.Exit == nil || *r.Exit != 0 || r.EndedAt.IsZero() {
		t.Errorf("end = %s exit %v at %v", r.Status, r.Exit, r.EndedAt)
	}
	if r.LastLine != "gate done" || r.FirstError != "--- FAIL: TestFlaky (0.01s)" {
		t.Errorf("output summary = %q / %q", r.LastLine, r.FirstError)
	}
	if r.LogPath != "…/tasks/bwylhfzv1.output" {
		t.Errorf("log path = %q", r.LogPath)
	}

	// A second pass over the same transcript adds nothing.
	before, _ := runledger.NewStore(wipnoteDir).ReadAll()
	UserPrompt(&CloudEvent{SessionID: runsSession, CWD: projectDir, TranscriptPath: transcript, Prompt: "next", Harness: HarnessClaude})
	after, _ := runledger.NewStore(wipnoteDir).ReadAll()
	if len(after) != len(before) {
		t.Errorf("re-reading the transcript appended %d events", len(after)-len(before))
	}
}

func TestRuns_NotificationArrivingAtPromptWithNonzeroExit(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	PostToolUseRecord(backgroundBash(projectDir, "toolu_B", "b2", "unit tests"))
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, transcript, notificationLine("b2", "toolu_B", "/nonexistent/b2.output", "failed",
		`Background command "unit tests" failed with exit code 1`))
	UserPrompt(&CloudEvent{SessionID: runsSession, CWD: projectDir, TranscriptPath: transcript, Prompt: "go on", Harness: HarnessClaude})

	recs := runRecords(t, wipnoteDir, false)
	if len(recs) != 1 || recs[0].Status != runledger.StatusFailed || recs[0].Exit == nil || *recs[0].Exit != 1 {
		t.Fatalf("records = %+v, want one failed run with exit 1", recs)
	}
	// No item was active: the run is linked to the session only.
	if recs[0].WorkItem != "" || recs[0].Session != runsSession {
		t.Errorf("links = item %q session %q", recs[0].WorkItem, recs[0].Session)
	}
}

func TestRuns_ForegroundBashIsNotARun(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	PostToolUseRecord(claudePost(projectDir, "Bash", "toolu_F",
		map[string]any{"command": "ls"}, map[string]any{"stdout": "a\nb\n"}))
	PostToolUseRecord(claudePost(projectDir, "Read", "toolu_R",
		map[string]any{"file_path": "x"}, map[string]any{"output": "x"}))
	if recs := runRecords(t, wipnoteDir, false); len(recs) != 0 {
		t.Fatalf("foreground calls recorded as runs: %+v", recs)
	}
}

func TestRuns_BashMovedToBackgroundIsARun(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	// No run_in_background flag: the command was backgrounded after it started.
	PostToolUseRecord(claudePost(projectDir, "Bash", "toolu_M",
		map[string]any{"command": "make all"}, map[string]any{"stdout": "", "backgroundTaskId": "bm1"}))
	recs := runRecords(t, wipnoteDir, false)
	if len(recs) != 1 || recs[0].TaskID != "bm1" || recs[0].Label() != "make all" {
		t.Fatalf("records = %+v", recs)
	}
}

func TestRuns_SubagentAndMonitorStarts(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	PostToolUseRecord(claudePost(projectDir, "Agent", "toolu_G",
		map[string]any{"description": "Design critic", "prompt": "review", "run_in_background": true},
		map[string]any{"isAsync": true, "status": "async_launched", "agentId": "ae62a0364fd7ac389"}))
	PostToolUseRecord(claudePost(projectDir, "Monitor", "toolu_W",
		map[string]any{"command": "until test -f done; do sleep 5; done", "description": "gate watch"},
		map[string]any{"output": "Monitor started (task bv1f3g9v6, expires in 30m unless the source ends first)."}))
	recs := runRecords(t, wipnoteDir, true)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(recs), recs)
	}
	byKind := map[string]runledger.Record{}
	for _, r := range recs {
		byKind[r.Kind] = r
	}
	if a := byKind[runledger.KindSubagent]; a.TaskID != "ae62a0364fd7ac389" || a.Description != "Design critic" {
		t.Errorf("subagent = %+v", a)
	}
	if m := byKind[runledger.KindMonitor]; m.TaskID != "bv1f3g9v6" || m.Description != "gate watch" {
		t.Errorf("monitor = %+v", m)
	}
	// The session is over and no notification came: unknown, not running.
	for _, r := range recs {
		if r.Status != runledger.StatusUnknown {
			t.Errorf("%s status = %s, want unknown", r.Kind, r.Status)
		}
	}
}

func TestRuns_SyncSubagentRecordsStartAndEnd(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	ev := claudePost(projectDir, "Agent", "toolu_S",
		map[string]any{"description": "quick look", "prompt": "x"},
		map[string]any{"status": "completed", "agentId": "a1"})
	ev.DurationMS = 90000
	PostToolUseRecord(ev)
	recs := runRecords(t, wipnoteDir, false)
	if len(recs) != 1 || recs[0].Status != runledger.StatusCompleted || !recs[0].StartedAt.Before(recs[0].EndedAt) {
		t.Fatalf("records = %+v, want one completed run spanning its duration", recs)
	}
}

func TestRuns_HooksWriteNoDatabase(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	PostToolUseRecord(backgroundBash(projectDir, "toolu_D", "bd", "gate"))
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, transcript, notificationLine("bd", "toolu_D", "/none", "completed", `Background command "gate" completed (exit code 0)`))
	SyncTranscriptRuns(projectDir, runsSession, transcript)
	filepath.Walk(wipnoteDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && (strings.HasSuffix(p, ".db") || strings.Contains(p, ".sqlite")) {
			t.Errorf("hook wrote a database file: %s", p)
		}
		return nil
	})
}

func savedRunOffset(projectDir string) int64 {
	return readRunOffset(filepath.Join(projectDir, ".wipnote", "sessions", runsSession, runOffsetFile))
}

// UserPromptSubmit is synchronous: one pass reads at most the prompt cap;
// Stop may read up to its larger cap.
func TestRuns_PromptPassReadsAtMostItsCap(t *testing.T) {
	projectDir, _ := runsProject(t)
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	line := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 1000) + `"}}` + "\n"
	appendFile(t, transcript, strings.Repeat(line, int(2*runPromptChunkBytes)/len(line)+10))

	UserPrompt(&CloudEvent{SessionID: runsSession, CWD: projectDir, TranscriptPath: transcript, Prompt: "go", Harness: HarnessClaude})
	first := savedRunOffset(projectDir)
	if first <= 0 || first > runPromptChunkBytes {
		t.Fatalf("prompt pass offset %d, want in (0, %d]", first, runPromptChunkBytes)
	}
	Stop(&CloudEvent{SessionID: runsSession, CWD: projectDir, TranscriptPath: transcript, Harness: HarnessClaude})
	if got := savedRunOffset(projectDir) - first; got <= runPromptChunkBytes || got > runStopChunkBytes {
		t.Fatalf("stop pass read %d bytes, want more than the prompt cap and at most %d", got, runStopChunkBytes)
	}
}

// A window full of notifications costs one output-file read each, so the
// prompt pass also caps ends; the rest are picked up by later passes, none
// lost and none twice.
func TestRuns_PromptPassCapsEndsAndResumes(t *testing.T) {
	projectDir, wipnoteDir := runsProject(t)
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	const total = 20
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("b%02d", i)
		appendFile(t, transcript, notificationLine(id, "toolu_"+id, "/none/"+id, "completed", `Background command "x" completed (exit code 0)`))
	}
	prompt := &CloudEvent{SessionID: runsSession, CWD: projectDir, TranscriptPath: transcript, Prompt: "go", Harness: HarnessClaude}
	UserPrompt(prompt)
	if n := len(runRecords(t, wipnoteDir, false)); n != runPromptMaxEnds {
		t.Fatalf("first prompt pass recorded %d ends, want %d", n, runPromptMaxEnds)
	}
	for i := 0; i < total; i++ {
		UserPrompt(prompt)
	}
	events, _ := runledger.NewStore(wipnoteDir).ReadAll()
	if len(events) != total || len(runRecords(t, wipnoteDir, false)) != total {
		t.Fatalf("after catching up: %d events, want %d distinct ends", len(events), total)
	}
}
