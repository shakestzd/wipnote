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
