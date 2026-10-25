package main

import (
	"os/exec"

	"github.com/shakestzd/wipnote/internal/launcher"
)

// runHarnessWithCleanup runs the harness child process under a signal
// handler that intercepts SIGINT and SIGTERM, ensuring cleanup runs
// before the launcher exits. It is the production entry point — for the
// testable core that returns a result struct without calling os.Exit /
// syscall.Kill, see launcher.RunHarnessWithCleanupCore.
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
	return launcher.RunHarnessWithCleanup(c, cleanup)
}
