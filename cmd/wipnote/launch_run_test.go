package main

import (
	"os"
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
// tolerated -- the helper should still run the child and report success.
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

	c := exec.Command("/this/binary/does/not/exist/wipnote-test")
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

// TestRunHarnessWithCleanupCore_ChildSignaled is the regression test for
// signal re-raise semantics. When the child is killed by a signal (SIGTERM),
// the core must surface the killing signal via ReraiseSig.
func TestRunHarnessWithCleanupCore_ChildSignaled(t *testing.T) {
	cleanupCalled := false
	cleanup := func() { cleanupCalled = true }

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

// TestNoexecTempRoot_ProfileCheck validates the slice-8 profile constraint:
// exec-capable temp roots are REQUIRED for any launcher path that spawns
// child binaries. This test documents the TMPDIR constraint and verifies
// the environment by attempting to exec a trivial script from t.TempDir().
//
// PROFILE REQUIREMENT: exec-capable TMPDIR (e.g. TMPDIR=/home/vscode/.gotest-tmp).
// The test skips cleanly when running under a noexec TMPDIR so CI without
// a privileged exec root still passes -- never fails, always skips.
//
// The practical constraint: /tmp is mounted noexec in this devcontainer.
// Any test that builds + execs a child binary MUST use an exec-capable root.
// Set TMPDIR=/home/vscode/.gotest-tmp before running such tests.
func TestNoexecTempRoot_ProfileCheck(t *testing.T) {
	dir := t.TempDir()
	scriptPath := dir + "/probe.sh"
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Skipf("noexec profile check: cannot write probe script: %v", err)
	}
	cmd := exec.Command(scriptPath)
	if err := cmd.Run(); err != nil {
		t.Skipf("noexec profile check: TMPDIR=%q is not exec-capable -- "+
			"set TMPDIR=/home/vscode/.gotest-tmp for tests that spawn child binaries "+
			"(slice-8 profile requirement, plan-1670cacd slice-10). Error: %v", dir, err)
	}
	// Exec-capable TMPDIR confirmed. No assertion needed beyond the skip gate.
	t.Logf("exec-capable TMPDIR confirmed: %s", dir)
}
