package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

// harnessResult describes how the parent launcher should terminate after
// the harness child has reaped. Exactly one of the three fields is
// meaningful per call:
//
//   - Err non-nil       → return this error to the caller (start failure or
//     other unexpected error before/around the child run).
//   - ReraiseSig non-0  → signal.Reset and re-raise this signal so the
//     parent exits with 128+signum POSIX semantics. Used for both
//     parent-received signals and child-was-signal-terminated cases.
//   - ExitCode non-0    → os.Exit(ExitCode) to propagate the child's
//     ordinary non-zero return code.
//   - All zero          → child exited 0; return nil.
type harnessResult struct {
	Err         error
	ReraiseSig  syscall.Signal
	ExitCode    int
}

// runHarnessWithCleanupCore is the testable core of runHarnessWithCleanup.
// It runs the child under SIGINT/SIGTERM signal handling, runs cleanup
// once the child reaps, and returns a harnessResult describing how the
// caller should terminate. Pure — no os.Exit, no syscall.Kill — so tests
// can assert on the result without crashing the test binary.
func runHarnessWithCleanupCore(c *exec.Cmd, cleanup func()) harnessResult {
	var once sync.Once
	callCleanup := func() {
		once.Do(func() {
			if cleanup != nil {
				cleanup()
			}
		})
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if err := c.Start(); err != nil {
		callCleanup()
		return harnessResult{Err: fmt.Errorf("start harness: %w", err)}
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- c.Wait() }()

	var sigReceived os.Signal
	select {
	case sigReceived = <-sigCh:
		// Forward the signal to the child so it exits gracefully.
		if c.Process != nil {
			_ = c.Process.Signal(sigReceived)
		}
		<-waitCh
	case <-waitCh:
		// Child exited on its own.
	}

	callCleanup()

	// Parent-received signal takes precedence: re-raise the same signal so
	// the launcher's exit reflects the user's interrupt intent.
	if sigReceived != nil {
		if sysSig, ok := sigReceived.(syscall.Signal); ok {
			return harnessResult{ReraiseSig: sysSig}
		}
	}

	// No parent signal — inspect the child's wait status. If the child was
	// killed by a signal (e.g. terminal SIGINT reached the child directly
	// because we share the foreground process group), preserve POSIX
	// signal-exit semantics by re-raising the same signal in the parent.
	if c.ProcessState != nil {
		if ws, ok := c.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return harnessResult{ReraiseSig: ws.Signal()}
		}
		if !c.ProcessState.Success() {
			return harnessResult{ExitCode: c.ProcessState.ExitCode()}
		}
	}
	return harnessResult{}
}

// runHarnessWithCleanup runs the harness child process under a signal
// handler that intercepts SIGINT and SIGTERM, ensuring cleanup runs
// before the launcher exits. It is the production entry point — for the
// testable core that returns a result struct without calling os.Exit /
// syscall.Kill, see runHarnessWithCleanupCore.
//
// Behavior:
//   - On parent-received SIGINT/SIGTERM: forward to the child, run
//     cleanup, then re-raise the signal in the parent for 128+signum
//     POSIX exit semantics.
//   - On child signal-termination (e.g. Ctrl-C reached the child via
//     the terminal foreground group): re-raise the same signal in the
//     parent so the launcher exits with the conventional 128+signum
//     code instead of -1 / 255.
//   - On child non-zero exit: os.Exit with the child's exit code.
//   - On child exit 0: return nil.
//
// cleanup may be nil — if so, no cleanup is invoked but signal handling
// still runs.
func runHarnessWithCleanup(c *exec.Cmd, cleanup func()) error {
	res := runHarnessWithCleanupCore(c, cleanup)
	if res.Err != nil {
		return res.Err
	}
	if res.ReraiseSig != 0 {
		signal.Reset(syscall.SIGINT, syscall.SIGTERM)
		_ = syscall.Kill(os.Getpid(), res.ReraiseSig)
		// If the re-raise didn't terminate (rare; some signal masks),
		// fall through with nil so the launcher returns cleanly.
		return nil
	}
	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
	return nil
}
