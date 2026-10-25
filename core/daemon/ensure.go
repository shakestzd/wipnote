package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// Launcher-side daemon guarantee (feat-f6759e37).
//
// OWNERSHIP. The launcher owns availability. It starts a daemon, or attaches to
// one already running, and then writes the socket path into the session
// environment so hooks never have to negotiate. It NEVER stops the daemon on
// session exit: a shared daemon outlives any one session, and stopping it would
// break every other session attached to it. What bounds the daemon's life
// instead is the attachment — see Listener.isIdle: once every attached launcher
// process has exited, the ordinary idle-exit resumes.

// ErrDaemonReadUnsupported is returned by EnsureDaemon when a daemon is already
// resident for this project but does not speak the read protocol — an older
// build left running from a previous install.
//
// This is deliberately NOT treated as "start another one": the lease permits a
// single writer per project, and killing another session's daemon to upgrade it
// is not the launcher's call. The launcher instead declines to promise
// availability, announces that, and the session runs under the unguaranteed
// contract — which works exactly as it always has.
var ErrDaemonReadUnsupported = errors.New("daemon: resident daemon does not speak the read protocol")

// ErrDaemonStarting is returned by EnsureDaemon when it started (or found
// another process starting) a daemon whose socket is not bound before ctx
// ends. It is not a failure: the daemon is on its way up, and hooks reach it
// once it binds. The launcher prints nothing for it (bug-06d2c91d).
var ErrDaemonStarting = errors.New("daemon: still starting")

// ErrDaemonLegacyOwner is returned by EnsureDaemon when no daemon answers but
// a live writer from before the heartbeat lease (bug-5ca53f70) still holds
// .wipnote/writer.pid. The launcher does not start a second writer beside it,
// and it cannot stop another session's process safely, so it leaves the old
// writer running and says so in one line.
var ErrDaemonLegacyOwner = errors.New("daemon: an older wipnote writer holds the legacy lease")

// ensureReadyBudget is the default bound on the wait for a freshly spawned
// daemon to bind and answer a ping. The launcher runs EnsureDaemon in the
// background and joins it with a much shorter budget, so this wait is never
// on the launch path.
//
// WIPNOTE_DAEMON_READY_TIMEOUT overrides it with a Go duration ("15s", "1m").
// Raise it where the socket appears slowly, such as a project on a bind mount
// from another kernel. An empty, unparsable or non-positive value keeps the
// default. See ReadyTimeout.
const ensureReadyBudget = 5 * time.Second

// readyTimeoutEnv names the override for ensureReadyBudget.
const readyTimeoutEnv = "WIPNOTE_DAEMON_READY_TIMEOUT"

// ReadyTimeout is the launcher's readiness bound: ensureReadyBudget, or the
// WIPNOTE_DAEMON_READY_TIMEOUT override.
func ReadyTimeout() time.Duration {
	if d, err := time.ParseDuration(os.Getenv(readyTimeoutEnv)); err == nil && d > 0 {
		return d
	}
	return ensureReadyBudget
}

// ensureDialInterval is the backoff between readiness dials.
const ensureDialInterval = 25 * time.Millisecond

// EnsureDaemon guarantees a read-capable daemon for projectRoot and returns the
// socket path to publish into the session environment.
//
// attachPID is the process whose lifetime should hold the daemon open —
// normally the launcher's own pid. While it lives the daemon will not
// idle-exit, so a hook firing after a long pause still finds the daemon the
// launcher promised.
//
// A non-nil error means NO guarantee could be made. The caller must then run
// the session under the unguaranteed contract and SAY SO, rather than
// publishing a socket path that hooks would trust.
func EnsureDaemon(ctx context.Context, projectRoot, selfExe string, attachPID int) (string, error) {
	client := NewReadClient(projectRoot)

	// Fast path: a daemon is already up. Attach and we are done — this is the
	// "attach if one is already running" half of the ownership rule.
	if _, err := client.Ping(ctx); err == nil {
		return attachAndReturn(ctx, client, attachPID)
	} else if errors.Is(err, ErrReadUnsupported) {
		return "", ErrDaemonReadUnsupported
	}

	if os.Getenv("WIPNOTE_NO_AUTO_WRITER") != "" {
		return "", fmt.Errorf("daemon: auto-start disabled by WIPNOTE_NO_AUTO_WRITER")
	}

	if err := startWriterUnlessHeld(projectRoot, selfExe); err != nil {
		return "", err
	}
	if !waitForSocketPath(ctx, client.SocketPath(), ReadyTimeout()) {
		return "", fmt.Errorf("%w: socket not bound yet", ErrDaemonStarting)
	}
	if _, err := client.Ping(ctx); err != nil {
		if errors.Is(err, ErrReadUnsupported) {
			return "", ErrDaemonReadUnsupported
		}
		return "", fmt.Errorf("daemon: ping after spawn: %w", err)
	}
	return attachAndReturn(ctx, client, attachPID)
}

// startWriterUnlessHeld spawns a writer unless somebody already holds the
// lease. A current-format owner is starting or serving, so the caller waits
// for its socket. A live legacy owner is left alone (ErrDaemonLegacyOwner).
// A spawned writer that loses a race with another launcher's simply exits:
// the lease admits one owner, so spawning on a stale reading is safe.
func startWriterUnlessHeld(projectRoot, selfExe string) error {
	state := InspectLease(projectRoot)
	switch state.Status {
	case LeaseLegacyHeld:
		return fmt.Errorf("%w (pid %d)", ErrDaemonLegacyOwner, state.LegacyPID)
	case LeaseHeld:
		return nil
	}
	if err := spawnHeadlessWriter(selfExe, projectRoot); err != nil {
		return fmt.Errorf("daemon: spawn: %w", err)
	}
	return nil
}

// attachAndReturn registers attachPID with the daemon and returns its socket.
// A failed attach is fatal to the guarantee: without it the daemon may
// idle-exit mid-session, and the availability policy gives hooks no way to
// recover from that except by pausing the agent.
func attachAndReturn(ctx context.Context, client *ReadClient, attachPID int) (string, error) {
	if attachPID <= 0 {
		attachPID = os.Getpid()
	}
	if _, err := client.Attach(ctx, attachPID); err != nil {
		return "", fmt.Errorf("daemon: attach: %w", err)
	}
	return client.SocketPath(), nil
}

// waitForSocketPath polls a Unix socket by DIALING until a connection succeeds,
// budget elapses, or ctx fires. A successful dial is the only proof the daemon
// is accepting frames; the probe connection closes immediately.
func waitForSocketPath(ctx context.Context, socketPath string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	for {
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		conn, err := net.DialTimeout("unix", socketPath, ensureDialInterval)
		if err == nil {
			_ = conn.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(ensureDialInterval):
		}
	}
}
