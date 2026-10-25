package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
)

// Claude Code gives a plugin SessionEnd hook 1.5s and cancels it after that.
// These tests pin what must be done INSIDE that window (bug-1daed6d6).

// TestSessionEnd_EssentialWritesFitTheWindow runs SessionEnd against a slow
// filesystem (25ms per probed stat/open/rewrite), 14 claim shards and a git
// that takes 500ms per call. The end note and the essential writes must finish
// in under 1.0s, and nothing may spawn git or scan the claim directory.
func TestSessionEnd_EssentialWritesFitTheWindow(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 13)
	gitLog := stubGit(t, f, 500*time.Millisecond)
	probe := installSlowFS(t, f, 25*time.Millisecond)

	start := time.Now()
	if _, err := SessionEnd(&CloudEvent{SessionID: f.sessionID, CWD: f.projectDir, Reason: "prompt_input_exit"}, td.DB, f.projectDir); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("SessionEnd took %s over %d probed filesystem operations", elapsed, probe.ops)

	if elapsed >= time.Second {
		t.Errorf("SessionEnd took %s, want < 1s (the harness grants 1.5s)", elapsed)
	}
	if calls := gitCalls(t, gitLog); len(calls) != 0 {
		t.Errorf("SessionEnd spawned git %d time(s): %v; the window allows none", len(calls), calls)
	}
	if len(probe.scanBeforeNote) != 0 || probe.claimScans != 0 {
		t.Errorf("SessionEnd scanned the claims directory %d time(s), %d before the note; it must not ReadAll",
			probe.claimScans, len(probe.scanBeforeNote))
	}
	note, ok := ReadSessionEndNote(f.projectDir, f.sessionID)
	if !ok || note.Reason != "prompt_input_exit" || note.ClaimOutcome() != claimledger.OutcomeAbandoned {
		t.Fatalf("end note = %+v ok=%v", note, ok)
	}
	if row := f.ledgerRow(t); row.IsOpen() {
		t.Error("ledger row still open after SessionEnd")
	}
	for _, e := range f.episodes(t, f.sessionID) {
		if e.IsOpen() || e.Outcome != claimledger.OutcomeAbandoned {
			t.Errorf("episode %s open=%v outcome=%q, want closed as abandoned", e.WorkItemID, e.IsOpen(), e.Outcome)
		}
	}
}

// violations lists what a SessionEnd step did before the end note existed.
func violations(f *endFixture, gitLog string, probe *probeLog, t *testing.T) []string {
	var out []string
	for _, c := range gitCalls(t, gitLog) {
		if len(c) >= 6 && c[:6] == "absent" {
			out = append(out, "git before note: "+c)
		}
	}
	for _, p := range probe.scanBeforeNote {
		out = append(out, "claims scan before note: "+p)
	}
	return out
}

// TestSessionEnd_NoGitOrScanBeforeNote is the guard: it must pass for the real
// handler AND must fail for the old ordering, so a regression cannot slip by.
func TestSessionEnd_NoGitOrScanBeforeNote(t *testing.T) {
	t.Run("real handler is clean", func(t *testing.T) {
		td := setupTestDB(t)
		f := newEndFixture(t, 3)
		gitLog := stubGit(t, f, 10*time.Millisecond)
		probe := installSlowFS(t, f, time.Millisecond)
		if _, err := SessionEnd(&CloudEvent{SessionID: f.sessionID, CWD: f.projectDir}, td.DB, f.projectDir); err != nil {
			t.Fatal(err)
		}
		if v := violations(f, gitLog, probe, t); len(v) != 0 {
			t.Fatalf("violations: %v", v)
		}
	})
	t.Run("old ordering is caught", func(t *testing.T) {
		f := newEndFixture(t, 3)
		gitLog := stubGit(t, f, 10*time.Millisecond)
		probe := installSlowFS(t, f, time.Millisecond)
		_ = headCommit(f.projectDir)                             // the old first line of SessionEnd
		_ = rootSessionForClaimLedger(f.wipnoteDir, f.sessionID) // the old ReadAll
		RecordSessionEndNote(&CloudEvent{SessionID: f.sessionID}, f.projectDir)
		if v := violations(f, gitLog, probe, t); len(v) != 2 {
			t.Fatalf("guard should flag the old git spawn and the old ReadAll, got %v", v)
		}
	})
}

// TestSessionEnd_CollectorIgnoringSIGTERMDoesNotHoldHook: the hook sends one
// SIGTERM and moves on. A collector that ignores it must neither delay the hook
// nor be SIGKILLed by it; catch-up reaps it later.
func TestSessionEnd_CollectorIgnoringSIGTERMDoesNotHoldHook(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 0)
	cmd := exec.Command("sh", "-c", `trap '' TERM; while :; do sleep 0.1; done`)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })
	time.Sleep(100 * time.Millisecond) // let sh install the trap

	pidPath := filepath.Join(f.wipnoteDir, "sessions", f.sessionID, ".collector-pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if _, err := SessionEnd(&CloudEvent{SessionID: f.sessionID, CWD: f.projectDir}, td.DB, f.projectDir); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("SessionEnd took %s with a SIGTERM-ignoring collector, want well under the 3s drain it used to wait", elapsed)
	}
	select {
	case <-exited:
		t.Fatal("the hook killed the collector; draining and SIGKILL belong to catch-up")
	default:
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Error("pid file removed although the collector is still alive")
	}

	CatchUpEndedSessions(td.DB, f.projectDir, "", true)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("catch-up did not kill the collector that ignores SIGTERM")
	}
}
