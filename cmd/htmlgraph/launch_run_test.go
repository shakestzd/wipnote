package main

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestRunHarnessWithCleanup_NormalExit asserts that cleanup runs and the
// helper returns nil when the child exits successfully.
func TestRunHarnessWithCleanup_NormalExit(t *testing.T) {
	cleanupCalled := false
	cleanup := func() { cleanupCalled = true }

	c := exec.Command("/bin/sh", "-c", "exit 0")
	if err := runHarnessWithCleanup(c, cleanup); err != nil {
		t.Errorf("expected nil error on success, got: %v", err)
	}
	if !cleanupCalled {
		t.Error("cleanup was not invoked on normal exit")
	}
}

// TestRunHarnessWithCleanup_NilCleanup asserts that a nil cleanup is
// tolerated — the helper should still run the child and report success.
func TestRunHarnessWithCleanup_NilCleanup(t *testing.T) {
	c := exec.Command("/bin/sh", "-c", "exit 0")
	if err := runHarnessWithCleanup(c, nil); err != nil {
		t.Errorf("expected nil error on success with nil cleanup, got: %v", err)
	}
}

// TestRunHarnessWithCleanup_StartFailure asserts that cleanup runs and an
// error is returned when the child fails to start (binary not found).
func TestRunHarnessWithCleanup_StartFailure(t *testing.T) {
	cleanupCalled := false
	cleanup := func() { cleanupCalled = true }

	c := exec.Command("/this/binary/does/not/exist/htmlgraph-test")
	err := runHarnessWithCleanup(c, cleanup)
	if err == nil {
		t.Error("expected error on start failure, got nil")
	}
	if !cleanupCalled {
		t.Error("cleanup was not invoked when c.Start failed")
	}
}

// TestRunHarnessWithCleanupCore_NormalExit verifies the testable core
// returns a zero-result for a clean child exit.
func TestRunHarnessWithCleanupCore_NormalExit(t *testing.T) {
	c := exec.Command("/bin/sh", "-c", "exit 0")
	res := runHarnessWithCleanupCore(c, nil)
	if res.Err != nil {
		t.Errorf("Err = %v, want nil", res.Err)
	}
	if res.ReraiseSig != 0 {
		t.Errorf("ReraiseSig = %v, want 0", res.ReraiseSig)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
}

// TestRunHarnessWithCleanupCore_NonZeroExit verifies the core returns the
// child's non-zero exit code for ordinary error returns.
func TestRunHarnessWithCleanupCore_NonZeroExit(t *testing.T) {
	c := exec.Command("/bin/sh", "-c", "exit 7")
	res := runHarnessWithCleanupCore(c, nil)
	if res.Err != nil {
		t.Errorf("Err = %v, want nil", res.Err)
	}
	if res.ReraiseSig != 0 {
		t.Errorf("ReraiseSig = %v, want 0", res.ReraiseSig)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
}

// TestRunHarnessWithCleanupCore_ChildSignaled is the regression test
// requested in roborev job 102. When the child is killed by a signal
// (SIGTERM here, simulating "terminal Ctrl-C reached the child first"),
// the core must surface the killing signal via ReraiseSig instead of
// reporting ExitCode=-1, so the launcher can re-raise for 128+signum
// POSIX semantics rather than exiting 255.
func TestRunHarnessWithCleanupCore_ChildSignaled(t *testing.T) {
	cleanupCalled := false
	cleanup := func() { cleanupCalled = true }

	// Self-terminate the shell with SIGTERM. The child reaps with
	// WaitStatus.Signaled()=true, Signal()=SIGTERM.
	c := exec.Command("/bin/sh", "-c", "kill -TERM $$")
	res := runHarnessWithCleanupCore(c, cleanup)

	if res.Err != nil {
		t.Errorf("Err = %v, want nil", res.Err)
	}
	if res.ReraiseSig != syscall.SIGTERM {
		t.Errorf("ReraiseSig = %v, want SIGTERM (the killing signal)", res.ReraiseSig)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (signal path takes precedence)", res.ExitCode)
	}
	if !cleanupCalled {
		t.Error("cleanup was not invoked when child was signal-killed")
	}
}
