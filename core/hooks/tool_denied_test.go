package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Denied tool calls count as failed, never aborted (feat-ddeaa0d1). A denied
// call fires PreToolUse (so it has a start line) but neither PostToolUse nor
// PostToolUseFailure, so without these records the session-start sweep would
// call it aborted.

// completionsFor returns the completion statuses the tool log holds for one
// call, in order.
func completionsFor(t *testing.T, projectDir, sessionID, toolUseID string) []string {
	t.Helper()
	data, err := os.ReadFile(canonicalToolLogPath(projectDir, sessionID))
	if err != nil {
		t.Fatalf("read tool log: %v", err)
	}
	id := toolCallEventID(sessionID, toolUseID)
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		var rec toolLogLine
		if json.Unmarshal([]byte(line), &rec) == nil && rec.ID == id && rec.Done != "" {
			out = append(out, rec.Done)
		}
	}
	return out
}

func statusInHTML(t *testing.T, projectDir, sessionID, toolUseID, status string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html"))
	if err != nil {
		t.Fatalf("read session html: %v", err)
	}
	id := regexp.QuoteMeta(toolCallEventID(sessionID, toolUseID))
	return regexp.MustCompile(`data-event-id="` + id + `"[^>]*data-status="` + status + `"`).Match(b)
}

// markOwnerDead points the session's .session-pid at a pid that cannot exist.
func markOwnerDead(t *testing.T, h *splitDBHarness) {
	t.Helper()
	writeSessionPIDRaw(t, filepath.Join(h.projectDir, ".wipnote", "sessions", h.sessionID), "2147483647\n1")
}

// sweepAndCheck runs the session-start sweep from another session and checks
// which of the session's calls ended up with which statuses.
func sweepAndCheck(t *testing.T, h *splitDBHarness, now time.Time, want map[string][]string) {
	t.Helper()
	SweepOrphanedToolCalls(h.projectDir, "denied-sweeper-1", now, 0)
	for tu, statuses := range want {
		got := completionsFor(t, h.projectDir, h.sessionID, tu)
		if strings.Join(got, ",") != strings.Join(statuses, ",") {
			t.Errorf("%s completions = %v, want %v", tu, got, statuses)
		}
	}
}

// TestPermissionDenied_WritesFailedNotAborted: the PermissionDenied hook (auto
// mode) writes a failed line by tool_use_id, so the sweep leaves the call
// alone; a genuinely unfinished call in the same dead-owner session is still
// aborted, and a normal success is untouched.
func TestPermissionDenied_WritesFailedNotAborted(t *testing.T) {
	h := newSplitDBHarness(t, "perm-denied-1")
	now := time.Now().UTC()
	old := now.Add(-10 * time.Minute)
	for _, tu := range []string{"tu_denied", "tu_orphan", "tu_ok"} {
		startToolAt(t, h, h.sessionID, tu, old)
	}
	finishTool(t, h, h.sessionID, "tu_ok")
	ev := &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, HookEventName: "PermissionDenied",
		ToolName: "Read", ToolUseID: "tu_denied", Reason: "[Irreversible Local Destruction]"}
	res, err := PermissionDenied(ev)
	if err != nil {
		t.Fatalf("PermissionDenied: %v", err)
	}
	if res == nil || res.HookSpecificOutput != nil {
		t.Errorf("PermissionDenied must observe only, got %+v", res)
	}
	markOwnerDead(t, h)
	sweepAndCheck(t, h, now, map[string][]string{
		"tu_denied": {"failed"}, "tu_orphan": {"aborted"}, "tu_ok": {"completed"},
	})
	if !statusInHTML(t, h.projectDir, h.sessionID, "tu_denied", "failed") {
		t.Error("denied call has no failed entry in the session HTML")
	}
	if abortedInHTML(t, h.projectDir, h.sessionID, "tu_denied") {
		t.Error("denied call was recorded as aborted")
	}
}

// writeTranscript writes a Claude Code transcript (JSONL) whose user entries
// carry the given tool_result blocks, preceded by filler entries.
func writeTranscript(t *testing.T, dir string, filler int, results ...map[string]any) string {
	t.Helper()
	var sb strings.Builder
	pad := strings.Repeat("x", 512)
	for i := 0; i < filler; i++ {
		line, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "text", "text": pad}}}})
		sb.Write(append(line, '\n'))
	}
	for _, r := range results {
		entry := map[string]any{"type": "user", "message": map[string]any{"role": "user",
			"content": []any{r["block"]}}}
		if kind, ok := r["denial"]; ok {
			entry["toolDenialKind"] = kind
		}
		line, _ := json.Marshal(entry)
		sb.Write(append(line, '\n'))
	}
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func toolResult(toolUseID string, isError bool, content any) map[string]any {
	block := map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": content}
	if isError {
		block["is_error"] = true
	}
	return map[string]any{"block": block}
}

