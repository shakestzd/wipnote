package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// mailWorker opens an interactive row for w1 with a run folder and no pane,
// outside herdr.
func mailWorker(t *testing.T) (dir, run string) {
	t.Helper()
	dir = delegateSessionProject(t)
	t.Setenv("HERDR_ENV", "")
	row, err := delegationledger.NewStore(filepath.Join(dir, ".wipnote")).Open(delegationledger.Record{
		Mode: delegationledger.ModeInteractive, Harness: "claude", Name: "w1", WorkItem: "feat-0a1b2c3d",
	})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := runstore.New(dir)
	rf, _ := store.CreateRun(row.ID)
	if err := session.WriteInfo(rf, session.Info{Run: row.ID, Name: "w1", Harness: "claude", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	return dir, row.ID
}

func TestDelegateWait_TimeoutRequiredAndExit124(t *testing.T) {
	mailWorker(t)
	if _, err := execDelegate(t, "wait", "w1"); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("no --timeout: %v", err)
	}
	cmd := delegateWaitCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	code, err := runDelegateWait(cmd, "w1", 50*time.Millisecond)
	if err != nil || code != session.TimeoutExit {
		t.Fatalf("code %d err %v", code, err)
	}
	var res session.WaitResult
	if json.Unmarshal([]byte(out.String()), &res) != nil || res.Outcome != session.OutcomeTimeout {
		t.Fatalf("payload %q", out.String())
	}
}

func TestDelegateResultQuestionsInbox(t *testing.T) {
	_, run := mailWorker(t)
	out, err := execDelegate(t, "result", "w1")
	if err != nil || !strings.Contains(out, `"result": "none"`) {
		t.Fatalf("result %q err %v", out, err)
	}
	out, err = execDelegate(t, "questions", "w1")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("questions %q err %v", out, err)
	}
	t.Setenv(session.RunEnv, "")
	if _, err := execDelegate(t, "inbox"); err == nil || !strings.Contains(err.Error(), session.RunEnv) {
		t.Fatalf("inbox outside a worker: %v", err)
	}
	t.Setenv(session.RunEnv, run)
	out, err = execDelegate(t, "inbox")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("inbox %q err %v", out, err)
	}
	if _, err := execDelegate(t, "send", "w1", "do it"); err == nil || !strings.Contains(err.Error(), "herdr") {
		t.Fatalf("send outside herdr: %v", err)
	}
}

func TestDelegateAnswer_ClearsQuestionAndRefusesStale(t *testing.T) {
	dir, run := mailWorker(t)
	store, _ := runstore.New(dir)
	t.Setenv(session.RunEnv, run)
	if _, err := execDelegate(t, "reply", "--ask", "--option", "yes", "Proceed?"); err == nil {
		// no task yet: reply needs one
		t.Fatal("reply --ask without a task should fail")
	}
	mb, _ := store.Mailbox("w1")
	task := runstore.NewMsgID()
	if _, err := mb.Post(runstore.Inbox, runstore.Message{ID: task, Run: run, From: "caller", To: "w1", Type: runstore.TypeTask, Turn: 1, Body: "go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := execDelegate(t, "reply", "--ask", "--option", "yes", "Proceed?"); err != nil {
		t.Fatal(err)
	}
	qid := task + "-q1"
	if out, _ := execDelegate(t, "questions", "w1"); !strings.Contains(out, qid) {
		t.Fatalf("questions %q", out)
	}
	out, err := execDelegate(t, "answer", "w1", qid, "yes")
	if err != nil || !strings.Contains(out, `"question": "`+qid+`"`) {
		t.Fatalf("answer %q err %v", out, err)
	}
	if out, _ := execDelegate(t, "questions", "w1"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("still pending %q", out)
	}
	if _, err := execDelegate(t, "answer", "w1", qid, "again"); err == nil {
		t.Fatal("a second answer should be refused")
	}
}

func TestDelegateWatch_NoNameWatchesAll(t *testing.T) {
	mailWorker(t)
	code := captureWaitExit(t)
	out, err := execDelegate(t, "watch", "--timeout", "50ms")
	if err != nil || *code != session.TimeoutExit || !strings.Contains(out, "name=w1 state=") {
		t.Fatalf("exit %d err %v out %s", *code, err, out)
	}
}
