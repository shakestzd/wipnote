//go:build linux

package procsweep

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestRealSleepChild_FoundBySessionID starts one real `sleep` with a fake
// session id in its environment and checks the default lister finds it. The
// test owns that child and stops it itself; the package never signals.
func TestRealSleepChild_FoundBySessionID(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}
	const fake = "44444444-4444-4444-8444-444444444444"
	cmd := exec.Command("sleep", "30")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "CLAUDE_CODE_SESSION_ID=" + fake}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	procs, err := DefaultLister().List(context.Background())
	if err != nil {
		t.Skipf("process environment not readable here: %v", err)
	}
	for _, p := range procs {
		if p.PID == cmd.Process.Pid {
			if got := SessionIDFromEnv(p.Env); got != fake {
				t.Fatalf("session id = %q, want %q", got, fake)
			}
			if p.Command != "sleep 30" || p.StartedAt.IsZero() {
				t.Fatalf("bad proc: %+v", p)
			}
			return
		}
	}
	t.Fatalf("real sleep child pid %d not found among %d procs", cmd.Process.Pid, len(procs))
}
