package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/core/hooks"
)

// Launcher-side daemon guarantee (feat-f6759e37).
//
// A launcher-started session GUARANTEES the work-item daemon: this is where
// that promise is made. The launcher starts a daemon or attaches to one already
// running, proves it speaks the read protocol, and publishes its socket path
// into the child environment. Hooks then read work-item state from canonical
// state via that socket instead of from the derived index — which is the whole
// point, since a hook is a fresh process that cannot afford the canonical parse
// itself.
//
// The promise is made ONLY when it can be kept. If anything prevents it the
// launcher publishes nothing, and the session runs under the unguaranteed
// contract — identical to how every session behaved before this feature. A
// real failure is announced in one line. A daemon that is merely still
// starting is not announced and not waited for (bug-06d2c91d).

// ensureDaemonSlack is what the background ensure gets beyond the daemon's
// readiness bound (daemon.ReadyTimeout, which WIPNOTE_DAEMON_READY_TIMEOUT
// overrides) for the ping and attach after the socket is up. The ensure is
// NOT on the launch path.
const ensureDaemonSlack = 3 * time.Second

// daemonJoinBudget is the most the launcher waits, just before it starts the
// harness, for the ensure it began at the top of launchClaude (bug-06d2c91d).
// The daemon binds its socket before it builds its projection, so it is
// normally up long before this point, and the wait is zero.
const daemonJoinBudget = 150 * time.Millisecond

// ensureDaemonFn is daemon.EnsureDaemon; a test can stub it.
var ensureDaemonFn = daemon.EnsureDaemon

// errNotAProject marks a launch outside a wipnote project: no daemon, no output.
var errNotAProject = errors.New("not a wipnote project")

// pendingDaemon is a daemon ensure running in the background. socket and err
// are written before done closes and read only after it has.
type pendingDaemon struct {
	done   chan struct{}
	socket string
	err    error
	// notice is one line about an older wipnote owning the project's writer
	// lease, or "" (bug-5ca53f70).
	notice string
}

// startDaemonEnsure starts guaranteeing the daemon for wipnoteRoot without
// waiting for it. The launcher calls it first and collects the result with
// sessionEnv just before it starts the harness, so the daemon's startup runs
// alongside the rest of the launch instead of in front of it.
func startDaemonEnsure(wipnoteRoot string) *pendingDaemon {
	p := &pendingDaemon{done: make(chan struct{})}
	if wipnoteRoot == "" || !hooks.IswipnoteProject(wipnoteRoot) {
		p.err = errNotAProject
		close(p.done)
		return p
	}
	selfExe, err := os.Executable()
	if err != nil {
		p.err = fmt.Errorf("cannot resolve the wipnote binary to start it (%v)", err)
		close(p.done)
		return p
	}
	go func() {
		defer close(p.done)
		ctx, cancel := context.WithTimeout(context.Background(), daemon.ReadyTimeout()+ensureDaemonSlack)
		defer cancel()
		p.socket, p.err = ensureDaemonFn(ctx, wipnoteRoot, selfExe, os.Getpid())
		p.notice = olderOwnerNotice(wipnoteRoot, p.err)
	}()
	return p
}

// sessionEnv returns the environment assignments to layer into the harness,
// waiting at most budget for the ensure.
//
// It publishes the socket only for a daemon that is up and attached. A daemon
// that is still starting gets no guarantee and NO output: the session runs
// under the unguaranteed contract, its hooks fall back at once (see
// they never start one of their own), and the daemon still attaches to this
// launcher when it binds. A real failure gets one quiet line on w, and so
// does an older wipnote holding the project's writer lease.
func (p *pendingDaemon) sessionEnv(w io.Writer, budget time.Duration) []string {
	select {
	case <-p.done:
	case <-time.After(budget):
		return nil
	}
	if p.notice != "" {
		fmt.Fprintln(w, p.notice)
	}
	switch {
	case p.err == nil:
		fmt.Fprintf(w, "wipnote: work-item daemon guaranteed for this session (socket %s)\n", p.socket)
		return []string{hooks.DaemonSocketEnv + "=" + p.socket}
	case errors.Is(p.err, errNotAProject), errors.Is(p.err, daemon.ErrDaemonStarting),
		errors.Is(p.err, daemon.ErrDaemonLegacyOwner): // the notice said it
		return nil
	case errors.Is(p.err, daemon.ErrDaemonReadUnsupported):
		announceUnguaranteed(w, "an older wipnote daemon serves this project without reads")
	default:
		announceUnguaranteed(w, p.err.Error())
	}
	return nil
}

// announceUnguaranteed reports, in one line, why this session runs without
// the daemon guarantee. Hooks then read the derived index, as they do in any
// session no launcher started.
func announceUnguaranteed(w io.Writer, reason string) {
	fmt.Fprintf(w, "wipnote: work-item daemon unavailable (%s); hooks use the derived index\n", reason)
}

// olderOwnerNotice is the one line for an older wipnote that owns the
// project's writer lease, or "". The ensure's own legacy-owner error is the
// fallback, so a launcher that declined to start a writer always says why.
func olderOwnerNotice(wipnoteRoot string, ensureErr error) string {
	if n := daemon.OlderOwnerNotice(wipnoteRoot, version); n != "" {
		return n
	}
	if errors.Is(ensureErr, daemon.ErrDaemonLegacyOwner) {
		return "wipnote: " + ensureErr.Error() + "; it is left running. Stop it to let this version start its own."
	}
	return ""
}
