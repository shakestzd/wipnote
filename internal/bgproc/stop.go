package bgproc

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/collector"
)

// StopResult is what Restart did to one process.
type StopResult struct {
	Proc   Proc
	Killed bool // needed SIGKILL after the grace period
	Err    error
}

// ErrNoLongerOurs means the process table changed between Inspect and the
// signal: the pid is gone or now runs something else. Nothing was sent.
var ErrNoLongerOurs = errors.New("process is no longer the verified wipnote process; not signalled")

// Restart stops every hung or orphaned process in procs — SIGTERM, then
// SIGKILL after opts.Grace — and starts a new writer when it stopped one.
// Collectors need no restart here: each live launcher's watchdog starts a
// replacement on the same port. Each pid is re-verified against the process
// table right before every signal.
func Restart(ctx context.Context, root string, procs []Proc, opts Options) []StopResult {
	opts = opts.withDefaults()
	var results []StopResult
	stoppedWriter := false
	for _, p := range procs {
		if !p.Stoppable() {
			continue
		}
		killed, err := stopVerified(ctx, root, p, opts)
		results = append(results, StopResult{Proc: p, Killed: killed, Err: err})
		if err == nil && p.Kind == KindWriter {
			stoppedWriter = true
		}
	}
	if stoppedWriter && opts.RestartWriter != nil {
		if err := opts.RestartWriter(ctx, root); err != nil {
			results = append(results, StopResult{Proc: Proc{Kind: KindWriter}, Err: fmt.Errorf("restart writer: %w", err)})
		}
	}
	return results
}

// stopVerified sends SIGTERM, waits up to opts.Grace, then SIGKILL.
func stopVerified(ctx context.Context, root string, p Proc, opts Options) (killed bool, err error) {
	if err := signalVerified(ctx, root, p, opts, syscall.SIGTERM); err != nil {
		return false, err
	}
	if waitExit(p.PID, opts.Grace) {
		return false, nil
	}
	if err := signalVerified(ctx, root, p, opts, syscall.SIGKILL); err != nil {
		if errors.Is(err, ErrNoLongerOurs) {
			return false, nil // it exited between the wait and the check
		}
		return false, err
	}
	if !waitExit(p.PID, 2*time.Second) {
		return true, fmt.Errorf("pid %d survived SIGKILL", p.PID)
	}
	return true, nil
}

// signalVerified re-reads the process table and sends sig only when p.PID is
// still this user's process with the same verified argv.
func signalVerified(ctx context.Context, root string, p Proc, opts Options, sig syscall.Signal) error {
	table, err := userTable(ctx, opts.Lister)
	if err != nil {
		return fmt.Errorf("re-verify pid %d: %w", p.PID, err)
	}
	raw, ok := table[p.PID]
	check := p
	check.Command = raw.Command
	if !ok || raw.Command != p.Command || !argvMatches(&check, root, opts.ExeNames) {
		return ErrNoLongerOurs
	}
	if err := syscall.Kill(p.PID, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal pid %d: %w", p.PID, err)
	}
	return nil
}

// waitExit polls until pid is gone or d elapses.
func waitExit(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for collector.PIDAlive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
	return true
}
