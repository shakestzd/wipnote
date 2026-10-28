package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// delegateWorker makes a temp project with a run folder, a session.json
// naming worker w1 and its mailbox, and sets the worker env.
func delegateWorker(t *testing.T) (project, runDir, mail string) {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	os.Unsetenv("WIPNOTE_PROJECT_DIR")
	os.Unsetenv("CLAUDE_PROJECT_DIR")
	project = t.TempDir()
	run := "dlg-0123456789ab"
	runDir = delegateRunDir(t, project, run)
	if err := os.WriteFile(filepath.Join(runDir, "session.json"), []byte(`{"run":"`+run+`","name":"w1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mail = filepath.Join(project, ".wipnote", "delegate", "mail", "w1")
	for _, f := range []string{"inbox", "outbox", "ack"} {
		if err := os.MkdirAll(filepath.Join(mail, f), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(DelegateRunEnv, run)
	return project, runDir, mail
}

func postInbox(t *testing.T, mail string, seq int, id, typ string) {
	t.Helper()
	body := fmt.Sprintf(`{"v":1,"id":%q,"type":%q,"seq":%d,"body":"x"}`, id, typ, seq)
	if err := os.WriteFile(filepath.Join(mail, "inbox", fmt.Sprintf("%06d-%s.json", seq, id)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func eventsOf(t *testing.T, runDir string) []map[string]any {
	t.Helper()
	return readDelegateLines(t, filepath.Join(runDir, "events-sid-1.ndjson"))
}

func TestDelegateEvents_StopRecordsTurnEndOnly(t *testing.T) {
	project, runDir, _ := delegateWorker(t)
	ev := &CloudEvent{SessionID: "sid-1", Prompt: "SECRET-PROMPT", PromptResponse: "SECRET-ANSWER", TurnID: "t9"}
	recordDelegateHook(ev, project, "sid-1", "prompt")
	recordDelegateHook(ev, project, "sid-1", "stop")
	data, _ := os.ReadFile(filepath.Join(runDir, "events-sid-1.ndjson"))
	if strings.Contains(string(data), "SECRET") {
		t.Fatalf("message text stored: %s", data)
	}
	lines := eventsOf(t, runDir)
	if len(lines) != 2 || lines[1]["kind"] != "stop" {
		t.Fatalf("%v", lines)
	}
	for k := range lines[1] {
		switch k {
		case "v", "seq", "ts", "run", "kind", "sid":
		default:
			t.Fatalf("stop line carries %q: %v", k, lines[1])
		}
	}
}

func TestDelegateEvents_AskCaptured(t *testing.T) {
	project, runDir, _ := delegateWorker(t)
	ask := &CloudEvent{SessionID: "sid-1", ToolName: "AskUserQuestion", ToolUseID: "tu-ask", ToolInput: map[string]any{
		"questions": []any{map[string]any{"question": "Which file?", "options": []any{
			map[string]any{"label": "a.go"}, map[string]any{"label": "b.go"}}}},
	}}
	recordDelegateToolStart(ask, project, "sid-1")
	recordDelegateToolStart(&CloudEvent{ToolName: "Bash", ToolUseID: "tu-b", ToolInput: map[string]any{"command": "ls"}}, project, "sid-1")
	recordDelegateToolEnd(&CloudEvent{ToolName: "Bash", ToolUseID: "tu-b"}, project, "sid-1")
	lines := eventsOf(t, runDir)
	if len(lines) != 3 {
		t.Fatalf("%v", lines)
	}
	q := lines[0]
	if q["kind"] != "question" || q["tool"] != "AskUserQuestion" || q["tool_use_id"] != "tu-ask" || q["summary"] != "Which file? [a.go | b.go]" {
		t.Fatalf("ask %v", q)
	}
	if lines[1]["kind"] != "tool_start" || lines[1]["tool"] != "Bash" || lines[2]["kind"] != "tool_end" || lines[2]["tool_use_id"] != "tu-b" {
		t.Fatalf("%v", lines)
	}
	// A permission prompt that reaches the person is recorded; an
	// auto-allowed read-only query never shows a prompt and is not.
	if err := os.MkdirAll(filepath.Join(project, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	perm := &CloudEvent{SessionID: "sid-1", CWD: project, ToolName: "Bash", ToolInput: map[string]any{"command": "rm -rf build"}}
	if _, err := PermissionRequest(perm); err != nil {
		t.Fatal(err)
	}
	allowed := &CloudEvent{SessionID: "sid-1", CWD: project, ToolName: "Bash", ToolInput: map[string]any{"command": "wipnote status"}}
	if res, _ := PermissionRequest(allowed); res.HookSpecificOutput == nil {
		t.Fatal("fixture: wipnote status is not auto-allowed")
	}
	lines = eventsOf(t, runDir)
	if len(lines) != 4 || lines[3]["kind"] != "permission_request" || !strings.Contains(fmt.Sprint(lines[3]["summary"]), "rm -rf build") {
		t.Fatalf("%v", lines)
	}
}

func TestDelegateEvents_PromptAcksNewestTask(t *testing.T) {
	project, runDir, mail := delegateWorker(t)
	postInbox(t, mail, 1, "msg-00000000000001", "task")
	postInbox(t, mail, 2, "msg-00000000000002", "task")
	postInbox(t, mail, 3, "msg-00000000000003", "note")
	recordDelegateHook(&CloudEvent{SessionID: "sid-1"}, project, "sid-1", "prompt")
	if _, err := os.Lstat(filepath.Join(mail, "ack", "msg-00000000000002")); err != nil {
		t.Fatalf("newest task not acked: %v", err)
	}
	for _, id := range []string{"msg-00000000000001", "msg-00000000000003"} {
		if _, err := os.Lstat(filepath.Join(mail, "ack", id)); err == nil {
			t.Fatalf("%s acked", id)
		}
	}
	if lines := eventsOf(t, runDir); len(lines) != 1 || lines[0]["kind"] != "prompt" {
		t.Fatalf("%v", lines)
	}
	// Other side: a stop never acks.
	postInbox(t, mail, 4, "msg-00000000000004", "task")
	recordDelegateHook(&CloudEvent{SessionID: "sid-1"}, project, "sid-1", "stop")
	if _, err := os.Lstat(filepath.Join(mail, "ack", "msg-00000000000004")); err == nil {
		t.Fatal("stop acked a task")
	}
}

func TestDelegateEvents_FailOpen(t *testing.T) {
	project, runDir, mail := delegateWorker(t)
	postInbox(t, mail, 1, "msg-00000000000001", "task")
	// Outside a worker: nothing at all.
	os.Unsetenv(DelegateRunEnv)
	recordDelegateToolStart(&CloudEvent{ToolName: "Bash", ToolUseID: "x"}, project, "sid-1")
	recordDelegateHook(&CloudEvent{}, project, "sid-1", "prompt")
	if _, err := os.Stat(filepath.Join(runDir, "events-sid-1.ndjson")); err == nil {
		t.Fatal("wrote outside a worker")
	}
	t.Setenv(DelegateRunEnv, "dlg-0123456789ab")
	// A symlinked ack folder and an unreadable session.json write no ack.
	ack := filepath.Join(mail, "ack")
	_ = os.Remove(ack)
	other := t.TempDir()
	if err := os.Symlink(other, ack); err != nil {
		t.Fatal(err)
	}
	recordDelegateHook(&CloudEvent{}, project, "sid-1", "prompt")
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Fatalf("acked through a symlink: %v", entries)
	}
	_ = os.WriteFile(filepath.Join(runDir, "session.json"), []byte("{not json"), 0o600)
	nil1 := &CloudEvent{ToolName: "AskUserQuestion", ToolInput: map[string]any{"questions": "not a list"}}
	start := time.Now()
	recordDelegateHook(&CloudEvent{}, project, "sid-1", "prompt")
	recordDelegateToolStart(nil1, project, "sid-1")
	recordDelegateToolStart(nil, project, "sid-1")
	recordDelegateToolEnd(nil, project, "sid-1")
	recordDelegatePermission(nil)
	// A huge question is bounded.
	long := strings.Repeat("é", 10000)
	recordDelegateToolStart(&CloudEvent{ToolName: "AskUserQuestion", ToolUseID: "tu", ToolInput: map[string]any{
		"questions": []any{map[string]any{"question": long}}}}, project, "sid-1")
	if time.Since(start) > time.Second {
		t.Fatalf("hooks took %s", time.Since(start))
	}
	lines := eventsOf(t, runDir)
	last := lines[len(lines)-1]
	if s, _ := last["summary"].(string); last["kind"] != "question" || len([]rune(s)) > delegateSummaryMax+1 {
		t.Fatalf("summary not bounded: %d runes", len([]rune(fmt.Sprint(last["summary"]))))
	}
}
