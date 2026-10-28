package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/sessionledger"
	"github.com/shakestzd/wipnote/internal/procsweep"
)

const (
	procLiveSession  = "55555555-5555-4555-8555-555555555555"
	procEndedSession = "66666666-6666-4666-8666-666666666666"
)

// reconcileProcsFixture builds a temp project whose ledger has one live and
// one ended session, and swaps in a fake process table: pid 4242 belongs to
// the ended session, pid 4343 to the live one, pid 4444 to no session.
func reconcileProcsFixture(t *testing.T) {
	t.Helper()
	root := reconcileTestRepo(t)
	store := sessionledger.NewStore(filepath.Join(root, ".wipnote"))
	start := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	for _, id := range []string{procLiveSession, procEndedSession} {
		if _, err := store.Open(sessionledger.Record{SessionID: id, Harness: "claude", StartedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(procEndedSession, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	at := start.Add(5 * time.Minute)
	table := []procsweep.RawProc{
		{PID: 4242, PPID: 1, Command: "python -m http.server 8000", StartedAt: at, Env: map[string]string{"CLAUDE_CODE_SESSION_ID": procEndedSession}},
		{PID: 4343, PPID: 1, Command: "npm run dev", StartedAt: at, Env: map[string]string{"CLAUDE_CODE_SESSION_ID": procLiveSession}},
		{PID: 4444, PPID: 1, Command: "vim", StartedAt: at, Env: map[string]string{}},
	}
	setReconcileLister(t, procsweep.ListerFunc(func(context.Context) ([]procsweep.RawProc, error) { return table, nil }))
}

func setReconcileLister(t *testing.T, l procsweep.Lister) {
	t.Helper()
	prev := reconcileProcLister
	reconcileProcLister = l
	t.Cleanup(func() { reconcileProcLister = prev })
}

func runReconcile(t *testing.T, args ...string) string {
	t.Helper()
	cmd := reconcileCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("reconcile %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestReconcileCmd_ReportsOrphanProcessWithStopHint(t *testing.T) {
	reconcileProcsFixture(t)
	out := runReconcile(t)
	if !strings.Contains(out, "orphaned process 4242") || !strings.Contains(out, "kill 4242") {
		t.Fatalf("orphan with stop hint missing:\n%s", out)
	}
	for _, want := range []string{"python -m http.server", procEndedSession, "started "} {
		if !strings.Contains(out, want) {
			t.Fatalf("orphan line missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "4343") || strings.Contains(out, "4444") {
		t.Fatalf("default reconcile reports only orphans:\n%s", out)
	}
	if strings.Contains(out, "nothing to reconcile") {
		t.Fatalf("an orphan process is something to reconcile:\n%s", out)
	}
}

func TestReconcileCmd_ProcessesListsLiveAndOrphan(t *testing.T) {
	reconcileProcsFixture(t)
	out := runReconcile(t, "--processes")
	for _, want := range []string{"4242", "4343", "orphan", "live", "npm run dev", "kill 4343", procLiveSession} {
		if !strings.Contains(out, want) {
			t.Fatalf("--processes output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "4444") {
		t.Fatalf("a process without a session id must not be listed:\n%s", out)
	}
}

func TestReconcileCmd_ProcessesSaysWhenEnvUnreadable(t *testing.T) {
	reconcileTestRepo(t)
	setReconcileLister(t, procsweep.ListerFunc(func(context.Context) ([]procsweep.RawProc, error) {
		return nil, procsweep.ErrEnvUnreadable
	}))
	for _, args := range [][]string{nil, {"--processes"}} {
		out := runReconcile(t, args...)
		if !strings.Contains(out, "process environment not readable on this platform") {
			t.Fatalf("reconcile %v must say the platform cannot read environments:\n%s", args, out)
		}
	}
}
