package session

import (
	"testing"
	"time"
)

// bug-da9bde6f: wait's exit status tells the caller's shell what happened.
// done (and every other settled outcome) is 0, a timeout 124, a lost worker
// GoneExit and a worker held at a dialog BlockedExit.
func TestWaitResult_ExitCodePerOutcome(t *testing.T) {
	want := map[string]int{
		OutcomeDone:         0,
		OutcomeReady:        0,
		OutcomeIdleNoResult: 0,
		OutcomeIdleShell:    0,
		OutcomeAsks:         0,
		OutcomeStopped:      0,
		OutcomeTimeout:      124,
		OutcomeGone:         GoneExit,
		OutcomeBlocked:      BlockedExit,
	}
	for outcome, code := range want {
		if got := (WaitResult{Outcome: outcome}).ExitCode(); got != code {
			t.Errorf("%s exits %d, want %d", outcome, got, code)
		}
	}
	codes := map[int]bool{0: true, 1: true, TimeoutExit: true}
	for _, c := range []int{GoneExit, BlockedExit} {
		if codes[c] {
			t.Errorf("exit %d clashes with 0, 1 or 124", c)
		}
		codes[c] = true
	}
}

func TestWait_ExitCodesFromRealWaits(t *testing.T) {
	e := newEnv(t)
	s := e.unbound("w1", "brief")
	task := e.task("w1")
	if res := e.wait("w1", 60*time.Millisecond); res.Outcome != OutcomeTimeout || res.ExitCode() != TimeoutExit {
		t.Fatalf("timeout: %+v exit %d", res, res.ExitCode())
	}
	e.herdr.pane(PaneOf(s)).screen = "❯ No, exit\n  Yes, I trust this folder\n"
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeBlocked || res.ExitCode() != BlockedExit {
		t.Fatalf("blocked: %+v exit %d", res, res.ExitCode())
	}
	e.herdr.pane(PaneOf(s)).screen = "plain output\n"
	e.writeResult("w1", task, "answer")
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeDone || res.ExitCode() != 0 {
		t.Fatalf("done: %+v exit %d", res, res.ExitCode())
	}
	e.herdr.pane(PaneOf(s)).gone = true
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeGone || res.ExitCode() != GoneExit {
		t.Fatalf("gone: %+v exit %d", res, res.ExitCode())
	}
}
