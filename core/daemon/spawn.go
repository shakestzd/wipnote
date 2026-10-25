package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// AutoSpawn controls whether SubmitOrSpawn is allowed to fork a headless
// writer daemon when none is reachable. Spawn is a separate, opt-in step
// from a plain dial so callers that only want to USE an existing writer
// (and never start one) keep the dumb dial-or-fail client.
//
// LIVE-SESSION SAFETY (feat-075c110d, bug-06d2c91d): the dial+submit is
// bounded by the caller's context, and a spawn is never waited for. On ANY
// failure, timeout, spawn, or sandbox/forbidden-spawn the caller receives
// ErrWriterUnavailable and falls back to direct-open. The
// spawn is guarded by the O_EXCL writer lease so two racing hooks never
// fork-bomb a pair of daemons.

// SubmitOrSpawn submits env to the per-project writer, auto-spawning a
// headless writer daemon when none is reachable.
//
// Flow:
//  1. Try a plain Submit. If it succeeds (writer already up) → done.
//  2. On ErrWriterUnavailable, try to acquire the O_EXCL writer lease.
//     - Lease acquired → we are the spawner: fork the headless writer and
//     release our lease so the child can claim it.
//     - ErrLeaseHeld → another process owns or is starting the writer.
//  3. Either way, return ErrWriterUnavailable AT ONCE so the caller takes
//     its direct-write fallback. The next caller finds the writer up.
//
// SubmitOrSpawn never waits for a writer to bind (bug-06d2c91d). It used to
// dial-wait up to 1.5s for the socket, and a hook makes several daemon writes,
// so every hook paid that wait several times over while a writer was starting:
// session-start took 6-12s and user-prompt 4.7s. A hook cannot use a writer
// that is not up yet, so waiting for one bought nothing the fallback does not
// already give. selfExe is the wipnote binary to fork (resolved by the caller
// via os.Executable); projectRoot is the project whose writer to target.
func (c *WriterClient) SubmitOrSpawn(ctx context.Context, projectRoot, selfExe string, env Envelope) (Ack, error) {
	// Fast path: an existing writer answers immediately.
	ack, err := c.Submit(ctx, env)
	if err == nil {
		return ack, nil
	}
	if err != ErrWriterUnavailable {
		// A real transport fault (not "no daemon") — surface as unavailable
		// so the caller falls back rather than interpreting a half-dead
		// owner as a hard failure.
		return Ack{}, ErrWriterUnavailable
	}
	if ctx.Err() != nil {
		return Ack{}, ErrWriterUnavailable
	}

	// Opt-out: operators (and tests) can disable auto-spawn entirely. When
	// set, an unreachable writer immediately yields ErrWriterUnavailable so
	// the caller falls back to direct-open without ever forking.
	if os.Getenv("WIPNOTE_NO_AUTO_WRITER") != "" {
		return Ack{}, ErrWriterUnavailable
	}

	// No writer reachable. If nobody is starting one, start it, then fall
	// back without waiting for it either way.
	lease, lerr := AcquireLease(projectRoot)
	if lerr == nil {
		// The child must own the lease, so release ours before the spawn. If
		// the child fails to start, the lease is free for the next attempt.
		_ = lease.Release()
		_ = spawnHeadlessWriter(selfExe, projectRoot)
	}
	return Ack{}, ErrWriterUnavailable
}

// spawnHeadlessWriter forks `<selfExe> --project-dir <projectRoot>
// _serve-child --headless` as a DETACHED process in its own process group so
// it outlives the spawning hook subprocess. Unlike the supervised
// _serve-child children, the writer must SURVIVE its spawner's exit (the
// hook exits in milliseconds), so it deliberately does NOT set Pdeathsig.
//
// stdout/stderr are routed to .wipnote/logs/writer.log; stdin is closed. The
// returned error is non-nil only when the fork itself fails (e.g. sandboxed
// exec). Readiness is not checked here: SubmitOrSpawn never waits for it,
// and EnsureDaemon dial-waits for the socket itself.
func spawnHeadlessWriter(selfExe, projectRoot string) error {
	if selfExe == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve self exe: %w", err)
		}
		selfExe = exe
	}
	logDir := filepath.Join(projectRoot, ".wipnote", "logs")
	_ = os.MkdirAll(logDir, 0o755)
	var out *os.File
	if f, err := os.OpenFile(filepath.Join(logDir, "writer.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		out = f
	}

	cmd := exec.Command(selfExe, "--project-dir", projectRoot, "_serve-child", "--headless")
	cmd.Dir = projectRoot
	cmd.Stdin = nil
	if out != nil {
		cmd.Stdout = out
		cmd.Stderr = out
	}
	cmd.Env = os.Environ()
	// Detach into a new process group so a SIGKILL to the hook's group does
	// not propagate. Portable across Linux + darwin (no Pdeathsig: the
	// writer must outlive the short-lived spawning hook).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		if out != nil {
			_ = out.Close()
		}
		return fmt.Errorf("spawn headless writer: %w", err)
	}
	// Reap the immediate child handle in the background so we don't leave a
	// zombie if it exits early (e.g. lost the lease race and exited 0). The
	// long-lived writer is in its own process group; this Wait only reaps
	// our direct fork handle.
	go func() { _ = cmd.Wait() }()
	if out != nil {
		_ = out.Close() // child holds its own fd after Start
	}
	return nil
}
