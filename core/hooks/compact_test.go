package hooks

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sessionFile(projectDir, name string) string {
	return filepath.Join(projectDir, ".wipnote", "sessions", canonGuardSession, name)
}

func TestPreCompact_WithActiveItem(t *testing.T) {
	projectDir := briefProject(t, []string{"+Read the docs", "Write the builder"}, briefContent, briefQuestions)
	ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, Trigger: "auto", Harness: HarnessClaude}

	out := PreCompact(ev, projectDir)
	for _, want := range []string{briefItemID, "Keep the thread", "Write the builder", "decisions", "files changed", "next step"} {
		if !strings.Contains(out, want) {
			t.Errorf("instructions missing %q:\n%s", want, out)
		}
	}
	if len(out) >= 10000 {
		t.Errorf("instructions are %d chars; Claude Code cuts PreCompact stdout at 10,000", len(out))
	}
	snap, err := os.ReadFile(sessionFile(projectDir, "compact-brief.md"))
	if err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}
	if string(snap) != BuildResumeBrief(projectDir, canonGuardSession) {
		t.Errorf("snapshot differs from the brief:\n%s", snap)
	}
}

func TestPreCompact_NoActiveItem(t *testing.T) {
	projectDir := canonGuardProject(t)
	ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, Trigger: "manual", Harness: HarnessClaude}
	if out := PreCompact(ev, projectDir); out != "" {
		t.Fatalf("no active item: want no instructions, got %q", out)
	}
	if _, err := os.Stat(sessionFile(projectDir, "compact-brief.md")); !os.IsNotExist(err) {
		t.Fatalf("no active item: want no snapshot, stat err = %v", err)
	}
}

// A snapshot that cannot be written must not stop the instructions or the
// compaction: the session folder here is a regular file.
func TestPreCompact_UnwritableSnapshotStillAllows(t *testing.T) {
	projectDir := briefProject(t, []string{"Write the builder"}, briefContent, "")
	sessions := filepath.Join(projectDir, ".wipnote", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, canonGuardSession), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, Trigger: "auto", Harness: HarnessClaude}
	if out := PreCompact(ev, projectDir); !strings.Contains(out, briefItemID) {
		t.Fatalf("instructions must survive a snapshot failure, got %q", out)
	}
}

func TestSessionStart_CompactReturnsBrief(t *testing.T) {
	projectDir := briefProject(t, []string{"Write the builder"}, briefContent, briefQuestions)
	t.Setenv("WIPNOTE_CONTINUE_HANDOFF_B64", "")

	compact := sessionStartResultForSource("compact", projectDir, canonGuardSession)
	brief := BuildResumeBrief(projectDir, canonGuardSession)
	if !strings.Contains(compact.AdditionalContext, brief) {
		t.Errorf("compact context must carry the brief:\n%s", compact.AdditionalContext)
	}
	if strings.Contains(compact.AdditionalContext, "wipnote plugin is active") {
		t.Errorf("compact context must replace the startup guidance:\n%s", compact.AdditionalContext)
	}
	if len(compact.AdditionalContext) >= 8000 {
		t.Errorf("compact context is %d chars, want under 8000", len(compact.AdditionalContext))
	}
}

// startup, resume, clear and a compact with no active item stay byte-identical
// to today's text, so a resumed session's prompt cache still matches.
func TestSessionStart_OtherSourcesUnchanged(t *testing.T) {
	projectDir := briefProject(t, []string{"Write the builder"}, briefContent, "")
	t.Setenv("WIPNOTE_CONTINUE_HANDOFF_B64", "")
	baseline := sessionStartResult(projectDir).AdditionalContext
	if !strings.Contains(baseline, "wipnote plugin is active") {
		t.Fatalf("baseline lost the startup guidance: %q", baseline)
	}
	for _, source := range []string{"startup", "resume", "clear", "fork", ""} {
		got := sessionStartResultForSource(source, projectDir, canonGuardSession).AdditionalContext
		if got != baseline {
			t.Errorf("source %q changed the injected text:\n%s", source, got)
		}
	}

	empty := canonGuardProject(t)
	want := sessionStartResult(empty).AdditionalContext
	if got := sessionStartResultForSource("compact", empty, canonGuardSession).AdditionalContext; got != want {
		t.Errorf("compact with no active item must behave as today:\n%s", got)
	}
}

