package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Writer lifetime (bug-5ca53f70).
//
// A writer exits in two cases, both checked by one watcher:
//
//  1. It no longer owns the project: the lease names another owner (or is
//     gone), or the socket path is no longer the inode it bound. Such a
//     server is unreachable or a duplicate, and it exits within one watch
//     interval. These were the orphans of #260: servers whose socket a later
//     server had unlinked, alive for hours and reachable by nobody.
//  2. It is idle: no client for the idle window. A client is an attached
//     launcher whose process lives, or an op (read or write) in the window.
//
// The startup hydrate is busy work, but it is busy only on behalf of a
// client. A server with no client for the whole window exits even while the
// hydrate runs, and its owner cancels the hydrate through ctx. A hydrate that
// keeps an unwanted server alive is the CPU cost #260 reported.

// DefaultIdleTimeout is how long a writer with no client waits before it
// exits (running the graceful-shutdown cleanup). Five minutes amortises the
// start cost across a session's bursts of hook writes and still frees an
// idle project promptly.
const DefaultIdleTimeout = 5 * time.Minute

// idleCheckInterval is how often the watcher checks ownership and idleness.
// It is a variable so tests can shorten it.
var idleCheckInterval = 15 * time.Second

// watchInterval is idleCheckInterval, or half the idle window when that is
// shorter, so an exit lands within one interval of its cause.
func watchInterval(idleTimeout time.Duration) time.Duration {
	interval := idleCheckInterval
	if idleTimeout > 0 && idleTimeout/2 < interval {
		interval = idleTimeout / 2
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	return interval
}

// ServeWithIdleTimeout runs Serve until ctx ends, the listener loses its
// ownership, or (when idleTimeout > 0) it has had no client for idleTimeout.
// The caller's cleanup then runs exactly as for a signal-driven shutdown.
func (l *Listener) ServeWithIdleTimeout(ctx context.Context, idleTimeout time.Duration) error {
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go l.watch(watchCtx, cancel, idleTimeout)
	return l.Serve(watchCtx)
}

func (l *Listener) watch(ctx context.Context, stop context.CancelFunc, idleTimeout time.Duration) {
	ticker := time.NewTicker(watchInterval(idleTimeout))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reason := l.exitReason(idleTimeout); reason != "" {
				log.Printf("writer: exiting: %s (pid %d)", reason, os.Getpid())
				stop()
				return
			}
		}
	}
}

// exitReason returns why the listener must stop now, or "".
func (l *Listener) exitReason(idleTimeout time.Duration) string {
	if l.lease != nil && !l.lease.Verify() {
		return "this process no longer holds the writer lease"
	}
	if !l.ownsSocket() {
		return "the socket path is no longer the socket this process bound"
	}
	if idleTimeout > 0 && l.isIdle(idleTimeout) {
		return "no client for " + idleTimeout.String()
	}
	return ""
}

// isIdle reports whether the writer has no client: no op in flight, no live
// attached launcher, and no op within idleTimeout. Readiness is deliberately
// not a condition (see the file comment).
func (l *Listener) isIdle(idleTimeout time.Duration) bool {
	if l.inFlight.Load() != 0 || l.liveAttachedCount() > 0 {
		return false
	}
	last := time.Unix(0, l.lastActivity.Load())
	return time.Since(last) >= idleTimeout
}

// ownsSocket reports whether the socket path is still the inode this
// listener bound. A stat error other than "missing" is not proof of loss.
func (l *Listener) ownsSocket() bool {
	cur, err := leaseFS.Stat(l.sockPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil || l.sockInfo == nil {
		return true
	}
	return os.SameFile(l.sockInfo, cur)
}

// unlinkStaleSocket checks that this process may bind sockPath, then
// removes a leftover socket file so the bind succeeds after an unclean exit.
//
// Only the lease owner may bind. With a lease, the lease must still name
// this process; any socket file at the path then belongs to a dead or
// displaced server and is removed. Without a lease (in-process fixtures),
// the bind is refused while anybody holds the lease, by heartbeat or as a
// live legacy owner, so a live writer's socket is never unlinked.
func unlinkStaleSocket(sockPath string, lease *Lease) error {
	if lease != nil && !lease.Verify() {
		return fmt.Errorf("daemon: refusing to bind %s: this process does not hold the writer lease", sockPath)
	}
	if lease == nil && inspectLeaseDir(filepath.Dir(sockPath), time.Now()).Held() {
		return fmt.Errorf("daemon: refusing to bind %s: a live writer holds the lease", sockPath)
	}
	if _, err := leaseFS.Lstat(sockPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat socket %s: %w", sockPath, err)
	}
	if err := leaseFS.Remove(sockPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear stale socket: %w", err)
	}
	return nil
}
