package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// captureWaitExit replaces wait's process exit for one test and reports the
// status it was given (-1 when wait never called it).
func captureWaitExit(t *testing.T) *int {
	t.Helper()
	code := -1
	prev := delegateWaitExit
	delegateWaitExit = func(c int) { code = c }
	t.Cleanup(func() { delegateWaitExit = prev })
	return &code
}

// bug-da9bde6f: the cobra command exits with the outcome's status.
func TestDelegateWaitCmd_TimeoutExits124(t *testing.T) {
	mailWorker(t)
	code := captureWaitExit(t)
	if _, err := execDelegate(t, "wait", "w1", "--timeout", "50ms"); err != nil {
		t.Fatal(err)
	}
	if *code != session.TimeoutExit {
		t.Fatalf("exit %d, want %d", *code, session.TimeoutExit)
	}
}

func TestDelegateWaitCmd_GoneExitsGoneExit(t *testing.T) {
	dir, run := mailWorker(t)
	ledger := delegationledger.NewStore(filepath.Join(dir, ".wipnote"))
	if err := ledger.Close(run, delegationledger.CloseInfo{Ended: time.Now().UTC(), Reason: delegationledger.EndGone}); err != nil {
		t.Fatal(err)
	}
	code := captureWaitExit(t)
	if _, err := execDelegate(t, "wait", "w1", "--timeout", "2s"); err != nil {
		t.Fatal(err)
	}
	if *code != session.GoneExit {
		t.Fatalf("exit %d, want %d", *code, session.GoneExit)
	}
}

func TestDelegateWaitCmd_DoneExitsZero(t *testing.T) {
	dir, run := mailWorker(t)
	store, _ := runstore.New(dir)
	mb, err := store.Mailbox("w1")
	if err != nil {
		t.Fatal(err)
	}
	task, err := mb.Post(runstore.Inbox, runstore.Message{Run: run, From: "caller", To: "w1", Body: "do it", Turn: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := mb.WriteResult(task.ID, runstore.Message{Run: run, From: "w1", To: "caller", Body: "ok"}); err != nil {
		t.Fatal(err)
	}
	code := captureWaitExit(t)
	out, err := execDelegate(t, "wait", "w1", "--timeout", "2s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"outcome": "done"`) || *code != -1 {
		t.Fatalf("done exited %d, want 0 (no exit call); out %s", *code, out)
	}
}