func TestPostCompact_AppendsRecord(t *testing.T) {
	projectDir := canonGuardProject(t)
	for _, trigger := range []string{"manual", "auto"} {
		ev := &CloudEvent{SessionID: canonGuardSession, CWD: projectDir, Trigger: trigger,
			CompactSummary: "summary after " + trigger, Harness: HarnessClaude}
		res, err := PostCompact(ev, projectDir)
		if err != nil || res == nil {
			t.Fatalf("PostCompact must never fail: %v %v", res, err)
		}
	}
	f, err := os.Open(sessionFile(projectDir, "compactions.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var row map[string]any
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("line is not one JSON object: %q: %v", sc.Text(), err)
		}
		rows = append(rows, row)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 records, got %d", len(rows))
	}
	if rows[0]["trigger"] != "manual" || rows[0]["compact_summary"] != "summary after manual" || rows[1]["trigger"] != "auto" {
		t.Errorf("records wrong: %v", rows)
	}
	ts, _ := rows[0]["timestamp"].(string)
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil || !strings.HasSuffix(ts, "Z") || parsed.IsZero() {
		t.Errorf("timestamp %q is not a UTC RFC 3339 time (%v)", ts, err)
	}
}

func TestPostCompact_NeverFails(t *testing.T) {
	projectDir := canonGuardProject(t)
	if res, err := PostCompact(&CloudEvent{CWD: projectDir, Trigger: "auto"}, projectDir); err != nil || res == nil {
		t.Fatalf("no session id: want a result and no error, got %v %v", res, err)
	}
}

// Fixtures copied from https://code.claude.com/docs/en/hooks (PreCompact input,
// PostCompact input) and https://learn.chatgpt.com/docs/hooks (field names).
func TestCompactPayloadDecoding(t *testing.T) {
	const claudePre = `{
  "session_id": "abc123",
  "transcript_path": "/Users/.../.claude/projects/.../00893aaf-19fa-41d2-8238-13269b9b3ca0.jsonl",
  "cwd": "/Users/...",
  "hook_event_name": "PreCompact",
  "trigger": "manual",
  "custom_instructions": null
}`
	const claudePost = `{
  "session_id": "abc123",
  "transcript_path": "/Users/.../.claude/projects/.../00893aaf-19fa-41d2-8238-13269b9b3ca0.jsonl",
  "cwd": "/Users/...",
  "hook_event_name": "PostCompact",
  "trigger": "manual",
  "compact_summary": "Summary of the compacted conversation..."
}`
	const codexPre = `{"session_id":"abc123","cwd":"/work","hook_event_name":"PreCompact","turn_id":"turn-1","trigger":"auto"}`

	ev, err := ParseEventForHarness(HarnessClaude, []byte(claudePre))
	if err != nil || ev.Trigger != "manual" || ev.CustomInstructions != "" {
		t.Errorf("claude PreCompact: %+v %v", ev, err)
	}
	ev, err = ParseEventForHarness(HarnessClaude, []byte(`{"hook_event_name":"PreCompact","trigger":"manual","custom_instructions":"focus on the parser"}`))
	if err != nil || ev.CustomInstructions != "focus on the parser" {
		t.Errorf("claude PreCompact custom_instructions: %+v %v", ev, err)
	}
	ev, err = ParseEventForHarness(HarnessClaude, []byte(claudePost))
	if err != nil || ev.Trigger != "manual" || ev.CompactSummary != "Summary of the compacted conversation..." {
		t.Errorf("claude PostCompact: %+v %v", ev, err)
	}
	ev, err = ParseEventForHarness(HarnessCodex, []byte(codexPre))
	if err != nil || ev.Trigger != "auto" || ev.TurnID != "turn-1" {
		t.Errorf("codex PreCompact: %+v %v", ev, err)
	}
}