// transcriptCase sets up four unfinished-or-finished calls and a transcript:
// a manual denial (string content), a cancelled approval (toolDenialKind), a
// success whose async completion line has not landed yet, and a call with no
// tool_result at all.
func transcriptCase(t *testing.T, h *splitDBHarness, filler int) (string, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	old := now.Add(-10 * time.Minute)
	for _, tu := range []string{"tu_denied", "tu_cancelled", "tu_ok", "tu_orphan", "tu_done"} {
		startToolAt(t, h, h.sessionID, tu, old)
	}
	finishTool(t, h, h.sessionID, "tu_done")
	cancelled := toolResult("tu_cancelled", true, "[Tool call not completed: an approval request was still unanswered]")
	cancelled["denial"] = "cancelled"
	return writeTranscript(t, t.TempDir(), filler,
		toolResult("tu_done", false, "ok"),
		toolResult("tu_denied", true, []any{map[string]any{"type": "text",
			"text": "The user doesn't want to proceed with this tool use. The tool use was rejected."}}),
		cancelled,
		toolResult("tu_ok", false, "fine"),
	), now
}

var transcriptWant = map[string][]string{
	"tu_denied": {"failed"}, "tu_cancelled": {"failed"}, "tu_ok": nil,
	"tu_orphan": {"aborted"}, "tu_done": {"completed"},
}

// TestStop_TranscriptDenialsCountAsFailed: Stop matches unfinished starts to
// error tool_results in the transcript tail and writes failed lines; the
// orphan with no result is still aborted by the sweep, and a success whose
// completion is still in flight gets nothing from Stop.
func TestStop_TranscriptDenialsCountAsFailed(t *testing.T) {
	h := newSplitDBHarness(t, "stop-denied-1")
	tp, now := transcriptCase(t, h, 0)
	ev := &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, HookEventName: "Stop",
		TranscriptPath: tp, Harness: HarnessClaude}
	if _, err := Stop(ev); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := completionsFor(t, h.projectDir, h.sessionID, "tu_ok"); len(got) != 0 {
		t.Errorf("Stop wrote %v for a successful call", got)
	}
	markOwnerDead(t, h)
	want := map[string][]string{}
	for k, v := range transcriptWant {
		want[k] = v
	}
	want["tu_ok"] = []string{"aborted"} // its completion never landed and the owner died
	sweepAndCheck(t, h, now, want)
	if !statusInHTML(t, h.projectDir, h.sessionID, "tu_denied", "failed") {
		t.Error("transcript denial has no failed entry in the session HTML")
	}
}

// TestSessionEnd_TranscriptDenialsCountAsFailed: SessionEnd does the same
// match before it writes the end note, so a closed session's denied calls are
// failed, while its genuinely unfinished calls are still aborted.
func TestSessionEnd_TranscriptDenialsCountAsFailed(t *testing.T) {
	h := newSplitDBHarness(t, "end-denied-1")
	tp, now := transcriptCase(t, h, 0)
	finishTool(t, h, h.sessionID, "tu_ok")
	ev := &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, HookEventName: "SessionEnd",
		TranscriptPath: tp, Reason: "prompt_input_exit", Harness: HarnessClaude}
	if _, err := SessionEnd(ev, h.projectDir); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
	want := map[string][]string{}
	for k, v := range transcriptWant {
		want[k] = v
	}
	want["tu_ok"] = []string{"completed"}
	sweepAndCheck(t, h, now.Add(time.Second), want)
}

// TestTranscriptFailures_Bounded: the scan reads only the transcript tail, a
// session with no unfinished start never opens the transcript, and a non-Claude
// harness is skipped.
func TestTranscriptFailures_Bounded(t *testing.T) {
	h := newSplitDBHarness(t, "bounded-denied-1")
	prev := transcriptTailBytes
	transcriptTailBytes = 4 << 10
	defer func() { transcriptTailBytes = prev }()
	startToolAt(t, h, h.sessionID, "tu_early", time.Now().Add(-time.Minute))
	tp := writeTranscript(t, t.TempDir(), 0, toolResult("tu_early", true, "rejected"))
	big := writeTranscript(t, t.TempDir(), 64, toolResult("tu_other", false, "ok"))
	early, _ := os.ReadFile(tp)
	tail, _ := os.ReadFile(big)
	if err := os.WriteFile(big, append(early, tail...), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := RecordTranscriptToolFailures(h.projectDir, h.sessionID, big); n != 0 {
		t.Errorf("a result before the tail was matched (%d); the scan must stay bounded", n)
	}
	if n := RecordTranscriptToolFailures(h.projectDir, h.sessionID, tp); n != 1 {
		t.Errorf("recorded %d failures from the tail, want 1", n)
	}
	if n := RecordTranscriptToolFailures(h.projectDir, h.sessionID, filepath.Join(t.TempDir(), "missing")); n != 0 {
		t.Errorf("no unfinished start left, yet recorded %d", n)
	}
	h2 := newSplitDBHarness(t, "bounded-denied-codex-1")
	startToolAt(t, h2, h2.sessionID, "tu_early", time.Now().Add(-time.Minute))
	tp2 := writeTranscript(t, t.TempDir(), 0, toolResult("tu_early", true, "rejected"))
	recordTranscriptFailuresFor(&CloudEvent{Harness: HarnessCodex, TranscriptPath: tp2}, h2.projectDir, h2.sessionID)
	if got := completionsFor(t, h2.projectDir, h2.sessionID, "tu_early"); len(got) != 0 {
		t.Errorf("codex transcript was scanned: %v", got)
	}
}
